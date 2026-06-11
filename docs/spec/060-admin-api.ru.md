# 060. Admin API

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение документа

Этот документ описывает Admin API Tokenio Gateway.

Admin API нужен для управления:

```text
users
user API keys
resellers
reseller balances
routes
route prices
route cooldowns
usage records
billing charge batches
manual resolution
audit log
```

Документ не описывает:

```text
public LLM API
runtime route selection internals
pricing formula internals
ledger state machine details
SQL schema details
provider adapter implementation
```

Эти темы описываются в отдельных спецификациях.

---

# 2. Главный admin invariant

Admin API — это control plane.

Public `/v1/*` endpoints — это data plane.

Control plane не должен смешиваться с data plane.

Запрещено:

```text
делать admin операции через public /v1 endpoints
использовать user API key как admin credential
изменять reseller balance без audit log
изменять route price без audit log
resolve pricing_failed без audit log
возвращать raw secrets через admin API
```

---

# 3. Admin auth

## 3.1. Первая версия

Минимальная auth-модель первой версии:

```http
Authorization: Bearer <TOKENIO_ADMIN_TOKEN>
```

Config key:

```text
TOKENIO_ADMIN_TOKEN
```

Если header отсутствует:

```text
HTTP 401
error.code = admin_unauthorized
```

Если token неверный:

```text
HTTP 403
error.code = admin_forbidden
```

## 3.2. Production direction

Production-рекомендация:

```text
admin API keys
admin roles
admin subjects
audit per admin subject
```

Но первая версия может использовать один admin token.

## 3.3. Admin subject

Даже если используется один static token, gateway должен формировать `admin_subject`.

Первая версия:

```text
admin_subject = "admin_token"
```

Будущая версия:

```text
admin_subject = admin_api_key_id OR admin_user_id
```

`admin_subject` записывается в audit log.

---

# 4. Admin base path

Все admin endpoints находятся под:

```text
/admin/v1
```

Примеры:

```text
GET  /admin/v1/users
POST /admin/v1/users
GET  /admin/v1/resellers
POST /admin/v1/resellers
GET  /admin/v1/routes
POST /admin/v1/routes
```

Public LLM endpoints не должны находиться под `/admin`.

---

# 5. Response format

## 5.1. Success response

Admin API возвращает JSON.

Пример:

```json
{
  "data": {
    "id": "reseller_openrouter_primary"
  }
}
```

Для списков:

```json
{
  "data": [],
  "pagination": {
    "limit": 50,
    "offset": 0,
    "total": 100
  }
}
```

## 5.2. Error response

Admin-owned errors используют общий error envelope:

```json
{
  "error": {
    "code": "admin_forbidden",
    "message": "Admin access denied",
    "request_id": "admreq_..."
  }
}
```

---

# 6. Admin request id

Каждый admin request получает:

```text
admin_request_id
```

Формат:

```text
admreq_<random>
```

Он используется в:

```text
response error envelope
audit log
logs
debugging
```

---

# 7. Users

## 7.1. List users

```http
GET /admin/v1/users
```

Query params:

```text
enabled
email
limit
offset
```

Response item:

```json
{
  "id": "usr_...",
  "external_billing_user_id": "billing_usr_...",
  "email": "user@example.com",
  "name": "User",
  "enabled": true,
  "created_at": "...",
  "updated_at": "...",
  "disabled_at": null
}
```

## 7.2. Create user

```http
POST /admin/v1/users
```

Request:

```json
{
  "id": "usr_...",
  "external_billing_user_id": "billing_usr_...",
  "email": "user@example.com",
  "name": "User"
}
```

Rules:

```text
id может быть передан явно или создан gateway
external_billing_user_id обязателен
enabled по умолчанию true
```

Audit action:

```text
user.create
```

## 7.3. Disable user

```http
POST /admin/v1/users/{user_id}/disable
```

Effect:

```text
tokenio_users.enabled = false
tokenio_users.disabled_at = now()
```

Disabled user не может использовать existing API keys.

Audit action:

```text
user.disable
```

## 7.4. Enable user

```http
POST /admin/v1/users/{user_id}/enable
```

Effect:

```text
tokenio_users.enabled = true
tokenio_users.disabled_at = NULL
```

Audit action:

```text
user.enable
```

---

# 8. User API keys

## 8.1. List API keys

```http
GET /admin/v1/users/{user_id}/api-keys
```

Response item:

