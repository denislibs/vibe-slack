# Workspaces Foundation (мульти-тенант ядро) — дизайн (подпроект 5, план 1)

**Дата:** 2026-06-17
**Статус:** утверждён
**Проект:** корпоративный мессенджер с E2E-шифрованием (production)

## Контекст

Переход на **Вариант B (мульти-воркспейс / multi-tenant)**: один аккаунт может создавать
и состоять в нескольких воркспейсах; внутри WS — беседы (каналы) и личные 1:1; поиск по
email/username; роли owner/admin/member; межворкспейс-каналы (Slack Connect). Это большой
объём, разбит на 4 подпроекта:

5. **Фундамент воркспейсов** (этот документ) — контейнер + членство + роли + глобальный username.
6. Директория + беседы внутри WS (поиск, 1:1 DM, каналы, клиентский MLS-group флоу, KT-client,
   ростер-authz по членству, починка bearer-над-WebSocket).
7. Приглашения (existing по email/username — частично уже в пхп5; **email-инвайты без аккаунта** +
   accept-on-register + email-сервис — здесь).
8. Межворкспейс-каналы (Slack Connect): кросс-тенантная MLS-группа, кросс-тенантный KT, guest.

Готово к этому моменту: AS (`users` с email, `devices`, `key_packages`, сессии в Redis),
DS (`conversations`, `messages`, `conversation_members`, `device_cursors`), KT
(`kt_leaves`, `kt_sths`, `kt_outbox`), фронт (SolidJS+FSD, OPAQUE-client, auth-гейт).
Текущая модель **плоская** — нет понятия воркспейса/тенанта.

### Принятые решения

| Решение | Выбор | Обоснование |
|---|---|---|
| Стратегия скоупинга | A — аддитивный фундамент; `workspace_id` как будущий tenant-ключ, conversations/KT привязываем в пхп6/8 | Минимальный риск; не правим работающий код без потребителя (YAGNI); каждый подпроект чистый |
| Где живёт воркспейс | Новый модуль `internal/workspace` в монолите | Org/identity-смежный, но отдельный концерн; AS остаётся только про auth |
| Роли | owner / admin / member | Соответствует требованию (создатель→owner, admin создаёт/приглашает) |
| Username | Глобально уникальный, рядом с email | Проще для KT/идентичности; поиск по обоим (поиск — пхп6) |
| Добавление людей в пхп5 | Прямое добавление **уже зарегистрированного** (owner/admin, без письма) | Даёт многочленные WS сразу → в пхп6 есть кому писать; email-инвайты — пхп7 |
| Передача владения | Вне области v1 | YAGNI; owner стабилен, добавим позже |

## Область и границы

**В scope:**
- Миграция: таблицы `workspaces`, `workspace_members`; колонка `username` (unique) на `users`.
- Регистрация AS принимает `username` (единственное касание AS).
- Модуль `internal/workspace`: repo + service + HTTP, переиспользует device-bound сессию AS.
- API: создать WS, список «моих» WS, состав, прямое добавление/удаление участника, смена роли.
- Резолв пользователя по email/username (минимальный, для add-эндпоинта; полнотекстовый поиск — пхп6).
- Проверки прав по ролям (owner > admin > member).
- Тонкий фронт-срез: `shared/api/workspace.ts`, «текущий воркспейс» в session-сторе,
  минимальный экран create/select WS как гейт перед чатом.

**Out of scope (отдельные подпроекты/планы):**
- Беседы/каналы/DM, поиск-директория, клиентский MLS-group флоу, KT-client — пхп6.
- Email-инвайты без аккаунта, accept-on-register, email-сервис — пхп7.
- Межворкспейс (Slack Connect), guest-роль — пхп8.
- Привязка `conversations`/KT к `workspace_id` — пхп6/8 (фундамент закладываем расширяемо).
- Передача владения, переименование/удаление WS со всеми данными, аватары/брендинг WS.

## Данные (новая миграция `0004_workspaces.sql`)

```sql
CREATE TABLE workspaces (
  id            UUID PRIMARY KEY,
  name          TEXT NOT NULL,
  slug          TEXT NOT NULL UNIQUE,           -- человекочитаемый идентификатор (a-z0-9-)
  owner_user_id UUID NOT NULL REFERENCES users(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workspace_members (
  workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  user_id      UUID NOT NULL REFERENCES users(id),
  role         TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
  joined_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (workspace_id, user_id)
);
CREATE INDEX workspace_members_user_idx ON workspace_members(user_id);

ALTER TABLE users ADD COLUMN username TEXT;
-- реальных пользователей ещё нет → бэкфилл не нужен; делаем NOT NULL + UNIQUE
UPDATE users SET username = id::text WHERE username IS NULL;  -- защита, если строки есть
ALTER TABLE users ALTER COLUMN username SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_username_key UNIQUE (username);
```

Создание WS — в одной транзакции: вставка `workspaces` + строки `workspace_members` с
ролью `owner`. Инвариант: для каждого WS ровно один `owner` (= `workspaces.owner_user_id`).

## Модуль `internal/workspace`

