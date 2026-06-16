# Delivery Service (DS) — дизайн (подпроект 2, план 2 из нескольких)

**Дата:** 2026-06-17
**Статус:** утверждён
**Проект:** корпоративный мессенджер с E2E-шифрованием (production)

## Контекст

Подпроект 2 — Go-бэкенд (modular monolith `backend/`). План 1 (Authentication Service)
реализован и влит в master: OPAQUE-аутентификация, отзываемые сессии в Redis, реестр
устройств, KeyPackage Store, транзакционный KT-outbox.

Этот документ — **план 2: Delivery Service (DS)** — realtime-фабрика доставки сообщений
(«ядро масштабируемости на сокетах»). DS — недоверенный пересыльщик: видит только
шифртекст MLS-сообщений и маршрутные метаданные, не читает содержимое (модель угроз из
`docs/superpowers/specs/2026-06-16-crypto-protocol-design.md`).

Опирается на подпроект 1 (MLS-движок: каждое устройство = свой MLS-лист; сообщения —
непрозрачные блобы) и на AS (сессии, устройства). Инфра: Postgres (источник истины),
Redis (pub/sub + presence), RabbitMQ (не используется в этом плане).

### Принятые решения

| Решение | Выбор | Обоснование |
|---|---|---|
| Модель доставки | Журнал шифртекста + per-device курсоры | Мульти-девайс, история на новых устройствах, комплаенс-архив; унифицирует онлайн/офлайн |
| Межнодовый fan-out | Redis Pub/Sub | Низкая задержка, Redis уже в стеке; durability даёт журнал, pub/sub — только live-оповещение |
| Членство для роутинга | Явная серверная ростер-таблица | DS не парсит крипту; метаданные членства сервер и так видит |
| Welcome новому устройству | Через журнал по `join_seq` | Один режим роутинга; не нужно direct-адресование устройств |
| Топология | Stateless ноды за балансировщиком | Всё разделяемое состояние в Postgres/Redis → горизонтальный масштаб |

## Область и границы

**В scope (этот план — DS realtime-доставка):**
- WebSocket-шлюз с аутентификацией по сессии AS (одно соединение на устройство)
- Append-only журнал шифртекста на группу + per-device курсоры (Postgres)
- Монотонный `seq` на группу (тотальный порядок внутри группы)
- Межнодовый live fan-out через Redis pub/sub (канал на устройство)
- Sync-with-cursor (догон при подключении/после простоя)
- Ростер-таблица членства + HTTP API её обновления
- At-least-once доставка + ack + клиентский дедуп по `(group_id, seq)`

**Out of scope (отдельные планы/позже):**
- Rich-presence (онлайн-индикаторы), typing, read-receipts
- Mobile push / RabbitMQ-fan-out, внешние интеграции
- KT-лог и OPAQUE-клиент (отдельные планы)
- Полноценная политика авторизации ростер-API (здесь — базовая проверка членства)

## Архитектура и пакеты

Продолжаем modular monolith в `backend/`. Ноды DS не держат долговременного состояния
(всё в Postgres/Redis), поэтому масштабируются горизонтально за балансировщиком.

| Пакет | Ответственность | Зависит от |
|---|---|---|
| `internal/ws` | WebSocket upgrade, аутентификация, read/write-pumps, кодек фреймов, ping/pong | session, hub, delivery |
| `internal/hub` | локальный реестр соединений ноды (device_id → conn), диспетч входящих фреймов | — |
| `internal/fanout` | Redis pub/sub: publish в `dev:{deviceID}`, подписка ноды на локальные устройства | redis |
| `internal/roster` | членство группы (group_id → device_ids, join_seq), lookup для fan-out + CRUD | store |
| `internal/delivery` | оркестрация: append→seq→persist→publish; sync-since-cursor; ack→advance | store, roster, fanout |
| `internal/store` (доп.) | репозитории `messages`, `conversations`, `conversation_members`, `device_cursors` | pgx |

WS-библиотека: `github.com/coder/websocket` (контекстный API). Точная версия/сигнатуры —
сверяются при написании плана.

## Жизненный цикл соединения и протокол

**Подключение:** клиент открывает WS на `/ws`, передаёт session-токен AS (заголовок
`Authorization: Bearer` при upgrade). DS валидирует через `session.Manager.Validate` →
`{user_id, device_id}`. Без привязанного устройства — отказ в upgrade (401; сначала enroll).
Соединение регистрируется в `hub`; нода подписывается на `dev:{device_id}` в Redis;
presence-ключ с TTL обновляется heartbeat'ом. Ping/pong ловит мёртвые соединения. При
дисконнекте — дерегистрация + отписка + очистка presence.

