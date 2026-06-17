# Key Transparency Log (KT) — дизайн (подпроект 2, план 3 из нескольких)

**Дата:** 2026-06-17
**Статус:** утверждён
**Проект:** корпоративный мессенджер с E2E-шифрованием (production)

## Контекст

Подпроект 2 — Go-бэкенд (modular monolith `backend/`). Реализованы и влиты: план 1
(Authentication Service — OPAQUE, сессии, устройства, KeyPackage Store, транзакционный
`kt_outbox`), план 2 (Delivery Service — WebSocket-фабрика, журнал, ростер).

Этот документ — **план 3: Key Transparency Log (KT)** — аудируемый append-only лог
привязок «личность → набор устройств», чтобы недоверенный сервер не мог незаметно
вставить/подменить ключ устройства (MITM). Опирается на крипто-спеку
(`docs/superpowers/specs/2026-06-16-crypto-protocol-design.md`: KT в стиле CONIKS/WhatsApp,
подписанные STH, доказательства включения для клиентов) и на AS, который уже пишет
изменения набора устройств в `kt_outbox`.

Инфра: Postgres (источник истины + лог), Redis (есть в стеке; здесь не обязателен).

### Принятые решения

| Решение | Выбор | Обоснование |
|---|---|---|
| Структура | Хронологический Merkle-лог (RFC 6962) + индекс Postgres | Закрывает MITM-детект (tamper-evident append-only + inclusion/consistency); зрелая математика; буилдабельно за один план |
| Математика доказательств | `github.com/transparency-dev/merkle` | Примитивы RFC 6962 (извлечены из Trillian) — не катаем свою крипту |
| Содержимое листа | Полный active-набор устройств + версия | Чистая монотонная история версий личности; lookup «текущие ключи X» = последний лист + inclusion proof |
| Каденс STH | На каждый relay-тик | Просто; эпохи по времени — оптимизация позже |
| Единый аппендер | Postgres advisory-lock | Один аппендер среди N stateless-нод без отдельного лидер-выбора |
| Ключ подписи | Ed25519, отдельный от OPAQUE, из env | Изоляция ключей; общий для всех нод |

## Область и границы

**В scope (этот план — хроно-лог KT):**
- Append-only хронологическое Merkle-дерево листьев (RFC 6962) поверх Postgres
- Лист = canonical-сериализация `{identity(user_id), version, device_set[signing_public_key...], ts}`
- Relay-петля: потребляет `kt_outbox`, по событию берёт текущий active-набор устройств → новый лист (новая версия), помечает `relayed_at`
- Signed Tree Heads (STH): `{tree_size, root_hash, ts}`, подпись Ed25519
- Доказательства inclusion и consistency
- HTTP API: latest STH, inclusion/consistency proofs, lookup ключей личности + inclusion, публичный ключ KT
- Единый аппендер через Postgres advisory-lock

**Out of scope (отдельные планы/позже):**
- VRF-приватность личностей и доказательства not-membership (апгрейд до CONIKS/AKD)
- Внешний аудитор-сервис и gossip-протокол (детект split-view)
- Клиентская верификация (KT-client) — отдельный план; форматы STH/proofs/листа фиксируются здесь как контракт
- Фиксированные эпохи по времени (оптимизация батчинга сверх per-tick STH)

## Архитектура и пакеты (modular monolith, `backend/`)

| Пакет | Ответственность | Зависит от |
|---|---|---|
| `internal/kt/log` | Merkle-лог: append листа, расчёт корня, inclusion/consistency proofs (обёртка над `transparency-dev/merkle`) | store, transparency-dev/merkle |
| `internal/kt/sth` | подпись/проверка STH (Ed25519, ключ из env) | — |
| `internal/kt/relay` | петля: advisory-lock → читает `kt_outbox` → собирает active-набор из `devices` → append листьев → новый STH → mark relayed | store, log, sth |
| `internal/store` (доп.) | репозитории `kt_leaves`, `kt_sths` | pgx |
| `internal/httpapi/kt_handlers.go` | `/kt/sth`, `/kt/proof/*`, `/kt/key/{identity}`, `/kt/pubkey` | log, sth, store |
| `cmd/genkeys` (доп.) | печать KT signing key (как для OPAQUE) | — |

Relay запускается фоном в `main.go`; advisory-lock гарантирует единственный аппендер
среди N stateless-нод. Лог детерминирован: порядок листьев = порядок `kt_outbox.id`.

**Масштаб (заметка):** для первого плана inclusion/consistency proofs считаются из хранимых
leaf-хэшей (при необходимости — построение поддерева в памяти на запрос). Персистентный
кэш узлов / compact-ranges — оптимизация на будущее.

## Поток данных

