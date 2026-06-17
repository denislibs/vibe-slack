# UI: Slack-подобный интерфейс + темы + анимации + живой чат — дизайн (подпроект 4 ⊕ 6.3c)

**Дата:** 2026-06-17
**Статус:** утверждён
**Проект:** корпоративный мессенджер с E2E-шифрованием (production)

## Контекст

Весь движок готов (крипто/MLS, OPAQUE, KT, бэкенд AS/DS/KT, мульти-тенант, беседы, клиентская
оркестрация). Фронт — голый функциональный каркас без стилей. Цель: превратить его в визуально
полноценный **Slack-подобный** мессенджер, подключить реальные беседы (6.3c — закрывает ошибку
`unknown group`), оживить доставку по WebSocket, и добавить **плавные анимации (Solid Transitions)**
в духе Telegram-ощущения. Решения пользователя: **тёмная тема по умолчанию + переключатель на светлую**;
**близкий клон Slack**; анимации/переходы (Solid Transitions); онбординг; сообщения должны реально летать.

### Принятые решения

| Решение | Выбор |
|---|---|
| Эталон вида | Близкий клон Slack (раскладка/плотность/иконки/левый workspace-rail) |
| Темы | Тёмная по умолчанию + светлая; toggle; выбор в localStorage; токены через CSS-переменные на `data-theme` |
| Анимации | Богатые, `solid-transition-group` (Solid Transitions): переходы экранов, появление сообщений, модалки, hover/active, смена темы |
| Стили | CSS-модули или один глобальный токен-слой + компонентные классы; без тяжёлых UI-фреймворков (свой ui-kit в `shared/ui`) |
| Чистая архитектура | UI презентационный (prop-driven); бизнес-логика в app/features (как и было); НЕ деструктурировать Solid-пропсы |
| WS | Прокинуть DS WS URL в протокол-воркер; коннект `?access_token=` (прокси уже форвардит /ws); online-статус; реальный send/receive |

## Палитра (близко к Slack; конкретные токены, тюнятся вживую)

CSS-переменные на `:root[data-theme="dark"]` и `[data-theme="light"]`:

**Dark (по умолчанию):**
```
--rail-bg:#15101a; --sidebar-bg:#1a1320; --sidebar-text:#cfc3d6; --sidebar-muted:#9b8fa6;
--sidebar-active-bg:#5b2e91; --sidebar-active-text:#ffffff; --sidebar-hover:rgba(255,255,255,.06);
--main-bg:#1a1d21; --text:#e8e8e9; --text-muted:#9a9b9e; --border:#2c2d30;
--accent:#7c3aed; --accent-text:#ffffff; --composer-bg:#222529; --topbar-bg:#3a1d4d; --danger:#e01e5a;
```
**Light:**
```
--rail-bg:#3f0e40; --sidebar-bg:#4a154b; --sidebar-text:#e8d9ea; --sidebar-muted:#bda9bf;
--sidebar-active-bg:#1164a3; --sidebar-active-text:#ffffff; --sidebar-hover:rgba(255,255,255,.10);
--main-bg:#ffffff; --text:#1d1c1d; --text-muted:#616061; --border:#e2e2e2;
--accent:#007a5a; --accent-text:#ffffff; --composer-bg:#ffffff; --topbar-bg:#350d36; --danger:#e01e5a;
```
Радиусы/тени/spacing-токены тоже в переменных (`--radius`, `--space-*`, `--font`).

## Раскладка (Slack-clone, три колонки + topbar)