```json
{
  "id": "ak_...",
  "user_id": "usr_...",
  "name": "Laptop key",
  "key_prefix": "sk_live_abcd...",
  "enabled": true,
  "created_at": "...",
  "last_used_at": "...",
  "revoked_at": null,
  "expires_at": null
}
```

Forbidden response fields:

```text
raw_api_key
key_hash
```

## 8.2. Create API key

```http
POST /admin/v1/users/{user_id}/api-keys
```

Request:

```json
{
  "name": "Laptop key",
  "expires_at": null
}
```

Response:

```json
{
  "data": {
    "id": "ak_...",
    "user_id": "usr_...",
    "name": "Laptop key",
    "api_key": "sk_live_...",
    "key_prefix": "sk_live_abcd...",
    "created_at": "..."
  }
}
```

Rules:

```text
raw api_key возвращается только один раз
raw api_key не хранится
key_hash сохраняется в БД
key_prefix сохраняется для display
```

Audit action:

```text
api_key.create
```

## 8.3. Revoke API key

```http
POST /admin/v1/api-keys/{api_key_id}/revoke
```

Effect:

```text
enabled = false
revoked_at = now()
```

Audit action:

```text
api_key.revoke
```

---

# 9. Resellers

## 9.1. List resellers

```http
GET /admin/v1/resellers
```

Query params:

```text
provider_type
enabled
limit
offset
```

Response item:

```json
{
  "id": "reseller_openrouter_primary",
  "name": "OpenRouter Primary",
  "provider_type": "openrouter",
  "base_url": "https://...",
  "api_key_env": "OPENROUTER_PRIMARY_API_KEY",
  "api_key_env_present": true,
  "enabled": true,
  "balance_cents": 100000,
  "reserved_cents": 0,
  "minimum_balance_cents": 10000,
  "created_at": "...",
  "updated_at": "..."
}
```

Forbidden response fields:

```text
raw reseller API key
os.Getenv(api_key_env) value
```

## 9.2. Create reseller

```http
POST /admin/v1/resellers
```

Request:

```json
{
  "id": "reseller_openrouter_primary",
  "name": "OpenRouter Primary",
  "provider_type": "openrouter",
  "base_url": "https://openrouter.example.com",
  "api_key_env": "OPENROUTER_PRIMARY_API_KEY",
  "enabled": true,
  "balance_cents": 100000,
  "minimum_balance_cents": 10000
}
```

Rules:

```text
api_key_env is required
api_key_env value is not stored
provider_type must be allowed
balance_cents is manual accounting value
```

Audit action:

```text
reseller.create
```

## 9.3. Update reseller

```http
PATCH /admin/v1/resellers/{reseller_id}
```

Allowed fields:

```text
name
base_url
api_key_env
enabled
minimum_balance_cents
```

Audit action:

```text
reseller.update
```

## 9.4. Disable reseller

```http
POST /admin/v1/resellers/{reseller_id}/disable
```

Effect:

```text
enabled = false
disabled_at = now()
all routes of reseller become unavailable
```

Audit action:

```text
reseller.disable
```

## 9.5. Enable reseller

```http
POST /admin/v1/resellers/{reseller_id}/enable
```

Audit action:

```text
reseller.enable
```

---

# 10. Reseller balance

## 10.1. Get reseller balance

```http
GET /admin/v1/resellers/{reseller_id}/balance
```

Response:

```json
{
  "data": {
    "reseller_id": "reseller_openrouter_primary",
    "balance_cents": 100000,
    "reserved_cents": 500,
    "minimum_balance_cents": 10000,
    "available_balance_cents": 89500,
    "currency": "RUB"
  }
}
```

## 10.2. Adjust reseller balance

```http
POST /admin/v1/resellers/{reseller_id}/balance/adjust
```

Request:

```json
{
  "delta_cents": 50000,
  "reason": "manual top-up after reseller payment"
}
```

Rules:

```text
delta_cents can be positive or negative
reason is required
operation must be transactional
operation must be audit logged
```

Audit action:

```text
reseller_balance.adjust
```

## 10.3. Set reseller balance

```http
POST /admin/v1/resellers/{reseller_id}/balance/set
```

Request:

```json
{
  "balance_cents": 150000,
  "reason": "manual reconciliation"
}
```

Rules:

```text
reason is required
operation must be audit logged
```

Audit action:

```text
reseller_balance.set
```

---

# 11. Routes

## 11.1. List routes

