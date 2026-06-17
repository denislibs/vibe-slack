# Frontend Scaffold (three threads + clean architecture) — дизайн (подпроект 3, план 1)

**Дата:** 2026-06-17
**Статус:** утверждён
**Проект:** корпоративный мессенджер с E2E-шифрованием (production)

## Контекст

Подпроект 3 — фронтенд (SolidJS, трёхпоточная архитектура как web Telegram). Этот документ —
**первый план: каркас** (архитектура, три потока, оболочка, транспорт-демо). Реальный UI/UX
и анимации — подпроект 4.

Текущее состояние `client/`: реализован только **крипто-поток** (из подпроекта 1) —
`client/src/crypto/` (`CryptoClient`, Web Worker с WASM MLS-движком, `protocol.ts`, e2e-тест,
собранный `wasm-pkg/`). Тулинг: TypeScript + Vitest. Нет SolidJS, Vite, UI, протокольного воркера.

Бэкенд готов: DS определил сокет-протокол (`send/ack/sync` → `sent/message/error`,
аутентификация по session-токену) — контракт протокольного воркера разблокирован.

### Директивы пользователя (зафиксированы)

- Межпоточное общение — через `window`/`postMessage` (типизированная шина, главный поток — хаб).
- Чистая архитектура: вся бизнес-логика вне UI-компонентов.
- UI-компоненты — в shared-слой (ui-kit), переиспользуемые.

### Принятые решения

| Решение | Выбор | Обоснование |
|---|---|---|
| Методология слоёв | Feature-Sliced Design (FSD) | Строгие границы импортов; `shared/ui` = ui-kit; бизнес-логика в entities/features |
| Межпоточная шина | window — хаб (postMessage), не worker↔worker MessageChannel | Простая единая типизированная шина; главный поток оркестрирует |
| Срез первого плана | Архитектура + 3 потока + транспорт-демо | Автономно; не блокируется на OPAQUE-client/MLS-group флоу |
| Крипто-воркер | Переезжает в `shared/lib/crypto` | Чисто по слоям FSD (инфраструктура) |
| Сборка | Vite + vite-plugin-solid | Стандарт для SolidJS; нативная поддержка воркеров |

## Область и границы

**В scope (каркас фронта):**
- Vite + SolidJS + строгий FSD-скелет, ui-kit
- Типизированная window-шина (главный поток — хаб; компоненты к воркерам напрямую не ходят)
- Протокольный воркер (2-й поток): WebSocket-клиент к DS — `send/ack/sync` → `sent/message/error`,
  bearer-аутентификация, очередь отправки, реконнект с backoff, sync с курсора
- Крипто-воркер (3-й поток): уже готов — встраивается в шину (переезд в `shared/lib/crypto`)
- Оркестрация: входящий шифртекст → шина → крипто-воркер decrypt → entity-стор → UI; обратно —
  intent → encrypt → send
- Транспорт-демо как vertical slice + тесты

**Out of scope (отдельные планы):**
- OPAQUE-client (экран входа), KT-client (проверка ключей), клиентский MLS-group флоу
  (Welcome/добавление участников) — потребляются фронтом, делаются отдельно
- Реальный UI/UX, анимации, Solid Transitions — подпроект 4
- Конкретный визуальный дизайн чата

## Архитектура: три потока + window-шина

```
┌─────────────── UI thread (window) ───────────────┐
│  SolidJS (FSD) + Orchestration (app-слой)          │
│      ▲ render/intent        ▲ store updates        │
│   ui-kit / widgets      entities/features          │
│            │ (через app-оркестратор, не напрямую)  │
│            ▼                                        │
│   ┌─────────── MessageBus (shared/lib) ───────────┐│
│   │  window — хаб; postMessage к каждому воркеру   ││
│   └───────┬───────────────────────────┬───────────┘│
└───────────│───────────────────────────│────────────┘
            ▼ postMessage               ▼ postMessage
   ┌──────────────────┐        ┌──────────────────────┐
   │ Protocol worker  │        │  Crypto worker (WASM) │
   │  WebSocket ↔ DS  │        │  MLS encrypt/decrypt  │
   └──────────────────┘        └──────────────────────┘
```

- Чистая архитектура: компоненты только рендерят и шлют intents; вся логика (оркестрация
  шины, бизнес-сценарии) — в `app`/`features`/`entities`, не в UI.
- Шина даёт каждому воркеру типизированный API: request/response (как уже есть у `CryptoClient`)
  + поток событий (входящие `message` от proto-воркера).

## FSD-слои и где что лежит

