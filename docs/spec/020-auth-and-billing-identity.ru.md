# 020. Auth and Billing Identity

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение документа

Этот документ описывает authentication и identity boundary Tokenio Gateway.

Документ фиксирует:

```text
public user API key contract
API key validation
API key storage
user identity
billing identity
billing JWT generation
billing service auth
reseller auth separation
admin auth boundary
```

Документ не описывает:

```text
route selection
pricing
usage extraction
ledger state machine
admin API endpoints
database migrations in detail
```

Эти темы описываются в отдельных спецификациях.

---

# 2. Главный инвариант identity layer

Tokenio Gateway использует разные credentials для разных trust boundaries.

```text
Client -> Tokenio Gateway:
  openai_compatible -> Authorization: Bearer sk_...
  anthropic_native  -> x-api-key: sk_...
  gemini_native     -> x-goog-api-key: sk_...
  ollama_native     -> Authorization: Bearer sk_...

Tokenio Gateway -> Billing Service balance:
  Authorization: Bearer <billing_jwt>

Tokenio Gateway -> Billing Service charge:
  X-Service-Token: <service_token>

Trusted Provisioning Caller -> Tokenio Gateway:
  X-Service-Token: <TOKENIO_PROVISIONING_SERVICE_TOKEN>

Tokenio Gateway -> Reseller:
  Authorization/API key from route.reseller.api_key_env
```

Эти credentials нельзя смешивать.

Запрещено:

```text
передавать user API key в billing service
передавать user API key reseller
передавать billing JWT клиенту
передавать billing JWT reseller
использовать reseller API key как user auth
использовать client JWT как public auth contract
использовать billing JWT как provisioning credential
использовать billing service charge token как provisioning credential
```

---

# 3. Trust boundaries

## 3.1. Public boundary

Public boundary — это внешний HTTP API Tokenio Gateway.

Native API family transport adapters accept only the explicitly approved
carrier for their family:

```text
openai_compatible -> Authorization: Bearer sk_...
anthropic_native  -> x-api-key: sk_...
gemini_native     -> x-goog-api-key: sk_...
ollama_native     -> Authorization: Bearer sk_...
```

Каждый transport adapter проверяет только форму carrier и нормализует значение
в один Tokenio `sk_...` credential. Hash lookup, API-key state и user state
проверяются shared application use case.

Запрещено:

```text
принимать Gemini key через URL query;
передавать carrier name в application business logic;
принимать carrier другой API family;
выбирать credential по скрытому precedence;
forward-ить inbound Tokenio credential upstream.
```

JWT от клиента не является публичным auth contract.

Полный contract определён ADR:

```text
docs/adr/0002-native-api-auth-carriers.md
```

## 3.2. Billing boundary

Billing boundary — это внутреннее взаимодействие:

```text
Tokenio Gateway -> Billing Service
```

Для запроса баланса пользователя используется billing JWT.

Для списания usage используется internal service token.

## 3.3. Upstream boundary

Upstream boundary — это взаимодействие:

```text
Tokenio Gateway -> Reseller / Provider
```

На этом boundary используется credential из environment variable, указанной в `reseller.api_key_env`.

## 3.4. Provisioning boundary

Provisioning boundary — это internal service-to-service взаимодействие после подтверждённой оплаты:

```text
Trusted Billing/Payment Caller -> Tokenio Gateway
```

На этом boundary используется отдельный:

```http
X-Service-Token: <TOKENIO_PROVISIONING_SERVICE_TOKEN>
```

Retry-safe delivery contract описан в:

```text
docs/spec/021-api-key-provisioning.ru.md
```

## 3.5. Admin boundary

Admin boundary — это управление Tokenio Gateway:

```text
users
api_keys
resellers
routes
prices
balances
cooldowns
```

Admin auth описывается в `docs/spec/060-admin-api.ru.md`.

---

# 4. User API key

## 4.1. Формат

User API key должен передаваться так:

```http
Authorization: Bearer sk_...
```

Минимальный допустимый prefix:

```text
sk_
```

Рекомендуемые production prefixes:

```text
sk_live_
sk_test_
```

`sk_live_` используется для production keys.
`sk_test_` может использоваться для sandbox/test окружения, если оно будет добавлено.

Первая версия может принимать любой key, начинающийся с:

```text
sk_
```

## 4.2. Raw key

Raw API key показывается пользователю только при initial delivery.

Raw API key не хранится plaintext.

Permanent auth storage `tokenio_api_keys` хранит только HMAC hash.

Для retry-safe initial delivery допускается отдельная временная encrypted copy только в `tokenio_api_key_provisionings` и только пока provisioning имеет status `pending_delivery`.

