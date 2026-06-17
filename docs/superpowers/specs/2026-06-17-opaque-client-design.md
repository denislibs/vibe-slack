# OPAQUE Client + Device Onboarding — дизайн (клиентская крипта, план 1)

**Дата:** 2026-06-17
**Статус:** утверждён
**Проект:** корпоративный мессенджер с E2E-шифрованием (production)

## Контекст

Парный к Authentication Service (AS) клиентский кусок: OPAQUE-аутентификация на фронте +
onboarding устройства, чтобы получить **device-bound** session-токен и реально подключить
WebSocket (`connect()`).

Готово: AS (`backend/internal/opaque`, `bytemare/opaque` v0.18.0, `DefaultConfiguration`),
HTTP-эндпоинты `/auth/register|login/start|finish`, `/devices`, `/keypackages`; DS-шлюз
требует device-bound сессию для WS; фронт-каркас (SolidJS+FSD, window-шина, крипто-воркер
MLS в `shared/lib/crypto`, протокольный воркер). Крипто-движок MLS уже даёт `key_package_bytes()`.

### Принятые решения

| Решение | Выбор | Обоснование |
|---|---|---|
| OPAQUE-клиент | Go `bytemare/opaque` клиент → WASM | Гарантированный интероп: идентичная библиотека+конфиг обеих сторон (как MLS OpenMLS→WASM); межреализационный OPAQUE хрупок и молча ломает вход |
| Объём | Auth + device onboarding | Доводит до device-bound сессии → реальный `connect()`; разблокирует всё |
| State OPAQUE | В WASM по flowId | bytemare-клиент stateful; держим инстанс в Go, не сериализуем наружу |
| crypto-core | + экспорт `signing_public_key()` | Нужен для `POST /devices` |
| Токен сессии | В памяти (не localStorage) | Меньше риск кражи при XSS; персистентность — будущий план |

## Область и границы

**В scope:**
- Go-клиент `bytemare/opaque` → WASM (`shared/lib/auth/`), ленивая загрузка на логине
- TS auth-клиент: пошаговый OPAQUE (WASM) + HTTP к AS
- Типизированный AS HTTP-клиент (register/login/devices/keypackages)
- Device onboarding: signing-pubkey + пул KeyPackages из крипто-воркера → `POST /devices` →
  device-bound сессия
- Session-entity (token, deviceId, статус) → фронт вызывает `connect(token)`
- Минимальная функциональная форма входа (не стилизованная)
- Доп. экспорт `signing_public_key()` в crypto-core (Rust→WASM)

**Out of scope (отдельные планы):**
- KT-client (проверка чужих ключей перед добавлением в группу)
- Клиентский MLS-group флоу (создание группы, добавление участников, Welcome)
- Стилизация/анимации экранов — подпроект 4
- Персистентность сессии между перезагрузками (шифрование локального состояния `export_key`)

## Архитектура и размещение (FSD)

| Слой/путь | Что |
|---|---|
| `shared/lib/auth/opaque-wasm/` | Go-модуль bytemare/opaque (клиент) + `wasm_exec.js`, сборка в `.wasm` |
| `shared/lib/auth/opaqueClient.ts` | загрузка Go-wasm + пошаговый API: regInit/regFinalize, loginKE1/loginKE3 |
| `shared/api/as.ts` | типизированный HTTP-клиент AS |
| `features/authenticate/` | use-case register/login → session-токен (WASM⇄HTTP) |
| `features/onboard-device/` | use-case: signing-pubkey + KeyPackages → POST /devices → device-bound |
| `entities/session/` | Solid-стор: token, deviceId, статус (anonymous/authenticated/onboarded) |
| `pages/auth/` + `widgets/login-form/` | минимальная форма (презентационная, prop-driven) |
| `app/orchestrator.ts` (доп.) | после onboarded — `protocol.connect(token)` |

Крипто-воркер расширяется экспортом `signing_public_key()` в `crypto-core` — Ed25519-публичный
ключ устройства для `/devices`. Бизнес-логика в `features`/`app`; UI — презентационный (FSD).

## OPAQUE-поток (WASM ⇄ HTTP)

bytemare-клиент stateful; Go-wasm хранит client-инстанс по `flowId`; HTTP-раунд-трипы — в TS.