```http
GET /admin/v1/routes
```

Query params:

```text
reseller_id
provider_type
api_family
endpoint_kind
client_model
enabled
limit
offset
```

Response item:

```json
{
  "id": "route_openrouter_gpt41mini_chat",
  "reseller_id": "reseller_openrouter_primary",
  "provider_type": "openrouter",
  "api_family": "openai_compatible",
  "endpoint_kind": "chat",
  "client_model": "gpt-4.1-mini",
  "provider_model": "openai/gpt-4.1-mini",
  "model_rewrite_policy": "provider_model",
  "enabled": true,
  "priority": 100,
  "requests_per_minute": 0,
  "tokens_per_minute": 0,
  "concurrent_requests": 0,
  "default_max_output_tokens": 4096,
  "capabilities": {
    "chat": true,
    "tools": true,
    "image_input": true
  },
  "cooldown_until": null,
  "cooldown_reason": null
}
```

## 11.2. Create route

```http
POST /admin/v1/routes
```

Request:

```json
{
  "id": "route_openrouter_gpt41mini_chat",
  "reseller_id": "reseller_openrouter_primary",
  "provider_type": "openrouter",
  "api_family": "openai_compatible",
  "endpoint_kind": "chat",
  "client_model": "gpt-4.1-mini",
  "provider_model": "openai/gpt-4.1-mini",
  "model_rewrite_policy": "provider_model",
  "enabled": true,
  "priority": 100,
  "requests_per_minute": 0,
  "tokens_per_minute": 0,
  "concurrent_requests": 0,
  "default_max_output_tokens": 4096,
  "capabilities": {
    "chat": true,
    "embeddings": false,
    "images_generation": false,
    "tools": true,
    "tool_choice": true,
    "response_format": true,
    "json_schema": true,
    "image_input": true,
    "audio_input": false,
    "file_input": false,
    "video_input": false,
    "reasoning": false
  }
}
```

Validation:

```text
reseller_id must exist
provider_type must match reseller.provider_type
api_family must be allowed
endpoint_kind must be allowed
client_model must be non-empty
provider_model must be non-empty
model_rewrite_policy must be one of: none, provider_model
if provider_model != client_model, model_rewrite_policy must be provider_model
default_max_output_tokens required for chat routes
capabilities must be JSON object
```

Audit action:

```text
route.create
```

## 11.3. Update route

```http
PATCH /admin/v1/routes/{route_id}
```

Allowed fields:

```text
provider_model
model_rewrite_policy
enabled
priority
requests_per_minute
tokens_per_minute
concurrent_requests
default_max_output_tokens
capabilities
```

Audit action:

```text
route.update
```

## 11.4. Disable route

```http
POST /admin/v1/routes/{route_id}/disable
```

Audit action:

```text
route.disable
```

## 11.5. Enable route

```http
POST /admin/v1/routes/{route_id}/enable
```

Audit action:

```text
route.enable
```

---

# 12. Route cooldowns

## 12.1. Get route cooldown

```http
GET /admin/v1/routes/{route_id}/cooldown
```

Response:

```json
{
  "data": {
    "route_id": "route_openrouter_gpt41mini_chat",
    "cooldown_until": "2026-06-11T12:00:00Z",
    "cooldown_reason": "rate_limit",
    "last_error_code": "upstream_rate_limit",
    "last_error_at": "2026-06-11T11:59:00Z"
  }
}
```

## 12.2. Set cooldown

```http
POST /admin/v1/routes/{route_id}/cooldown
```

Request:

```json
{
  "cooldown_until": "2026-06-11T12:00:00Z",
  "cooldown_reason": "manual_disabled"
}
```

Audit action:

```text
route_cooldown.set
```

## 12.3. Clear cooldown

```http
DELETE /admin/v1/routes/{route_id}/cooldown
```

Effect:

```text
cooldown_until = NULL
cooldown_reason = NULL
```

Audit action:

```text
route_cooldown.clear
```

---

# 13. Route prices

## 13.1. Get route price

```http
GET /admin/v1/routes/{route_id}/price
```

Response:

