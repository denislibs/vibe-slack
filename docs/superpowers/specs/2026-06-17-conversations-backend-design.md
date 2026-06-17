# Conversations Backend (DM + channels, workspace-scoped) — дизайн (подпроект 6, план 1 = 6.1)

**Дата:** 2026-06-17
**Статус:** утверждён
**Проект:** корпоративный мессенджер с E2E-шифрованием (production)

## Контекст

Подпроект 6 — «найти человека → написать»: директория, личные переписки (DM), каналы.
Решения пользователя: **DM + каналы сразу**; каналы **публичные и приватные (Slack-like)**;
каждая беседа = одна MLS-группа; **видимый комплаенс-участник** в каждой группе (решено в начале).

Подпроект 6 разбит на 3 под-плана:
- **6.1 (этот документ) — Беседы (бэкенд):** workspace-скоупинг бесед, метаданные DM/каналов,
  пользовательское членство + authz, поиск участников, серверная authz ростера (закрывает долг
  DS M2), починка bearer-над-WebSocket. **Без MLS — бэкенд её не парсит.**
- **6.2 — KT-client:** проверка ключей собеседника против KT-лога перед добавлением в группу.
- **6.3 — Клиентский MLS-group флоу + UI:** create group / addMember (KeyPackages+Welcome) /
  join-from-Welcome / **external commit для входа в публичный канал**; экраны DM/каналов.

Готово: DS (журнал `conversations(group_id TEXT PK, next_seq)`, `messages`, **device-level**
`conversation_members(group_id, device_id, join_seq)`, `device_cursors`); WS-шлюз требует
device-bound сессию, читает `Authorization: Bearer`. Workspaces (пхп5): `workspaces`,
`workspace_members(role)`, `users.username`, `internal/workspace` сервис, `FindByEmailOrUsername`.

### Ключевая развязка слоёв

DS-роутинг **device-level** и MLS-агностичен (`group_id` — непрозрачный ключ фан-аута,
`join_seq` для Welcome). Мы НЕ ломаем это. Поверх кладём **conversation-meta + user-membership**
(новый модуль `internal/conversations`), который держит тип/видимость/workspace/участников-юзеров
и **гейтит** device-ростер: мутировать device-роутинг беседы может только её участник-пользователь.

### Принятые решения

| Решение | Выбор | Обоснование |
|---|---|---|
| Где метаданные | Новая таблица `conversation_meta` + модуль `internal/conversations`, DS-журнал не трогаем | Не связываем DS с workspace-моделью; аддитивно |
| Беседа ↔ MLS | 1 беседа = 1 MLS-группа; DM = группа на 2 юзеров | Совпадает с готовым DS (`group_id`) |
| `group_id` | Сервер генерит UUID при создании беседы | Раньше был клиентский; теперь беседы создаются через API |
| Тип/видимость | `type: dm\|channel`, `visibility: public\|private` (для каналов) | Slack-модель по выбору пользователя |
| Членство-юзеры vs device-ростер | Два уровня: `conversation_user_members` (authz) + DS `conversation_members` (фан-аут устройств) | Authz — на юзерах; доставка — на устройствах |
| Вступление в публичный канал | На бэкенде = self-add юзера в членство (любой член WS); MLS-вход (external commit) — в 6.3 | Бэкенд MLS не знает |
| WS-аутентификация | Шлюз принимает и `Authorization: Bearer`, и `?access_token=` | Браузерный WS не шлёт заголовки — закрывает известный долг |

## Область и границы

**В scope (6.1, бэкенд):**
- Миграция `0005_conversations.sql`: `conversation_meta`, `conversation_user_members`.
- Модуль `internal/conversations` (repo + service + http): создать DM/канал, список «моих» бесед
  и видимых публичных каналов в WS, вступить в публичный канал, добавить/убрать участника-юзера.
- **Authz device-ростера**: существующие `POST/DELETE /conversations/{group}/members` (device-level)
  гейтятся — только участник-пользователь беседы может менять её device-роутинг (закрывает DS M2).
- Поиск участников WS по email/username (`GET /workspaces/{id}/members/search?q=`).
- Починка bearer-над-WebSocket в `internal/ws` (принимать `?access_token=`).
- Всё под device-bound сессией; всё скоупится по членству в workspace.

**Out of scope:**
- MLS-механика (createGroup/addMember/Welcome/external-commit) — клиент, 6.3.
- KT-проверка ключей — 6.2.
- Фронт-экраны бесед — 6.3.
- Реакции/треды/редактирование/пины/непрочитанное-как-счётчик — позже.
- Перенос истории при вступлении в публичный канал (MLS даёт ключи только с момента входа —
  это свойство, не баг; «история до входа» вне области).

## Данные (миграция `0005_conversations.sql`)