**Регистрация:**
```
WASM regInit(password) → {flowId, registration_request}
  → POST /auth/register/start {email, opaque_registration_request} → {opaque_registration_response}
WASM regFinalize(flowId, response, serverIdentity="messenger-as") → {opaque_registration_record}
  → POST /auth/register/finish {email, opaque_registration_record} → {ok}
```
**Вход:**
```
WASM loginKE1(password) → {flowId, ke1}
  → POST /auth/login/start {email, ke1} → {login_id, ke2}
WASM loginKE3(flowId, ke2, serverIdentity) → {ke3, session_key}
  → POST /auth/login/finish {login_id, ke3} → {session_token, device_enroll_required}
```
`serverIdentity` фиксирован контрактом AS (`"messenger-as"`). Неверный пароль → KE3/LoginFinish
падает → единая ошибка (анти-энумерация уже на сервере). Все байтовые поля — base64 на проводе.

## Onboarding устройства

После `login` с `device_enroll_required=true`:
```
1. crypto-воркер: signing_public_key()  → Ed25519 pub устройства   (новый экспорт)
   crypto-воркер: key_package_bytes() ×N → пул one-time KeyPackages (есть)
2. POST /devices { signing_public_key, label, initial_key_packages[] }  (Bearer: session_token)
   → { device_id } — AS привязывает устройство к сессии (device-bound)
3. session-entity: статус → onboarded, deviceId сохранён
4. app-оркестратор: protocol.connect(session_token) — WS-шлюз пускает (device-bound)
```
Идемпотентность: если устройство уже привязано (повторный заход), enroll пропускается. Пул
KeyPackages пополняется при низком уровне (`GET /keypackages/count`); базовый пул — при onboarding.

## Session-entity, токен, ошибки

**Session-стор** (`entities/session`): `{ token, deviceId, status: anonymous|authenticated|onboarded }`.
Токен — в памяти (не localStorage): снижает риск кражи при XSS; перезагрузка = повторный вход.
Персистентность (шифрование локального состояния `export_key`-ом из OPAQUE) — будущий план.

**Ошибки:**
- Неверный пароль / неизвестный пользователь → `loginKE3`/`/login/finish` падает → единое
  «неверный email или пароль» (сервер неотличим).
- Конфликт регистрации (email занят) → `/register/finish` 409 → «аккаунт уже существует».
- Сбой загрузки WASM → ошибка инициализации auth, повтор.
- Onboarding-сбой (`/devices` 5xx) → статус остаётся authenticated (без connect), повтор enroll.

## Тестирование и сборка

**Сборка Go-wasm:** Go-модуль `shared/lib/auth/opaque-wasm`, `GOOS=js GOARCH=wasm go build`
→ `.wasm` + копия `wasm_exec.js` из Go toolchain. Vite отдаёт статикой; `opaqueClient.ts` грузит лениво.

**Тестирование:**
- **Интероп (главное):** Go-тест в opaque-wasm — экспортируемые клиентские функции (нативно,
  без wasm) против `bytemare/opaque` Server с `DefaultConfiguration`: полный register+login
  round-trip, **session_key клиента == server SessionSecret**. Доказывает байтовую совместимость
  с тем, что использует AS.
- **WASM smoke:** загрузка Go-wasm в node (через `wasm_exec.js`) — функции вызываются,
  regInit/loginKE1 дают непустые base64, два flow независимы по flowId.
- **TS auth-клиент:** против мок-AS (fetch-мок) — правильная последовательность эндпоинтов,
  проброс ошибок (неверный пароль, 409).
- **Onboarding:** мок крипто-воркера + мок-AS — `device_enroll_required` → POST /devices →
  статус onboarded → connect вызван.
- **E2E против живого AS (follow-up, heavy):** реальный Go AS (httptest) + TS-клиент через WASM —
  истинный кросс-процессный интероп.

**Заметка:** KSF в `DefaultConfiguration` (возможно Argon2id) memory-hard → в wasm логин может
занять ~секунду; приемлемо (вход нечастый), сверяется на этапе плана.

## Что вне области этого плана

- KT-client, клиентский MLS-group флоу — отдельные планы.
- Стилизация/анимации (подпроект 4).
- Персистентность сессии.
- Серверная авторизация ростера DS (M2) — не относится.