```json
{
  "data": {
    "route_id": "route_openrouter_gpt41mini_chat",
    "currency": "RUB",
    "input_price_per_1m_tokens_cents": 1000,
    "cached_input_price_per_1m_tokens_cents": 500,
    "output_price_per_1m_tokens_cents": 4000,
    "reasoning_output_price_per_1m_tokens_cents": 8000,
    "image_input_price_per_1m_tokens_cents": 3000,
    "audio_input_price_per_1m_tokens_cents": 3000,
    "audio_output_price_per_1m_tokens_cents": 6000,
    "file_input_price_per_1m_tokens_cents": 3000,
    "video_input_price_per_1m_tokens_cents": 10000,
    "image_generation_price_per_unit_cents": 150,
    "image_generation_unit_kind": "generated_image",
    "markup_coefficient": 1.3,
    "enabled": true
  }
}
```

## 13.2. Upsert route price

```http
PUT /admin/v1/routes/{route_id}/price
```

Request:

```json
{
  "currency": "RUB",
  "input_price_per_1m_tokens_cents": 1000,
  "cached_input_price_per_1m_tokens_cents": 500,
  "output_price_per_1m_tokens_cents": 4000,
  "reasoning_output_price_per_1m_tokens_cents": 8000,
  "image_input_price_per_1m_tokens_cents": 3000,
  "audio_input_price_per_1m_tokens_cents": 3000,
  "audio_output_price_per_1m_tokens_cents": 6000,
  "file_input_price_per_1m_tokens_cents": 3000,
  "video_input_price_per_1m_tokens_cents": 10000,
  "markup_coefficient": 1.3,
  "enabled": true
}
```

Validation:

```text
currency must be RUB
all price fields must be >= 0
markup_coefficient must be > 0
route_id must exist
```

Audit action:

```text
route_price.upsert
```

---

# 14. Usage records

## 14.1. List usage records

```http
GET /admin/v1/usage-records
```

Query params:

```text
user_id
status
provider_type
client_model
selected_route_id
selected_reseller_id
created_from
created_to
limit
offset
```

Response item:

```json
{
  "local_request_id": "llmreq_...",
  "user_id": "usr_...",
  "api_family": "openai_compatible",
  "endpoint_kind": "chat",
  "client_model": "gpt-4.1-mini",
  "billing_model": "openrouter:gpt-4.1-mini",
  "selected_reseller_id": "reseller_openrouter_primary",
  "selected_route_id": "route_openrouter_gpt41mini_chat",
  "provider_type": "openrouter",
  "status": "billable",
  "client_amount_cents": 123,
  "charged_amount_cents": 0,
  "remaining_amount_cents": 123,
  "currency": "RUB",
  "created_at": "..."
}
```

## 14.2. Get usage record

```http
GET /admin/v1/usage-records/{local_request_id}
```

Response должен включать полную ledger запись без secrets.

---

# 15. Manual resolution

## 15.1. Resolve pricing_failed as billable

```http
POST /admin/v1/usage-records/{local_request_id}/resolve/billable
```

Request:

```json
{
  "input_tokens": 1000,
  "output_tokens": 500,
  "client_amount_cents": 123,
  "actual_upstream_cost_cents": 80,
  "reason": "manual usage reconstruction"
}
```

Rules:

```text
current status must be pricing_failed
reason is required
operation must be transactional
operation must be audit logged
```

Audit action:

```text
usage.resolve_billable
```

## 15.2. Resolve pricing_failed as failed

```http
POST /admin/v1/usage-records/{local_request_id}/resolve/failed
```

Request:

```json
{
  "reason": "manual write-off"
}
```

Effect:

```text
status = failed
remaining_amount_cents = 0
```

Audit action:

```text
usage.resolve_failed
```

## 15.3. Resolve pricing_failed as charged

```http
POST /admin/v1/usage-records/{local_request_id}/resolve/charged
```

Request:

```json
{
  "charged_amount_cents": 123,
  "billing_charge_request_id": "charge_...",
  "reason": "manual external charge confirmed"
}
```

Effect:

```text
status = charged
charged_amount_cents = request.charged_amount_cents
remaining_amount_cents = 0
charged_at = now()
```

Audit action:

```text
usage.resolve_charged
```

---

# 16. Billing charge batches

## 16.1. List charge batches

```http
GET /admin/v1/billing-charge-batches
```

Query params:

```text
user_id
provider_type
client_model
billing_status
created_from
created_to
limit
offset
```

## 16.2. Get charge batch

```http
GET /admin/v1/billing-charge-batches/{batch_id}
```

Response includes:

```text
batch metadata
billing status
billing error
allocations
related usage records
```

## 16.3. Retry failed charge batch

```http
POST /admin/v1/billing-charge-batches/{batch_id}/retry
```