После delivery confirmation или expiration encrypted copy удаляется.

Raw API key не логируется.

После delivery confirmation raw API key не возвращается через API.

## 4.3. Key entropy

API key должен содержать cryptographically secure random value.

Минимум:

```text
32 random bytes
```

Рекомендуемый формат:

```text
sk_live_<base64url-or-hex-random>
```

## 4.4. Key prefix для отображения

Для admin UI/API можно хранить безопасный display prefix.

Пример:

```text
sk_live_abcd...
```

Display prefix не должен быть достаточным для аутентификации.

---

## 4.5. Payment-triggered provisioning

После подтверждённой оплаты Billing Service, Telegram bot backend или другой trusted payment orchestrator может запросить initial API key через internal provisioning API.

Provisioning:

```text
не использует public user JWT
не использует user sk_...
не использует admin token
использует отдельный service credential
```

Provisioning обязан поддерживать same-key retry при потере HTTP response.

Полный contract:

```text
docs/spec/021-api-key-provisioning.ru.md
```

# 5. API key storage

## 5.1. Permanent and temporary storage

В permanent auth table `tokenio_api_keys` запрещено хранить raw API key или reversible encrypted API key.

Permanent auth record хранит только HMAC hash.

Единственное допустимое reversible storage — отдельный temporary provisioning record согласно `docs/spec/021-api-key-provisioning.ru.md`.

Temporary encrypted copy:

```text
существует только для pending_delivery
не является authentication source of truth
удаляется при delivered или expired
```

## 5.2. Hashing

Обязательное требование:

```text
key_hash = HMAC-SHA256(TOKENIO_API_KEY_HASH_SECRET, raw_api_key)
```

Secret задаётся через environment variable:

```text
TOKENIO_API_KEY_HASH_SECRET
```

SHA-256 без secret запрещён как fallback.

Причина:

```text
raw API key является bearer credential;
hashing должен быть keyed, чтобы утечка БД не позволяла offline matching без TOKENIO_API_KEY_HASH_SECRET.
```


Инвариант:

```text
generic auth layer работает только с hash;
raw key существует только в момент parsing/validation.
```

## 5.3. Constant-time comparison

Сравнение hash должно выполняться constant-time comparison.

Запрещено:

```text
обычное string == comparison для секретов
ранний выход по первому несовпадающему символу
логирование expected/actual hash
```

## 5.4. API key record

Минимальная модель API key:

```text
id
user_id
name
key_hash
key_prefix
enabled
created_at
last_used_at
revoked_at
expires_at
```

`expires_at` может быть nullable.

Если `expires_at` задан и меньше текущего времени, key считается недействительным.

## 5.5. API key status

API key считается valid только если:

```text
key exists
enabled = true
revoked_at IS NULL
expires_at IS NULL OR expires_at > now()
user exists
user.enabled = true
```

---

# 6. User identity

## 6.1. User record

Tokenio Gateway должен иметь локальную user identity, достаточную для auth, billing mapping и admin operations.

Минимальная модель user:

```text
id
external_billing_user_id
email
name
enabled
created_at
updated_at
disabled_at
```

`external_billing_user_id` нужен, если billing service использует отдельный user id.

Если billing service использует тот же `user_id`, поле может быть равно `id`.

## 6.2. Disabled user

Если user отключён:

```text
HTTP 403
error.code = user_disabled
```

Отключённый user не может:

```text
вызывать /v1/models
вызывать LLM endpoints
создавать новые API keys
использовать старые API keys
```

---

# 7. Public auth validation flow

Для каждого auth-required endpoint Tokenio Gateway выполняет:

```text
1. Прочитать Authorization header.
2. Проверить формат Bearer.
3. Проверить prefix sk_.
4. Захешировать raw key.
5. Найти API key record.
6. Проверить key enabled/revoked/expired.
7. Найти user.
8. Проверить user enabled.
9. Обновить last_used_at асинхронно или best-effort.
10. Вернуть AuthPrincipal.
```

## 7.1. `last_used_at` mutation boundary

Consumer-side lookup и usage mutation являются разными ports:

```text
APIKeyRepository.FindByHash
APIKeyUsageRecorder.RecordLastUsedAt
```

`RecordLastUsedAt` принимает только:

```text
api_key_id
used_at в UTC
```

Передавать в mutation raw API key или `key_hash` запрещено.

Persistence contract:

```text
last_used_at изменяется монотонно;
более старый used_at является успешным no-op;
unknown key не создаётся;
disabled key не обновляется;
revoked key не обновляется;
key с expires_at <= used_at не обновляется;
upsert и INSERT в mutation запрещены.
```

