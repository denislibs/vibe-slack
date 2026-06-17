# Client MLS group flow + conversation UI — дизайн (подпроект 6, план 3 = 6.3)

**Дата:** 2026-06-17
**Статус:** утверждён
**Проект:** корпоративный мессенджер с E2E-шифрованием (production)

## Контекст

Последний кусок до «живого чата». Бэкенд бесед (6.1) и KT-client (6.2) готовы. Здесь —
клиентская MLS-механика, проведённая через три потока, + экраны бесед. Решение пользователя:
**сразу всё** — DM, приватные И публичные каналы (публичные через **MLS external commit**).

Готово: crypto-core (OpenMLS 0.6, `use_ratchet_tree_extension(true)` включён) уже умеет
`create_group`, `add_member` (→ commit+welcome, `group_info` сейчас отбрасывается),
`remove_member`, `join_from_welcome`, `encrypt`, `process`, `create_group_with_compliance`,
`key_package_bytes`, `signing_public_key`. DS доставляет шифртекст по device-ростеру (journal +
`join_seq` для Welcome). Беседы (6.1): conversation_meta (dm/channel, public/private), членство-юзеры,
device-роутинг гейтится членством. KT-client: `verifyIdentity(identity) → verified device keys`.

### Развязка: беседа ↔ MLS-группа ↔ устройства

Одна беседа (`group_id`) = одна MLS-группа. Участник-**пользователь** в беседе (6.1) ⇒ все его
**устройства** должны быть в MLS-группе И в device-ростере DS. Добавление участника = (1) на клиенте
получить KeyPackages всех его устройств (`GET /keypackages/{device_id}`), (2) **KT-проверить**, что
их signing-ключи входят в KT-набор личности (6.2), (3) MLS `add_member` → commit + Welcome,
(4) залить device'ы в DS-ростер (`POST /conversations/{group}/members` с `join_seq`), (5) разослать
commit существующим, Welcome — новым (через DS journal).

### Вход в публичный канал (external commit)

Существующий участник не нужен онлайн. Поток:
- После каждого изменения группы участник экспортирует свежий `GroupInfo` (с ratchet tree) и
  публикует на сервер (`PUT /conversations/{group}/group-info`, хранится в conversation_meta/доп.
  таблице). Это **не E2E-секрет** — GroupInfo предназначен для входа (содержит публичный ratchet tree).
- Джойнер: `GET /conversations/{group}/group-info` → `join_by_external_commit(groupInfo)` в crypto-core
  → external-commit сообщение → отправляет в DS; добавляет свои device'ы в ростер; существующие
  участники применяют external commit через `process` (приходит как обычный commit).
- Перед входом джойнер не верифицирует чужих (он лишь присоединяется); существующие, получив commit,
  узнают нового члена — его личность видна в ростере беседы (6.1) и может быть KT-сверена.

### Принятые решения

| Решение | Выбор | Обоснование |
|---|---|---|
| Объём | DM + приватные (Add+Welcome) + публичные (external commit) | Выбор пользователя «сразу всё» |
| crypto-core | + `export_group_info`, `join_by_external_commit`, отдать `group_info` из add/remove | OpenMLS 0.6 поддерживает; ratchet-tree-extension уже включён |
| GroupInfo | Публикуется на сервер для публичных каналов; не секрет | Нужен для external join; ratchet tree публичен |
| KT-проверка | Перед `add_member` (DM/приватные); для входящих в публичный — по факту членства | MITM-защита на добавлении |
| Комплаенс | Видимый комплаенс-участник в каждой группе (как решено в пхп1) | Корпоративный escrow |
| UI | Презентационные экраны: список бесед, окно беседы, composer; бизнес-логика в app/features | FSD, чистая архитектура |

## Декомпозиция (3 плана)

- **6.3a — crypto-core external commit + GroupInfo (Rust→WASM) + плумбинг воркера.** Экспорт
  `export_group_info`, `join_by_external_commit`; вернуть `group_info` из `add_member`/`remove_member`;
  расширить протокол крипто-воркера (`protocol.ts`/`worker.ts`/`CryptoClient`) новыми kind'ами;
  e2e-крипто-тесты (alice создаёт публичную группу → bob входит external-commit'ом → оба шифруют/дешифруют;
  RFC-vector-структурный гейт как раньше). Пересборка wasm-pkg.
- **6.3b — клиентская оркестрация бесед (app/features).** `create-conversation` (DM/канал → создать
  на сервере 6.1 + создать MLS-группу + комплаенс), `add-member` (KeyPackages + **KT verifyIdentity** +
  MLS add + DS ростер + раздача commit/Welcome), `join-public` (GET group-info + external commit + ростер),
  входящие commit/Welcome → `process`/`join_from_welcome`; publish GroupInfo после изменений; реальная
  отправка/приём текста через готовый orchestrator (encrypt → DS send; DS message → decrypt → стор).
  Бэкенд: эндпоинты `PUT/GET /conversations/{group}/group-info` (хранение GroupInfo, gate по членству).
- **6.3c — UI бесед (pages/widgets, презентационные).** Сайдбар списка бесед (из 6.1 list) + поиск/старт
  DM (6.1 search) + создание канала; окно беседы (сообщения из стора, composer); подключение к app-гейту
  (после выбора workspace → реальный список бесед вместо заглушки). Solid-тесты.

## Область и границы

**В scope:** см. 3 плана выше — полный вертикальный срез «найти → беседа (DM/канал) → E2E-обмен», вкл.
публичные каналы через external commit, KT-проверку при добавлении, видимый комплаенс, экраны.

**Out of scope:** приглашения по email (пхп7), межворкспейс Slack Connect (пхп8), богатый UI/анимации
(пхп4), реакции/треды/редактирование/файлы, перенос истории до входа (MLS даёт ключи только с момента
вступления — свойство), офлайн-очередь сверх уже имеющейся в протокол-воркере, ключевой ротейшн/PCS-политики
(полагаемся на штатный MLS), персистентность доверенного KT-STH между перезагрузками.

## Тестирование

- **6.3a:** Rust unit/e2e в crypto-core (создать публичную группу → export GroupInfo → join_by_external_commit
  на втором движке → обмен; add/remove возвращают GroupInfo; join_from_welcome как раньше). Сборка wasm.
- **6.3b:** unit use-case'ов с моками (CryptoClient/ProtocolClient/AS/KT/conversations) — последовательности
  вызовов, KT-проверка блокирует add при несовпадении; интеграция против мок-DS как в transport-demo.
- **6.3c:** `@solidjs/testing-library` презентационные тесты; FSD-границы (нет бизнес-логики в UI).
- **Сквозной слайс:** alice логинится → создаёт DM с bob по username → KT-verify → MLS add → шлёт текст →
  bob (второй движок/стор) принимает и расшифровывает (как capstone transport-demo, но через реальный
  conversation-флоу).

## Что вне области этого плана/подпроекта

- Приглашения email (пхп7), межворкспейс (пхп8), UI/UX-полиш и анимации (пхп4).
- Серверная ротация/ревокация ключей сверх KT; модерация; админ-UI каналов.