```
┌──────────────────────────── topbar (search, workspace name) ────────────────────────────┐
├──────┬──────────────────────┬────────────────────────────────────────────────────────────┤
│ rail │  sidebar             │  conversation pane                                          │
│ (WS) │  • workspace header  │  ┌ header: #channel / @dm name · members ─────────────────┐ │
│  SI  │  • Channels          │  │                                                          │ │
│  +   │    # general …       │  │  message list (grouped by sender, avatar+name+time,      │ │
│      │  • Direct messages   │  │  E2E-decrypted text; «no messages» empty state)          │ │
│ ☾    │    @alice (you) …    │  │                                                          │ │
│ ava  │  • + Create / search │  └ composer: input + send (Enter to send) ─────────────────┘ │
└──────┴──────────────────────┴────────────────────────────────────────────────────────────┘
```
- **Rail:** workspace initials (switch между «моими» WS), `+` создать WS, theme-toggle (☀/☾), аватар пользователя.
- **Sidebar:** заголовок воркспейса; список каналов (public/private иконки # / 🔒); список DM; «+ Create channel» и «New message» (поиск участников WS); активная беседа подсвечена.
- **Pane:** шапка беседы (имя, тип, кол-во участников, «add people»); список сообщений; composer.
- **Topbar:** имя воркспейса, поиск (заглушка/локальный фильтр в v1), меню пользователя.

## 6.3c — подключение к реальным беседам (закрывает `unknown group`)

- Sidebar тянет `ConversationsClient.list(token, wsId)` → группирует на каналы/DM.
- **Создать канал:** модалка (имя + public/private) → `createConversation` use-case (MLS-группа + комплаенс + publish GroupInfo) → беседа появляется в списке, выбирается.
- **New DM:** поиск участников WS (`/members/search`) → выбор → `createConversation(type:dm)` → DM.
- **Выбор беседы** ставит активный `groupId` (вместо заглушки `"g1"`); ChatPage рендерит сообщения этой группы; composer шлёт в эту группу (encrypt→DS). Это устраняет `unknown group`.
- **Добавить в канал:** в шапке «add people» → поиск → `addMember` (KT-verify + MLS add).
- **Вступить в public-канал:** видимые публичные каналы WS, которых нет в членстве → «Join» → `joinPublic`.

## WS — живая доставка (online)

- Прокинуть WS-URL в протокол-воркер: same-origin `/ws?access_token=<token>` (vite-прокси форвардит на бэкенд; `ws:true` уже настроен). В app/bootstrap/env завести `DS_WS_URL` (по умолчанию относительный `/ws`), воркер строит `new WebSocket(wsUrl + "?access_token=" + token)`.
- После логина: `connect(token)` поднимает сокет → connection-стор → online-статус в UI.
- Входящие `message`-фреймы → decrypt → conversation-стор → рендер (через готовый orchestrator); commit/welcome — как в 6.3b. Исходящие — encrypt → send. Проверить руками: два окна/два юзера обмениваются.

## Анимации (Solid Transitions)

- `solid-transition-group`: переходы экранов онбординг↔workspace↔chat (fade/slide); смена активной беседы (cross-fade pane); появление новых сообщений (slide-up+fade, TransitionGroup); модалки (scale+fade+backdrop); sidebar item hover/active (CSS-transition); смена темы (плавный переход цветов); toast/ошибки. Уважать `prefers-reduced-motion`.

## Онбординг

- Стилизованный auth (центрированная карточка, логотип, поля Email/Username/Password, переключение Вход/Регистрация — табы, не две кнопки в ряд); понятная ошибка (логировать настоящую, не «invalid email or password» на всё).
- Стилизованный первый запуск: создать/выбрать воркспейс → подсказка «создай первый канал».
- Аватар/инициалы пользователя из username.

## Декомпозиция (планы)

1. **UI-1 — Design system + темы + ui-kit.** Токены (dark/light) на `data-theme`, theme-стор (localStorage+toggle, дефолт dark), глобальные стили/шрифты, стилизованные `shared/ui`: Button, IconButton, Input, Avatar (инициалы+цвет по имени), Modal, Spinner, Tooltip. Тесты: рендер + тема переключается (атрибут на root).
2. **UI-2 — App shell + Slack-раскладка.** Rail + sidebar + pane + topbar (презентационные, с пропсами), responsive grid, скролл-зоны; theme-toggle и аватар в rail. Тесты презентационные.
3. **UI-3 — Беседы (6.3c) подключены.** Sidebar из реального list; модалки create-channel / new-DM (поиск); выбор беседы → активный groupId; composer шлёт в реальную группу; message-list (группировка, аватар, время); join public; add people. Use-cases create/add/join подключены. Снимает `unknown group`.
4. **UI-4 — WS живая доставка.** WS-URL в воркер, connect после логина, online-статус, реальный send/receive; ручной двухоконный тест.
5. **UI-5 — Анимации + онбординг-полиш.** solid-transition-group везде (экраны/сообщения/модалки/тема), `prefers-reduced-motion`, стилизованный онбординг (табы вход/регистрация, нормальные ошибки), пустые состояния. Финальный визуальный проход.

## Тестирование

- Презентационные тесты `@solidjs/testing-library` (рендер, пропсы, тема, не-деструктуризация пропсов сохраняет реактивность); use-case-интеграция (выбор беседы → encrypt в нужную группу; создание → list обновляется); orchestrator inbound; live-проверка вручную в браузере (стек уже поднят: фронт :5173, бэк :18080). FSD-границы: бизнес-логика вне UI.
- Каждый план: `npx vitest run && npx tsc --noEmit && npx vite build` зелёные; визуальный просмотр вживую.

## Вне области

- Реакции/треды/редактирование/файлы/пины, голос/видео (huddles), уведомления, поиск по сообщениям (только локальный фильтр), мобильная адаптация сверх базовой, email-инвайты (пхп7), Slack Connect (пхп8). Персистентность сессии.