Если запись отсутствует или не является допустимой для фиксации
использования, persistence adapter возвращает generic `not found`.
Он не раскрывает caller-у конкретную причину.

Application orchestration, timeout и best-effort policy задаются
отдельно. Persistence adapter не создаёт goroutine, queue или retry.

## 7.2. Bounded best-effort orchestration

Application layer оборачивает основной authentication use case
отдельным decorator:

```text
core authentication
→ successful AuthPrincipal
→ bounded RecordLastUsedAt
→ вернуть тот же AuthPrincipal
```

Mutation выполняется только после успешной проверки key, user и
billing identity.

Policy:

```text
recorder error не отменяет успешную authentication;
recorder error не включается в public response;
mutation context имеет обязательный короткий timeout;
client cancellation не оставляет mutation без собственного deadline;
один request не создаёт goroutine;
unbounded queue и background retry запрещены;
used_at получается из injected UTC clock;
```

Timeout задаётся configuration contract:

```text
TOKENIO_API_KEY_LAST_USED_TIMEOUT
```

Core authentication use case остаётся единственным source of truth
для решения, является ли credential допустимым. Decorator не повторяет
key/user validation.

Минимальная структура AuthPrincipal:

```text
user_id
api_key_id
billing_subject_user_id
```

`AuthPrincipal` не должен содержать `billing_jwt`.

Billing JWT строится отдельным billing identity service перед вызовом billing service.

Причина:

```text
auth layer отвечает за user identity;
billing identity layer отвечает за billing JWT;
эти ответственности нельзя смешивать.
```

---

# 8. Auth errors

## 8.1. Missing Authorization

```text
HTTP 401
error.code = unauthorized
error.message = Authorization header is required
```

## 8.2. Invalid Authorization scheme

```text
HTTP 401
error.code = unauthorized
error.message = Authorization header format must be Bearer {api_key}
```

## 8.3. Invalid API key prefix

```text
HTTP 401
error.code = unauthorized
error.message = API key must start with sk_
```

## 8.4. Unknown API key

```text
HTTP 401
error.code = invalid_api_key
error.message = Invalid API key
```

## 8.5. Disabled/revoked/expired API key

```text
HTTP 401
error.code = invalid_api_key
error.message = Invalid API key
```

Gateway не должен раскрывать, был key неизвестен, отключён, отозван или истёк.

## 8.6. Disabled user

```text
HTTP 403
error.code = user_disabled
error.message = User is disabled
```

---

# 9. Billing identity

## 9.1. Billing subject

Billing subject — identity пользователя в billing service.

Минимально:

```text
billing_subject_user_id = user.external_billing_user_id OR user.id
```

Billing subject используется для построения billing JWT.

## 9.2. Billing JWT

Billing JWT — internal token для запроса баланса пользователя.

Он используется только для:

```text
Tokenio Gateway -> Billing Service
GET /api/v1/wallet/balance
```

Billing JWT не используется для user auth на public API.

Billing JWT не возвращается клиенту.

## 9.3. Billing JWT claims

Минимальные claims:

```json
{
  "user_id": "<billing_subject_user_id>",
  "iss": "tokenio-gateway",
  "aud": "billing-service",
  "iat": 0,
  "exp": 0
}
```

Допустимые дополнительные claims:

```text
email
role
org_id
```

Только если billing service требует эти claims.

## 9.4. Billing JWT signing

Billing JWT подписывается gateway через:

```text
TOKENIO_BILLING_JWT_SIGNING_KEY
```

Алгоритм первой версии:

```text
HS256
```

Если billing service позже потребует asymmetric JWT, это должно быть отдельное ADR.

## 9.5. Billing JWT TTL

Billing JWT должен иметь короткий TTL.

Рекомендуемое значение:

```text
15 минут
```

Config key:

```text
TOKENIO_BILLING_JWT_TTL
```

Default:

```text
15m
```

## 9.6. Billing JWT cache

Gateway может cache-ить billing JWT внутри процесса.

Cache key:

```text
user_id + billing_subject_user_id
```

Cache TTL не должен превышать JWT expiration.

Для одного instance достаточно in-memory cache.

---

# 10. Billing service calls

## 10.1. Balance request

Для проверки баланса пользователя gateway вызывает billing service:

```http
GET /api/v1/wallet/balance
Authorization: Bearer <billing_jwt>
```

Ответ billing service должен содержать:

```json
{
  "currency": "RUB",
  "balance_cents": 10000
}
```

Если billing service недоступен на этапе preflight balance check, gateway возвращает:

```text
HTTP 502
error.code = billing_unavailable
```

