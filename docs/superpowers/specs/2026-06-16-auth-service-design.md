# Authentication Service (AS) — дизайн (подпроект 2, план 1 из нескольких)

**Дата:** 2026-06-16
**Статус:** утверждён
**Проект:** корпоративный мессенджер с E2E-шифрованием (production)

## Контекст

Подпроект 2 — Go-бэкенд. Он состоит из нескольких подсистем (AS, DS, KT, KeyPackage
Store, WebSocket-фабрика, presence). Этот документ описывает **первый план: Authentication
Service (AS)** — корень доверия. Остальные подсистемы получают свои планы.

Опирается на спеку криптопротокола (`docs/superpowers/specs/2026-06-16-crypto-protocol-design.md`):
там определены роли сервера (AS/DS/KT), модель «сервер недоверенный», OPAQUE и Key
Transparency. Подпроект 1 (MLS-движок) уже реализован: каждое устройство имеет свой
MLS signing key + credential и публикует one-time KeyPackages.

Инфраструктура: Docker, PostgreSQL (источник истины), Redis (сессии, login-state,
rate-limit), RabbitMQ (доставка — используется в плане DS, не здесь).

### Принятые решения

| Решение | Выбор | Обоснование |
|---|---|---|
| Топология | Modular monolith (один Go-бинарь, пакеты по ролям) | Горизонтальное масштабирование N идентичными инстансами без microservice-налога; splittable позже |
| Первый план | AS (аутентификация) | Корень доверия; аутентифицированные сессии гейтят всё остальное, включая сокеты |
| Сессии | Серверные отзываемые в Redis | Мгновенный отзыв (logout устройства, бан); Redis уже в стеке |
| Регистрация устройств | Пароль (OPAQUE) + публикация изменений в KT-лог; cross-sign позже | KT даёт детектируемость rogue-устройств; cross-signing — будущее усиление |
| Username | Email | Корпоративная почта как идентификатор аккаунта |
| OPAQUE lib | `github.com/bytemare/opaque` (серверная сторона) | RFC-совместимая зрелая Go-реализация |
| HTTP | stdlib `net/http` (роутинг Go 1.22) | Минимум зависимостей — важно для security-продукта |

## Область и границы

**В scope (этот план — AS):**
- OPAQUE-регистрация и вход (сервер никогда не видит пароль)
- Серверные отзываемые сессии в Redis (+ device logout, logout/all)
- Реестр устройств: enroll после OPAQUE-входа, привязка MLS-credential/подписного ключа
- KeyPackage Store: загрузка пула one-time KeyPackages + last-resort, выдача, replenishment
- HTTP/JSON API (Go stdlib net/http) + Docker-обвязка (Postgres, Redis)

**Out of scope (отдельные планы):**
- **KT-лог** (Merkle-дерево, STH, доказательства включения/консистентности) — отдельный
  план. Здесь AS лишь эмитит событие изменения набора устройств в `kt_outbox` (в той же
  транзакции). Источник истины по привязкам — Postgres.
- **DS / WebSocket-фабрика** — отдельный план (следующий после AS).
- **OPAQUE-клиент** — отдельный парный план; здесь фиксируется wire-протокол OPAQUE и
  библиотека, клиент реализуется против них.
- Cross-signing устройств — будущее усиление.

Это держит план сфокусированным и автономно тестируемым (через HTTP API + тестовый
OPAQUE-клиент на `bytemare/opaque`).

## Топология и структура (modular monolith)

Один Go-бинарь `backend/`, пакеты по ответственности:

| Пакет | Ответственность | Зависит от |
|---|---|---|
| `cmd/server` | точка входа, сборка зависимостей, graceful shutdown | всё ниже |
| `internal/httpapi` | HTTP-роутер (net/http 1.22), middleware (auth, logging, recover), DTO | as, session |
| `internal/as` | OPAQUE регистрация/вход, оркестрация | opaque, store, session |
| `internal/opaque` | тонкая обёртка над `github.com/bytemare/opaque` (server), конфиг suite | — |
| `internal/session` | выпуск/валидация/отзыв серверных сессий в Redis | redis |
| `internal/devices` | реестр устройств: enroll, список, привязка ключей, эмит KT-события | store |
| `internal/keypackages` | загрузка/выдача/replenishment KeyPackage-пула, last-resort | store |
| `internal/store` | репозитории Postgres, миграции | pgx |
| `internal/platform/redis`, `internal/platform/postgres` | клиенты/пулы соединений | — |

Каждый пакет — одна ответственность, явный интерфейс, тестируется изолированно
(репозитории за интерфейсами → моки; интеграция → testcontainers).

**Инфра (Docker Compose):** `postgres`, `redis`, `backend`. Миграции — раннер
(`golang-migrate` или `goose`), запускаемый отдельной командой/при старте.

**Масштабирование:** инстансы AS stateless (всё разделяемое состояние — в Redis/Postgres),
поэтому масштабируются горизонтально за балансировщиком.

## OPAQUE-поток и сессии

OPAQUE (aPAKE): сервер хранит per-user registration record, из которого пароль не
восстановить. Конфигурация suite фиксируется здесь и становится частью контракта для
OPAQUE-клиента.