```
client/src/
  app/        # bootstrap: создание воркеров, сборка шины, провайдеры, роутер, DI-композиция
  pages/      # экраны (chat) — композиция виджетов
  widgets/    # композитные UI-блоки (message-list, composer) — презентационные
  features/   # сценарии (use-cases): send-message, sync-conversation, connect-transport — БИЗНЕС-ЛОГИКА
  entities/   # доменные модели + Solid-сторы: message, conversation, session/device
  shared/
    ui/       # UI-KIT: чистые презентационные компоненты (button, input, avatar, spinner)
    lib/      # MessageBus, worker-клиенты, transport-примитивы, crypto/ (переезд из src/crypto)
    api/      # контракты/DTO: WS-фреймы DS, протокол крипто-воркера
    config/   # env, константы
```

Строгие правила импортов FSD (только вниз по слоям). `shared/ui` (ui-kit) не знает о доменах.
Бизнес-логика недопустима в `shared/ui`, `widgets`, `pages` — только в `features`/`entities`/`app`.

## Протокольный воркер + протокол шины

**Протокольный воркер** (`shared/lib/transport/protocol.worker.ts`) — WebSocket-клиент к DS:
- Подключение на `/ws` с `Authorization: Bearer <session_token>` (токен из app-слоя; в демо — из тест-стенда/мока)
- Исходящее: `send {client_msg_id, group_id, content_type, ciphertext}` с локальной очередью
  (буфер при оффлайне/реконнекте) и сопоставлением `sent {seq}`
- Входящее: `message {group_id, seq, sender_device, content_type, ciphertext}` → событие в шину;
  `error` → событие
- `ack {up_to_seq}` при обработке; `sync {since_seq}` при подключении/реконнекте (догон с курсора
  из entity-стора)
- Реконнект с backoff; на reconnect — повторная подписка + sync

**Контракт шины** (`shared/lib/bus`): типизированные конверты, главный поток маршрутизирует.
- Worker-клиент (request/response): `call(kind, payload) → Promise<result>` (как `CryptoClient`)
- Worker-клиент (события): `on(event, handler)` (входящие message/error/status)
- `ProtocolClient` (обёртка proto-воркера): `connect(token)`, `send(...)`, `sync(...)`,
  `onMessage(...)`, `onStatus(...)`
- `CryptoClient` (уже есть): `encrypt/decrypt/...`

Компоненты не импортируют клиентов — только app-оркестратор и features.

## Поток данных и состояние

**Состояние** — Solid-сторы в `entities`:
- `conversation`: сообщения по `group_id` (отсортированы по `seq`), последний `acked_seq` (курсор), статус
- `connection`: online/connecting/offline

**Входящий поток:** proto-воркер → `message{ciphertext}` → шина → app-оркестратор →
`CryptoClient.decrypt` → plaintext → `conversation`-стор → реактивный рендер. Затем `ProtocolClient.ack(seq)`.

**Исходящий:** UI intent (`send-message`) → `CryptoClient.encrypt` → `ProtocolClient.send` →
`sent{seq}` обновляет стор.

**Транспорт-демо (vertical slice):** поднять JS-мок DS WebSocket-сервер; протокольный воркер
коннектится, получает `message`; оркестратор гонит шифртекст в крипто-воркер (реальный MLS: в
тесте заранее установлена группа alice↔bob через готовый движок); расшифрованный текст попадает
в стор; минимальный компонент рендерит. Доказывает window-шину + оба воркера + чистое разделение слоёв.

## Тулинг и тестирование

**Тулинг:** Vite + `vite-plugin-solid`, dev-сервер, `vite build`. Воркеры —
`new Worker(new URL('...', import.meta.url), {type:'module'})`. Зависимости: `solid-js`, `vite`,
`vite-plugin-solid`, `@solidjs/testing-library`, `jsdom`/`happy-dom` (уже есть `vitest`, `typescript`).

**Тестирование:**
- Unit: ui-kit компоненты (`@solidjs/testing-library` + jsdom); маршрутизация шины; сторы entities.
- Протокольный воркер: против JS-мок WebSocket-сервера — фреймы `send/sent/message`, очередь при
  оффлайне, реконнект+sync, ack двигает курсор.
- E2E-пайплайн (vertical slice): реальный крипто-воркер + мок proto/мок-WS: входящее `message` →
  decrypt → стор → рендер; исходящее intent → encrypt → send.
- FSD-границы: проверка правил импортов (слои только вниз) — линт-плагин или ревью.

## Обработка ошибок

- Нет токена / отказ upgrade WS → статус offline, повтор с backoff; UI показывает «не подключено».
- Воркер падает/недоступен → шина возвращает ошибку запроса; оркестратор логирует, статус деградирует.
- Decrypt-ошибка (нет ключей/эпоха) → сообщение помечается как нерасшифрованное, не роняет поток
  (догон через sync/последующие коммиты).
- Backpressure/реконнект: при разрыве — очередь исходящих сохраняется, на reconnect — sync с курсора.

## Что вне области этого плана/подпроекта

- OPAQUE-client, KT-client, клиентский MLS-group флоу — отдельные планы.
- UI/UX, анимации, Solid Transitions, визуальный дизайн — подпроект 4.
- Серверная часть — подпроект 2 (готова: AS/DS/KT).
