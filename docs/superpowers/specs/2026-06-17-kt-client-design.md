# KT-client (клиентский верификатор ключей) — дизайн (подпроект 6, план 2 = 6.2)

**Дата:** 2026-06-17
**Статус:** утверждён
**Проект:** корпоративный мессенджер с E2E-шифрованием (production)

## Контекст

Перед добавлением устройства собеседника в MLS-группу клиент должен убедиться, что
signing-ключ этого устройства действительно принадлежит заявленной личности — иначе
сервер мог бы подменить ключ (MITM). Источник истины — KT-лог (RFC 6962 Merkle, готов в
`backend/internal/kt/`). 6.2 — **клиентский верификатор** этого контракта; потребитель — 6.3
(перед `addMember` сверяет ключ из KeyPackage с верифицированным набором).

Контракт KT (зафиксирован сервером, его и проверяем):
- `GET /kt/pubkey` → Ed25519 публичный ключ KT (доверяемый якорь).
- `GET /kt/sth` → подписанный tree-head `{tree_size, root_hash, signature}`.
- `GET /kt/key/{identity}` → `{leaf_index, version, device_set, audit_path[], sth}` — verifiable lookup.
- `GET /kt/proof/{inclusion,consistency}` — proof-эндпоинты.
Лог отдаёт только листы, покрытые текущим STH (404 пока тик не покрыл свежий enroll).

### Точные байтовые форматы (из `backend/internal/kt/leaf.go`, `sth.go`)

**Канонический лист** (`CanonicalLeaf`), big-endian:
```
identity_len(u32) ‖ identity ‖ version(u64) ‖ count(u32) ‖ for each key (sorted asc by raw bytes): key_len(u32) ‖ key
```
**Хеш листа** (RFC 6962): `SHA-256(0x00 ‖ canonical)`. **Внутренний узел:** `SHA-256(0x01 ‖ left ‖ right)`.
**Подписываемое сообщение STH:** `"KTSTHv1" ‖ tree_size(u64 BE) ‖ root_hash`, подпись Ed25519.

### Принятые решения

| Решение | Выбор | Обоснование |
|---|---|---|
| Где живёт | `client/src/shared/lib/kt/` (TS, Web Crypto) | FSD-инфраструктура; чистая, тестируемая |
| Хеширование | Своя реализация RFC 6962 на Web Crypto SHA-256 (не WASM) | Простые SHA-256 + префиксы; WASM не нужен (в отличие от OPAQUE/MLS) |
| Якорь доверия | TOFU на `/kt/pubkey` (пин в памяти; в конфиг — позже) | v1; внешний аудитор/gossip — будущее |
| Анти-форк | Хранить последний доверенный STH; новый STH проверять consistency-proof против него | Лог нельзя переписать/откатить между запросами |
| Доказательство интеропа | Тест против реального Go-сервера (httptest) + против известных байтовых векторов | Кодировка листа/хеша/подписи обязана совпасть байт-в-байт |

## Область и границы

**В scope (6.2):**
- `client/src/shared/lib/kt/`: RFC 6962-хеши (leaf/node), каноническое кодирование листа (как `CanonicalLeaf`),
  verify-inclusion (audit path → корень), verify-STH-signature (Ed25519), verify-consistency.
- Типизированный KT HTTP-клиент (`/kt/pubkey|sth|key/{id}|proof/...`).
- `verifyIdentity(identity)` → верифицированный набор signing-ключей (или ошибка).
- Стор последнего доверенного STH + monotonicity-проверка через consistency-proof.
- **Интероп-тест** против httptest-обёртки реального KT-сервиса: enroll → tick → lookup →
  клиент переэшировал device_set, проверил inclusion vs STH root, проверил подпись STH.

**Out of scope:**
- MLS-group флоу/использование результата при addMember — 6.3.
- VRF-приватность, not-membership, внешний аудитор/gossip — будущее (как помечено в KT-спеке).
- Персистентность доверенного STH между перезагрузками — позже (в памяти).
- Ed25519-проверка: использовать Web Crypto (`Ed25519`) либо аудированную мини-библиотеку, если
  рантайм без нативного Ed25519 (решается на этапе плана; предпочтительно Web Crypto subtle).

## Архитектура (FSD)

| Путь | Что |
|---|---|
| `shared/lib/kt/hash.ts` | `leafHash(bytes)`, `nodeHash(l,r)` (RFC 6962, Web Crypto SHA-256); `canonicalLeaf(identity,version,keys)` |
| `shared/lib/kt/proof.ts` | `verifyInclusion(leafHash, leafIndex, treeSize, auditPath, root)`, `verifyConsistency(...)` |
| `shared/lib/kt/sth.ts` | `sthMessage(treeSize, root)` + `verifySTHSignature(pubKey, sth)` (Ed25519) |
| `shared/api/kt.ts` | типизированный HTTP-клиент KT |
| `shared/lib/kt/client.ts` | `KTClient`: `verifyIdentity(identity)`, держит доверяемый STH, дергает consistency на обновлении |

Всё — презентационно-независимо (инфраструктура). Бизнес-использование — в 6.3.

## Поток `verifyIdentity(identity)`

```
1. pub ← getPubKey() (один раз, пин)
2. {leaf_index, version, device_set, audit_path, sth} ← GET /kt/key/{identity}
3. verifySTHSignature(pub, sth)                       — иначе FAIL (подделка STH)
4. monotonicity: если есть сохранённый STH с tree_size' ≤ sth.tree_size →
   verifyConsistency(stored, sth) (GET /kt/proof/consistency); сохранить новый STH
5. leaf ← canonicalLeaf(identity, version, decode(device_set))
   verifyInclusion(leafHash(leaf), leaf_index, sth.tree_size, audit_path, sth.root_hash) — иначе FAIL
6. вернуть набор signing-ключей (device_set), проверенный против лога
```
Несоответствие любого шага → беседа НЕ создаётся / устройство НЕ добавляется (в 6.3).
`device_set` декодируется ровно как сериализует сервер (формат уточняется чтением `kt_handlers.go`).

## Обработка ошибок

- 404 на `/kt/key/{identity}` (личность ещё не покрыта STH) → «ключи ещё не опубликованы, повтор позже».
- Неверная подпись STH / inclusion / consistency → жёсткий FAIL (потенциальная подмена) — не молчать.
- Сбой сети → ретрай; отсутствие KT-pubkey → ошибка инициализации (нельзя верифицировать).

## Тестирование

- **Юнит (векторы):** `canonicalLeaf` против заранее посчитанных байтов (совпадение с `CanonicalLeaf`);
  `leafHash`/`nodeHash` против известных RFC 6962 SHA-256 значений; `verifyInclusion` на маленьком
  дереве (корень сходится / искажённый путь → fail); `sthMessage` совпадает с `"KTSTHv1"…`.
- **Интероп (главное):** поднять реальный KT-сервис (Go httptest, как в `kt_e2e_test.go`) ИЛИ
  использовать зафиксированные векторы, снятые с сервера; enroll→tick→`GET /kt/key` → TS-клиент
  переэшировал и проверил inclusion vs STH root + подпись STH публичным ключом из `/kt/pubkey`.
- **Анти-форк:** два STH (рост дерева) + consistency-proof → ок; поддельный/несогласованный → fail.
- **Negative:** подменённый device_set → inclusion fails; подменённая подпись → verify fails.

## Что вне области этого плана

- Клиентский MLS-group флоу и использование verifyIdentity при addMember — 6.3.
- Персистентность доверенного STH, VRF/not-membership, внешний аудитор/gossip — будущее.