**Фреймы (JSON-конверт `{type, ...}`):**
```
client→server:
  send  { client_msg_id, group_id, content_type, ciphertext }
  ack   { group_id, up_to_seq }
  sync  { group_id, since_seq }

server→client:
  sent     { client_msg_id, group_id, seq, server_ts }
  message  { group_id, seq, sender_device, content_type, ciphertext, server_ts }
  error    { code, message }
```

**Членство** (add/remove device в группу) — **HTTP API** DS, не WS (control-plane,
привязан к MLS-коммитам). Welcome новому устройству доставляется через журнал: коммиттер
сначала обновляет ростер (с `join_seq`), затем шлёт Welcome обычным `send`
(content_type=handshake); новое устройство синкается с `join_seq` и первым получает Welcome.

## Семантика доставки, порядок, синхронизация

**Порядок (per-group seq):** при `send` DS в одной транзакции инкрементит счётчик группы
и пишет сообщение:
```sql
UPDATE conversations SET next_seq = next_seq + 1 WHERE group_id=$1 RETURNING next_seq
INSERT INTO messages (group_id, seq, sender_device, content_type, ciphertext) VALUES (...)
```
Это сериализует append внутри группы (per-group throughput умеренный) и даёт тотальный
порядок, одинаковый для всех устройств. Если строки `conversations` ещё нет — создать
(первый `send` в группу или при создании ростера).

**Поток отправки:** `send` → транзакция выше → `sent {seq}` отправителю → fan-out: для
каждого `device_id` из ростера с `join_seq <= seq` — publish в `dev:{device_id}`;
нода-владелец соединения шлёт `message`. На другие устройства самого отправителя — да
(мульти-девайс); на то же устройство — нет.

**At-least-once + курсоры:** live-push — best-effort; источник истины — журнал. Устройство
подтверждает `ack {up_to_seq}` → двигается `device_cursors`. При подключении/после простоя
устройство шлёт `sync {since_seq=cursor}` → DS отдаёт пропущенные `message` из журнала.
Дедуп на клиенте по `(group_id, seq)`. Потеря pub/sub-сообщения → догон синком.

**Презенс для роутинга** — неявный: нода подписана только на каналы локально подключённых
устройств; офлайн-устройство не подписано и догонит синком. Онлайн-индикатор — вне scope.

## Модель данных

**Postgres:**
- `conversations` — group_id TEXT PK, next_seq BIGINT NOT NULL DEFAULT 0, created_at
- `messages` — group_id TEXT, seq BIGINT, sender_device UUID, content_type TEXT,
  ciphertext BYTEA, server_ts TIMESTAMPTZ; PK `(group_id, seq)`
- `conversation_members` — group_id TEXT, device_id UUID, join_seq BIGINT, added_at;
  PK `(group_id, device_id)`
- `device_cursors` — device_id UUID, group_id TEXT, acked_seq BIGINT; PK `(device_id, group_id)`

**Redis:**
- pub/sub каналы `dev:{device_id}` (live fan-out)
- `presence:device:{device_id} = node_id` (TTL + heartbeat) — диагностика/будущий презенс

Сервер хранит шифртекст и метаданные членства (в модели угроз; содержимое не читается).
Журнал = история мульти-девайс + комплаенс-архив.

## Обработка ошибок

- Невалидная сессия / нет привязанного устройства → отказ в WS upgrade (401).
- `send` в группу, где отправитель не член ростера → `error{forbidden}`.
- Backpressure: write-pump с ограниченным буфером; при переполнении — закрыть соединение
  (клиент переподключится и синкнётся).
- Дубликат `client_msg_id` → идемпотентный `sent` (повторно в журнал не пишем).
- Graceful shutdown: дренаж соединений, отписка от Redis.

## Тестирование

- **Unit:** кодек фреймов; fan-out-роутинг (мок pub/sub); присвоение seq; advance курсора.
- **Интеграция (testcontainers Postgres + miniredis/Redis):** монотонность seq при append;
  sync отдаёт ровно пропущенное; ack двигает курсор; ростер фильтрует по `join_seq`.
- **Многонодовый:** два инстанса DS на общих Postgres+Redis; устройство A на ноде 1 шлёт в
  группу, устройство B на ноде 2 получает через pub/sub (доказывает горизонтальный масштаб).
- **Reconnect/at-least-once:** устройство пропускает live-push → после `sync` получает
  пропущенное; дедуп по seq.
- **Welcome/join_seq:** новое устройство с `join_seq=N` при синке не видит сообщения < N,
  видит Welcome и далее.

## Что вне области этого плана/подпроекта

- Rich-presence, typing, read-receipts — отдельный план.
- Mobile push / RabbitMQ, внешние интеграции — позже.
- KT-лог, OPAQUE-клиент — отдельные планы.
- Полноценная авторизация ростер-API — позже (здесь базовая проверка членства).
- UI/UX (подпроект 4).