- `store` (Postgres, pgx): `WorkspaceRepo` — `Create(tx)`, `ListForUser`, `Members`, `AddMember`,
  `RemoveMember`, `SetRole`, `RoleOf(workspaceID, userID)`; `slug` генерируется из имени + дедуп.
- `UserLookup` (в `internal/store`/`users`): `FindByEmailOrUsername(q) → (userID, found)` — для add.
- `service` — бизнес-правила и проверки прав (см. матрицу). Не знает про HTTP.
- `httpapi` — хендлеры; аутентификация через существующий session-middleware AS
  (Bearer device-bound токен → `userID`).

Username при регистрации: AS `register/finish` (где создаётся запись `users`) принимает
`username`; валидация формата (`^[a-z0-9_]{3,32}$`), конфликт → 409. Это единственное касание AS.

## API (HTTP, JSON; Bearer device-bound сессия)

| Метод | Путь | Кто | Действие |
|---|---|---|---|
| POST | `/workspaces` | любой аутентиф. | `{name}` → создать; вызывающий → owner; вернуть `{id, slug, role:"owner"}` |
| GET | `/workspaces` | любой | список «моих» WS со своей ролью |
| GET | `/workspaces/{id}/members` | member+ | состав: `[{user_id, username, email, role}]` |
| POST | `/workspaces/{id}/members` | owner/admin | `{email_or_username}` → резолв existing → добавить как `member`; 404 если юзер не найден, 409 если уже состоит |
| PATCH | `/workspaces/{id}/members/{user_id}` | owner | `{role:"admin"\|"member"}` сменить роль |
| DELETE | `/workspaces/{id}/members/{user_id}` | owner (любого, кроме owner) / admin (только member) | убрать участника |
| DELETE | `/workspaces/{id}/members/me` | member/admin (не owner) | покинуть WS |

Все, кроме `POST /workspaces`, проверяют членство вызывающего и его роль. Несостоящий →
404 (не 403 — не раскрываем существование чужого WS).

## Роли и права (матрица v1)

| Действие | owner | admin | member |
|---|---|---|---|
| Создать WS (→ становится owner) | — | — | — (любой юзер) |
| Видеть состав | ✓ | ✓ | ✓ |
| Добавить member (existing) | ✓ | ✓ | ✗ |
| Убрать member | ✓ | ✓ | ✗ |
| Убрать admin | ✓ | ✗ | ✗ |
| Сменить роль (member↔admin) | ✓ | ✗ | ✗ |
| Покинуть WS | ✗ (нужна передача — отложено) | ✓ | ✓ |
| Убрать/понизить owner | ✗ (никто) | ✗ | ✗ |

Owner стабилен: не удаляется и не понижается никем; передача владения — будущий план.

## Фронт-срез (тонкий, FSD)

- `shared/api/workspace.ts` — типизированный клиент (create/list/members/addMember/…), как `as.ts`.
- `entities/session` (или новый `entities/workspace`) — «текущий воркспейс» (`currentWorkspaceId`),
  список «моих» WS; сохраняется в памяти (как session-токен).
- Гейт после onboarded: если у пользователя нет/не выбран WS → минимальный экран
  «создать или выбрать воркспейс»; после выбора → чат (по-прежнему `groupId` пока заглушка до пхп6).
- UI презентационный (prop-driven), бизнес-логика в `app`/`features` (чистая архитектура).

Полноценное управление WS (UI ролей, удаление участников из интерфейса) — позже; здесь
достаточно создать/выбрать, чтобы появился контекст воркспейса для пхп6.

## Обработка ошибок

- Создание WS: конфликт `slug` → дедуп суффиксом (`-2`, `-3`); имя пустое → 400.
- Username при регистрации: занят → 409 «username занят»; неверный формат → 400.
- Add member: юзер не найден → 404; уже состоит → 409; нет прав → 403 (если вызывающий
  состоит) / 404 (если не состоит в WS).
- Смена роли/удаление owner или несуществующего участника → 400/404 соответственно.
- Все мутации членства — в транзакции; гонка двойного добавления отсекается PK.

## Тестирование

- **Интеграция (testcontainers Postgres, как AS/DS/KT):** create WS → owner-строка и инвариант
  одного owner; ListForUser; add/remove/role с проверкой прав для каждой роли (таблица-матрица);
  резолв по email/username; уникальность username; миграция применяется чисто.
- **Unit:** генерация/дедуп slug; permission-хелпер (owner>admin>member) на табличных кейсах;
  валидация username-формата.
- **Фронт:** `workspace.ts` против fetch-мока (последовательность/ошибки); session-контекст WS;
  гейт create/select (презентационно, `@solidjs/testing-library`).
- Negative-authz: member не может добавлять; admin не может трогать admin/owner; не-член → 404.

## Что вне области этого плана/подпроекта

- Беседы, DM, каналы, поиск-директория, MLS-group флоу, KT-client — пхп6.
- Email-инвайты без аккаунта, email-сервис, accept-on-register — пхп7.
- Межворкспейс (Slack Connect), guest — пхп8.
- Скоупинг `conversations`/KT по `workspace_id`, передача владения, удаление WS с каскадом данных.