1. AS пишет в `kt_outbox` (уже есть; событие `device_added`/`device_revoked`, payload `{device_id}`).
2. Relay-тик (под advisory-lock): выбрать неотрелеенные события по возрастанию `id`; для
   каждого `user_id` собрать текущий active-набор `signing_public_key` из `devices`;
   сериализовать лист `{identity, version, device_set, ts}` с `version = (max version
   личности)+1`; вставить в `kt_leaves` с монотонным `leaf_index`; пометить событие
   `relayed_at` — всё в одной транзакции.
3. После батча: пересчитать корень дерева размера n, подписать и вставить новый STH в `kt_sths`.
4. Lookup: `/kt/key/{identity}` → последний лист личности + `leaf_index` + inclusion proof
   против текущего STH. Клиент проверяет proof и подпись STH.
5. Аудит: клиент/аудитор тянет STH со временем, проверяет consistency-proofs (append-only),
   мониторит свою историю версий (детект подмены).

## Модель данных и ключ подписи

**Postgres:**
- `kt_leaves` — `leaf_index BIGINT PRIMARY KEY` (0..n-1), `identity UUID NOT NULL`,
  `version BIGINT NOT NULL`, `device_set BYTEA NOT NULL` (canonical-сериализация набора),
  `leaf_hash BYTEA NOT NULL` (RFC6962 leaf hash), `created_at TIMESTAMPTZ`.
  Индекс по `(identity, version)` для lookup последней версии.
- `kt_sths` — `tree_size BIGINT PRIMARY KEY`, `root_hash BYTEA NOT NULL`,
  `signature BYTEA NOT NULL`, `created_at TIMESTAMPTZ`. Последний STH = max(tree_size).
- Вход — существующий `kt_outbox` (`relayed_at` помечается в той же транзакции, что и вставка листа).

**Версионирование:** `version` на личность = (текущий max version данной identity)+1,
считается в транзакции под advisory-lock — монотонно.

**Ключ подписи KT:** Ed25519, отдельный от OPAQUE-ключа; base64 из env (`KT_SIGNING_KEY`),
общий для всех нод; печатается `cmd/genkeys`. Публичный ключ раздаётся через `/kt/pubkey`.

**Канонизация листа:** фиксированная детерминированная сериализация — поля length-prefixed,
`signing_public_key` отсортированы по байтам, — чтобы `leaf_hash` был воспроизводим клиентом.
Формат фиксируется как контракт для KT-client.

## HTTP API

```
GET /kt/pubkey                          → { kt_public_key }
GET /kt/sth                             → { tree_size, root_hash, signature, ts }
GET /kt/proof/inclusion?leaf_index=&tree_size=
                                        → { leaf_index, tree_size, audit_path[] }
GET /kt/proof/consistency?from=&to=     → { from, to, proof[] }
GET /kt/key/{identity}                  → { leaf_index, version, device_set,
                                            inclusion: { tree_size, audit_path[] },
                                            sth: { tree_size, root_hash, signature, ts } }
```
Эндпоинты read-only, доступны аутентифицированным пользователям (через существующий
`auth`-middleware). STH и proofs сами по себе не секретны.

## Обработка ошибок

- Lookup несуществующей личности (нет листьев) → 404.
- Proof с `tree_size` больше текущего, или несогласованные `from/to/leaf_index` → 400.
- Relay идемпотентен: повторный тик не дублирует листья (`relayed_at` фильтрует,
  advisory-lock сериализует аппенд).
- Если у личности после события не осталось active-устройств — лист с пустым набором
  (валидно: «нет ключей»), версия инкрементится.

## Тестирование

- **Unit:** канонизация листа детерминирована/воспроизводима; STH sign/verify; tampered STH
  отвергается.
- **Свойства (через `transparency-dev/merkle` verifier):** inclusion proof для листа i
  проверяется против root из STH размера n; consistency proof между STH m и n проверяется;
  подделка (изменённый лист/корень) → верификация падает.
- **Интеграция (testcontainers Postgres):** relay из `kt_outbox` создаёт листья в порядке
  `id`; `device_added`→`device_revoked` даёт монотонные версии с корректным active-набором;
  advisory-lock — параллельные тики не плодят дубли (один аппендер); lookup отдаёт последнюю
  версию + валидный inclusion против текущего STH.
- **E2E через AS:** enroll устройства (AS) → событие в `kt_outbox` → relay → `/kt/key/{user}`
  отдаёт набор с этим устройством + проверяемый proof.

## Что вне области этого плана/подпроекта

- VRF-приватность, not-membership, CONIKS/AKD-апгрейд.
- Внешний аудитор, gossip (split-view detection).
- Клиентская верификация (KT-client) — отдельный план; контракт зафиксирован здесь.
- Per-conversation авторизация ростера DS (отложенный пункт плана 2, M2) — не относится к KT.
- UI/UX (подпроект 4).