**Регистрация:**
```
POST /auth/register/start   { email, opaque_registration_request }
                          →  { opaque_registration_response }
POST /auth/register/finish  { email, opaque_registration_record }
                          →  { ok }   // сервер хранит record в Postgres (users.opaque_record)
```

**Вход (OPAQUE login = KE1/KE2/KE3):**
```
POST /auth/login/start   { email, ke1 }     →  { login_id, ke2 }
POST /auth/login/finish  { login_id, ke3 }  →  { session_token, device_enroll_required }
```
- `login_id` — кратковременное login-state в Redis (TTL ~30с), чтобы не держать состояние
  в памяти конкретной ноды (масштабирование на N инстансов).
- При успехе сервер выпускает `session_token` (случайный, ~256 бит) и кладёт состояние
  в Redis: `session:{token} → { user_id, device_id?, created, expires }`. TTL + скользящее
  продление.

**Сессии (Redis, отзываемые):**
```
GET  /auth/session   (Authorization: Bearer <token>)  → { user_id, device_id }
POST /auth/logout        → удаляет session:{token}
POST /auth/logout/all    → удаляет все сессии user_id (бан / смена пароля)
```
Middleware `auth` проверяет токен в Redis на каждый защищённый запрос (отзыв мгновенный).

## Реестр устройств, KeyPackage Store, точка KT

**Enroll устройства.** После OPAQUE-входа клиент генерирует MLS signing key + credential
и регистрирует устройство:
```
POST /devices                 { signing_public_key, label, initial_key_packages[] }
                            →  { device_id }
GET  /devices                 → [ { device_id, label, status, created_at } ]
POST /devices/{id}/revoke     → status=revoked
```
В `login/finish` `device_enroll_required` = true, если токен ещё не привязан к устройству →
клиент обязан вызвать `POST /devices`; токен затем апгрейдится до device-scoped.

**KeyPackage Store** (стиль Signal one-time prekeys):
```
POST /keypackages             { key_packages[] }                 // пополнение пула устройства
GET  /keypackages/{device_id} → { key_package, is_last_resort }  // выдаёт ОДИН, помечает consumed
GET  /keypackages/count       → { available }                    // клиент пополняет при низком уровне
```
- One-time KeyPackage выдаётся ровно один раз и удаляется/помечается consumed (forward
  secrecy при добавлении устройства в группу).
- При исчерпании пула выдаётся **last-resort** KeyPackage (переиспользуемый, `is_last_resort=true`).
- Сервер недоверенный к содержимому: хранит KeyPackage как непрозрачный blob, привязанный
  к device_id (опц. лёгкая sanity-проверка размера/версии).

**Точка интеграции с KT (transactional outbox).** Любое изменение набора устройств/ключей
пишет событие в `kt_outbox` **в той же БД-транзакции**, что и само изменение (без dual-write).
Будущий KT-сервис читает outbox и кладёт в Merkle-лог. Гарантирует, что KT не разойдётся
с источником истины.

## Модель данных (Postgres)

- `users` — id, email (unique), `opaque_record` (bytea), created_at
- `devices` — id, user_id→users, `signing_public_key` (bytea), label, status
  (active|revoked), created_at, revoked_at (nullable)
- `key_packages` — id, device_id→devices, `key_package` (bytea), is_last_resort (bool),
  consumed_at (nullable), created_at
- `kt_outbox` — id, user_id, event_type, payload (jsonb), created_at, relayed_at (nullable)

Сессии и login-state хранятся только в Redis.

## Обработка ошибок и безопасность

- **Анти-энумерация пользователей:** для несуществующего email вход отрабатывает как для
  существующего (OPAQUE «fake record»), ответы неотличимы — нельзя выяснить, есть ли аккаунт.
- **Rate-limiting** auth-эндпоинтов (счётчики в Redis) против онлайн-перебора.
- Пароль нигде не хранится в восстановимом виде (свойство OPAQUE).
- Единый формат ошибок API `{ error: <code>, message: <text> }`; без утечки внутренних деталей.
- Все защищённые эндпоинты требуют валидную сессию (middleware `auth`).

## Тестирование

- **Unit** на пакет: репозитории за интерфейсами → моки.
- **Интеграция** (`testcontainers`, реальные Postgres + Redis):
  - Полный OPAQUE round-trip (тестовый клиент на `bytemare/opaque`): register → login →
    session_token.
  - Жизненный цикл сессии: выдача → валидация → отзыв (logout) → токен немедленно невалиден.
  - Enroll устройства; `device_enroll_required` флоу.
  - KeyPackage: upload → consume (один и тот же one-time не выдаётся дважды) → exhaustion →
    last-resort.
  - Запись в `kt_outbox` в той же транзакции, что и изменение устройств.
- **Security-тесты:**
  - Вход для несуществующего пользователя неотличим от существующего (анти-энумерация).
  - Отзыв сессии немедленный.
  - Пароль не восстановим из хранимого record.

## Что вне области этого подпроекта/плана

- Реализация KT-лога (Merkle, STH, proofs) — отдельный план; здесь только `kt_outbox`.
- DS / WebSocket-фабрика, presence — отдельный план.
- OPAQUE-клиент — отдельный парный план (контракт зафиксирован здесь).
- Cross-signing устройств — будущее усиление.
- UI/UX (подпроект 4).