Allowed only if:

```text
billing_status = failed
```

Rules:

```text
must reuse stable idempotency semantics
must not double-charge already charged usage records
must audit log retry
```

Audit action:

```text
billing_charge.retry
```

---

# 17. Route events

## 17.1. List route events

```http
GET /admin/v1/route-events
```

Query params:

```text
route_id
reseller_id
event_type
local_request_id
created_from
created_to
limit
offset
```

Used for debugging:

```text
route skipped
cooldown set
retry
provider error
balance low
healthcheck failed
```

---

# 18. Telegram alerts

## 18.1. List alerts

```http
GET /admin/v1/telegram-alerts
```

Query params:

```text
alert_type
reseller_id
status
created_from
created_to
limit
offset
```

## 18.2. Retry failed alert

```http
POST /admin/v1/telegram-alerts/{alert_id}/retry
```

Audit action:

```text
telegram_alert.retry
```

---

# 19. Audit log

## 19.1. List audit log

```http
GET /admin/v1/audit-log
```

Query params:

```text
admin_subject
action
entity_type
entity_id
created_from
created_to
limit
offset
```

## 19.2. Audit entry

Response item:

```json
{
  "id": "audit_...",
  "admin_subject": "admin_token",
  "action": "route_price.upsert",
  "entity_type": "route_price",
  "entity_id": "route_openrouter_gpt41mini_chat",
  "before_state": {},
  "after_state": {},
  "request_id": "admreq_...",
  "created_at": "..."
}
```

Audit log should be append-only.

---

# 20. Validation rules

Admin API должен валидировать:

```text
provider_type allowed values
api_family allowed values
endpoint_kind allowed values
model_rewrite_policy allowed values
currency = RUB
price fields >= 0
markup_coefficient > 0
balance fields valid
route reseller exists
route provider_type matches reseller provider_type
client_model non-empty
provider_model non-empty
api_key_env non-empty
manual resolution reason non-empty
```

---

# 21. Secrets policy

Admin API никогда не возвращает:

```text
raw user API key, except one-time key creation response
key_hash
raw reseller API key
billing JWT
billing signing key
billing service token
admin token
Authorization header
```

Admin API может возвращать:

```text
key_prefix
api_key_env
api_key_env_present
```

`api_key_env_present` — boolean, показывающий, существует ли env-переменная в runtime.

---

# 22. Audit policy

Audit log обязателен для:

```text
user.create
user.enable
user.disable
api_key.create
api_key.revoke
reseller.create
reseller.update
reseller.enable
reseller.disable
reseller_balance.adjust
reseller_balance.set
route.create
route.update
route.enable
route.disable
route_cooldown.set
route_cooldown.clear
route_price.upsert
usage.resolve_billable
usage.resolve_failed
usage.resolve_charged
billing_charge.retry
telegram_alert.retry
```

Audit entry должен включать:

```text
admin_subject
action
entity_type
entity_id
before_state
after_state
request_id
created_at
```

---

# 23. Pagination

List endpoints должны поддерживать:

```text
limit
offset
```

Defaults:

```text
limit = 50
offset = 0
```

Maximum:

```text
limit = 500
```

---

# 24. Error codes

Admin-specific errors:

```text
admin_unauthorized
admin_forbidden
admin_validation_error
admin_not_found
admin_conflict
admin_state_conflict
admin_secret_not_available
```

Shared errors:

```text
invalid_json
method_not_allowed
not_found
internal_error
```

Подробный mapping описывается в:

```text
docs/spec/080-error-model.ru.md
```

---

# 25. Acceptance criteria

Admin API считается реализованным, если:

```text
1. Все endpoints находятся под /admin/v1.
2. Admin API требует отдельный admin credential.
3. User API key не даёт admin access.
4. Raw secrets не возвращаются.
5. Raw user API key возвращается только один раз при создании.
6. Reseller API key хранится только через api_key_env.
7. Admin может управлять users, API keys, resellers, routes, prices и balances.
8. Admin может ставить и очищать route cooldown.
9. Admin может смотреть usage records и charge batches.
10. Admin может resolve pricing_failed records.
11. Все опасные изменения пишутся в audit log.
12. Manual balance changes требуют reason.
13. Manual usage resolution требует reason.
14. Pagination есть на list endpoints.
15. Validation запрещает inconsistent provider_type/api_family/price/currency.
16. Tests покрывают auth, validation, secrets policy, audit log и manual resolution.
```