```sql
CREATE TABLE conversation_meta (
    group_id     TEXT PRIMARY KEY,                 -- = conversations.group_id (UUID-строка)
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    type         TEXT NOT NULL CHECK (type IN ('dm','channel')),
    visibility   TEXT NOT NULL CHECK (visibility IN ('public','private')),
    name         TEXT NOT NULL DEFAULT '',         -- пусто для DM
    created_by   UUID NOT NULL REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX conversation_meta_ws_idx ON conversation_meta(workspace_id);

CREATE TABLE conversation_user_members (
    group_id  TEXT NOT NULL REFERENCES conversation_meta(group_id) ON DELETE CASCADE,
    user_id   UUID NOT NULL REFERENCES users(id),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX conversation_user_members_user_idx ON conversation_user_members(user_id);

-- DM uniqueness: at most one DM per unordered user pair per workspace.
-- Enforced by a deterministic dm_key (sorted user ids) unique within the workspace.
ALTER TABLE conversation_meta ADD COLUMN dm_key TEXT;  -- null for channels
CREATE UNIQUE INDEX conversation_meta_dm_uniq ON conversation_meta(workspace_id, dm_key)
    WHERE dm_key IS NOT NULL;
```
DM `dm_key` = отсортированные `userA|userB`. Создание DM идемпотентно: повтор возвращает
существующую беседу. Создание беседы атомарно (meta + членства создателя/участников в одной tx),
плюс строка в DS `conversations` (через существующий путь создаёт первый коммит/первое сообщение;
в 6.1 достаточно завести `conversations.group_id` запись с `next_seq=0`).

## Модуль `internal/conversations`

- `store.ConversationRepo` (pgx): `CreateChannel(tx)`, `GetOrCreateDM`, `Get(groupID)`,
  `ListForUser(wsID, userID)` (членства + публичные каналы WS), `IsMember(groupID,userID)`,
  `AddUser`, `RemoveUser`, `Members(groupID)`; вставляет и строку в `conversations` (DS-журнал).
- `Service`: бизнес-правила и authz поверх `workspace.Service`/`store.WorkspaceRepo.RoleOf`
  (членство в WS) и собственного членства в беседе. Не знает HTTP/MLS.
- `httpapi` хендлеры под `authMW`.

## API (под device-bound сессией; всё проверяет членство в workspace)

| Метод | Путь | Кто | Действие |
|---|---|---|---|
| POST | `/workspaces/{wsId}/conversations` | член WS | `{type, visibility?, name?, target_user?}` → создать; DM идемпотентен по паре; вернуть `{group_id, type, visibility, name}` |
| GET | `/workspaces/{wsId}/conversations` | член WS | «мои» беседы (DM+каналы, где состою) + публичные каналы WS |
| GET | `/conversations/{group}` | член беседы / публичный-канал+член WS | метаданные + список участников-юзеров |
| POST | `/conversations/{group}/join` | член WS, канал public | вступить (self-add в `conversation_user_members`) |
| POST | `/conversations/{group}/users` | приватный: член беседы; публичный: член WS | `{email_or_username}` → добавить юзера в членство |
| DELETE | `/conversations/{group}/users/{userId}` | член беседы (себя — всегда; другого — создатель) | убрать из членства |
| GET | `/workspaces/{wsId}/members/search?q=` | член WS | поиск участников WS по email/username (префикс) |

**Authz device-ростера (закрытие DS M2):** `POST/DELETE /conversations/{group}/members`
(device-level, `roster_handlers.go`) теперь требует: вызывающий — участник-пользователь беседы
(`conversation_user_members`). Не член → 404 (без утечки существования). DS-доставка
(`/ws`, журнал) — без изменений по форме, но создание ростера гейтится.

## Правила и видимость

- **DM:** ровно 2 юзера (создатель + target), `visibility='private'`, `name=''`, идемпотентно по паре.
  Оба добавляются в членство при создании.
- **Публичный канал:** виден всем членам WS (в списке), любой член WS может `join`.
- **Приватный канал:** виден только участникам; добавлять может участник (создатель — точно).
- **Несостоящий + приватная беседа / чужой WS → 404** (не 403): не раскрываем существование.
- Удаление: себя — всегда (кроме DM — из DM не выходят в v1, беседа остаётся); другого юзера —
  создатель канала.
- **Workspace-скоуп везде:** беседа принадлежит одному WS; все операции проверяют членство
  вызывающего в этом WS до действия.

## Bearer-над-WebSocket (починка известного долга)

`internal/ws` gateway: при upgrade брать токен из `Authorization: Bearer`, **а если его нет —
из query `?access_token=`** (браузерный WS не умеет заголовки). Затем как сейчас — `sess.Validate`.
Это оживляет фронтовый `connect(token)`. (OriginPatterns/InsecureSkipVerify — как есть, отдельный долг.)

## Обработка ошибок

- Создание беседы: не член WS → 404; DM с самим собой → 400; target не член WS → 400/404;
  гонка DM → idемпотентно вернуть существующую (по `dm_key` unique).
- Join: канал приватный или не существует → 404; уже состоит → 200 (идемпотентно) или 409.
- Add user: target не найден/не член WS → 404; нет прав → 403 (если состоит) / 404 (если нет).
- Device-роутинг от не-участника → 404.

## Тестирование

- **Интеграция (testcontainers Postgres):** создать DM (идемпотентность по паре); создать public/
  private канал; ListForUser показывает «мои» + публичные, скрывает чужие приватные; join public;
  add/remove user с authz по ролям; поиск по email/username (префикс, скоуп WS); миграция чисто.
- **Authz device-ростера:** не-участник беседы не может добавить/убрать device (404); участник может.
- **WS-токен:** unit на gateway — токен из заголовка ИЛИ из `?access_token=`, оба валидируются;
  отсутствие/битый → отказ upgrade.
- **Negative/cross-tenant:** член WS-A не видит/не трогает беседы WS-B (404).

## Что вне области этого плана

- KT-client (6.2), клиентский MLS-флоу + external commit + UI (6.3).
- Email-инвайты (пхп7), межворкспейс-каналы (пхп8), UI/UX (пхп4).
- Скоуп `messages`/курсоров по workspace (журнал и так под group_id, доступ гейтится членством).