Если локальный cached balance и pending ledger позволяют принять решение, gateway может использовать local effective balance согласно ledger specification.

## 10.2. Charge request

Для списания usage gateway вызывает billing service:

```http
POST /api/v1/usage/charge
X-Service-Token: <TOKENIO_BILLING_SERVICE_TOKEN>
Idempotency-Key: <billing_charge_request_id>
Content-Type: application/json
```

Body:

```json
{
  "request_id": "<billing_charge_request_id>",
  "user_id": "<billing_subject_user_id>",
  "model": "openrouter:gpt-4.1-mini",
  "input_tokens": 1000,
  "output_tokens": 500,
  "amount_cents": 123,
  "currency": "RUB"
}
```

`model` всегда формируется как:

```text
provider_type:client_model
```

## 10.3. Charge idempotency

`billing_charge_request_id` должен быть stable для набора usage records, которые списываются.

Повторный charge с тем же `billing_charge_request_id` не должен приводить к двойному списанию.

Gateway обязан передавать:

```http
Idempotency-Key: <billing_charge_request_id>
```

---

# 11. Separation from reseller auth

Reseller auth полностью отделён от user auth и billing auth.

Reseller credential берётся из:

```text
reseller.api_key_env
```

Пример:

```text
api_key_env = OPENROUTER_PRIMARY_API_KEY
```

Gateway читает:

```text
os.Getenv("OPENROUTER_PRIMARY_API_KEY")
```

Если env-переменная отсутствует или пустая, route считается unavailable.

Ошибка route:

```text
route_unavailable
reason = missing_reseller_api_key
```

Эта причина не должна раскрывать имя env-переменной обычному client response.

Admin/debug API может показывать безопасную диагностику.

---

# 12. Admin auth boundary

Admin API должен иметь отдельный auth boundary.

User API key не обязан давать admin-доступ.

Минимально допустимая первая версия:

```text
Authorization: Bearer <admin_token>
```

Config key:

```text
TOKENIO_ADMIN_TOKEN
```

Production-рекомендация:

```text
отдельные admin API keys с ролями
```

Admin API подробно описывается в:

```text
docs/spec/060-admin-api.ru.md
```

---

# 13. Logging and secrets

## 13.1. Запрещено логировать

Запрещено логировать:

```text
raw user API key
encrypted provisioning raw key
provisioning encryption nonce
provisioning encryption key
provisioning service token
billing JWT
billing signing key
billing service token
reseller API key
Authorization header
X-Service-Token header
full request body by default
```

## 13.2. Разрешено логировать

Разрешено логировать:

```text
local_request_id
user_id
api_key_id
endpoint_kind
api_family
client_model
selected_route_id
selected_reseller_id
provider_type
status
error_code
amount_cents
token counts
```

Сырые prompt/body не логируются по умолчанию.

---

# 14. Required config

Auth and billing identity layer requires:

```text
TOKENIO_BILLING_BASE_URL
TOKENIO_BILLING_SERVICE_TOKEN
TOKENIO_BILLING_JWT_SIGNING_KEY
TOKENIO_BILLING_JWT_TTL
TOKENIO_ADMIN_TOKEN
TOKENIO_API_KEY_HASH_SECRET
TOKENIO_PROVISIONING_SERVICE_TOKEN
TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY
TOKENIO_API_KEY_PROVISIONING_KEY_VERSION
TOKENIO_API_KEY_PROVISIONING_TTL
```

`TOKENIO_API_KEY_HASH_SECRET` is required for runtime API key validation.

Runtime API key validation must fail at startup if this secret is missing.

SHA-256 without secret is not a valid runtime fallback.

---

# 15. Acceptance criteria

Auth layer считается реализованным, если:

```text
1. Public endpoints принимают Authorization: Bearer sk_...
2. Public endpoints не принимают client JWT как auth contract.
3. Raw API key не хранится.
4. API key hash считается через HMAC-SHA256 с TOKENIO_API_KEY_HASH_SECRET.
5. API key сравнивается через hash и constant-time comparison.
6. Disabled/revoked/expired key отклоняется.
7. Disabled user получает 403 user_disabled.
8. Gateway строит billing JWT для balance request.
9. Gateway использует service token для charge request.
10. Gateway не передаёт sk_... в billing service.
11. Gateway не передаёт sk_... reseller.
12. Gateway не логирует secrets.
13. go test покрывает auth parsing, HMAC hash, constant-time comparison, disabled key, disabled user и billing JWT claims.
14. Payment provisioning использует отдельный service credential.
15. Permanent auth storage остаётся HMAC-only.
16. Retry-safe raw key delivery соответствует docs/spec/021-api-key-provisioning.ru.md.
```
