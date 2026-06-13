# 090. Configuration

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение документа

Этот документ описывает runtime configuration Tokenio Gateway.

Документ фиксирует:

```text
environment variables
required config
optional config
defaults
startup validation
secret handling
timeouts
limits
cooldowns
billing config
pricing config
admin config
API key provisioning config
Telegram alerts config
provider/reseller secret loading policy
```

Документ не описывает:

```text
public HTTP API
admin endpoint contracts
SQL schema
route selection algorithm
pricing formula
ledger state machine
provider adapter implementation
```

Эти темы описываются в отдельных спецификациях.

---

# 2. Главный configuration invariant

Tokenio Gateway должен загружать и валидировать configuration на старте.

Если required config отсутствует или invalid:

```text
process must fail fast at startup
```

Запрещено:

```text
молча использовать пустой DATABASE_DSN
молча использовать пустой billing service token
молча использовать пустой admin token
молча использовать invalid currency
читать env напрямую внутри business logic
читать env напрямую внутри HTTP handlers
хранить raw secrets в Postgres
логировать secret values
```

Разрешено:

```text
читать env только в config/bootstrap layer
передавать typed config в runtime services
читать reseller API key по api_key_env через dedicated secret resolver
```

---

# 3. Config loading boundary

## 3.1. Config package

Единая точка загрузки config:

```text
internal/config
```

Config package отвечает за:

```text
чтение env
trim spaces
parsing int/float/duration/bool
defaults
required validation
startup errors
typed Config struct
```

Config package не отвечает за:

```text
route selection
billing calls
pricing calculation
HTTP response writing
provider-specific behavior
```

## 3.2. Direct os.Getenv policy

`os.Getenv` разрешён только в:

```text
1. config/bootstrap layer
2. dedicated reseller secret resolver
3. admin diagnostic check api_key_env_present
```

Запрещено читать env напрямую в:

```text
HTTP handlers
route selector
pricing calculator
ledger service
billing client business logic
provider adapter business logic
```

Provider adapter получает уже resolved credential или explicit error.

---

# 4. Required config

## 4.1. Core runtime

Обязательные переменные:

```text
TOKENIO_DATABASE_DSN
TOKENIO_BILLING_BASE_URL
TOKENIO_BILLING_SERVICE_TOKEN
TOKENIO_BILLING_JWT_SIGNING_KEY
TOKENIO_ADMIN_TOKEN
```

Если любая из них отсутствует или пустая:

```text
startup error
```

## 4.2. Required for production

Production deployment должен задавать:

```text
TOKENIO_GATEWAY_ADDR
TOKENIO_DATABASE_DSN
TOKENIO_BILLING_BASE_URL
TOKENIO_BILLING_SERVICE_TOKEN
TOKENIO_BILLING_JWT_SIGNING_KEY
TOKENIO_ADMIN_TOKEN
TOKENIO_API_KEY_HASH_SECRET
TOKENIO_PROVISIONING_SERVICE_TOKEN
TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY
```

`TOKENIO_API_KEY_HASH_SECRET` обязателен для API key hashing во всех runtime environments, кроме специально изолированных unit tests без persisted keys.

---

# 5. Optional config with defaults

## 5.1. Gateway

```text
TOKENIO_GATEWAY_ADDR
```

Default:

```text
:8880
```

Validation:

```text
must be non-empty
```

---

## 5.2. Currency

```text
TOKENIO_COST_CURRENCY
```

Default:

```text
RUB
```

Allowed values first version:

```text
RUB
```

If value is not `RUB`:

```text
startup error
```

---

## 5.3. Request body limit

```text
TOKENIO_REQUEST_BODY_MAX_BYTES
```

Default:

```text
67108864
```

Meaning:

```text
64 MiB
```

Validation:

```text
must be positive
```

If request body exceeds limit:

```text
HTTP 413
error.code = request_body_too_large
```

---

# 6. Billing config

## 6.1. Billing base URL

```text
TOKENIO_BILLING_BASE_URL
```

Required.

Validation:

```text
must be non-empty
must be valid URL
must not end with whitespace
```

Trailing slash may be trimmed by config loader.

## 6.2. Billing service token

```text
TOKENIO_BILLING_SERVICE_TOKEN
```

Required.

Used only for:

```text
POST /api/v1/usage/charge
X-Service-Token: <TOKENIO_BILLING_SERVICE_TOKEN>
```

Forbidden:

```text
returning this value through API
logging this value
storing this value in Postgres
using this as public client auth
```

## 6.3. Billing JWT signing key

```text
TOKENIO_BILLING_JWT_SIGNING_KEY
```

Required.

Used for signing internal billing JWT.

Forbidden:

```text
logging value
returning value through admin API
storing value in Postgres
```

## 6.4. Billing JWT TTL

```text
TOKENIO_BILLING_JWT_TTL
```

Default:

```text
15m
```

Validation:

```text
must be positive duration
recommended <= 1h
```

## 6.5. Billing timeout

```text
TOKENIO_BILLING_TIMEOUT
```

Default:

```text
30s
```

Validation:

```text
must be positive duration
```

---

# 7. Auto-charge config

## 7.1. Auto-charge threshold

```text
TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS
```

Default:

```text
1000
```

Meaning:

```text
auto-charge starts when pending billable amount >= threshold
```

Validation:

```text
must be positive
```

## 7.2. Minimum charge amount

```text
TOKENIO_MIN_CHARGE_AMOUNT_CENTS
```

Default:

```text
100
```

Validation:

```text
must be >= 0
```

If pending amount is below minimum, auto-charge may be deferred.

## 7.3. Minimum request balance

```text
TOKENIO_MIN_REQUEST_BALANCE_CENTS
```

Default:

```text
500
```

Validation:

```text
must be >= 0
```

A request can pass preflight only if:

```text
effective_balance_cents >= estimated_client_amount_cents
AND effective_balance_cents >= TOKENIO_MIN_REQUEST_BALANCE_CENTS
```

---

# 8. Pricing estimation config

## 8.1. Token estimation safety factor

```text
TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR
```

Default:

```text
1.25
```

Validation:

```text
must be >= 1
```

Used for conservative local token estimation.

## 8.2. Cost estimation safety factor

```text
TOKENIO_COST_ESTIMATION_SAFETY_FACTOR
```

Default:

```text
1.10
```

Validation:

```text
must be >= 1
```

Used for preflight cost estimation.

## 8.3. Estimation invariant

Safety factors are applied only to estimation.

Actual committed usage should use actual provider usage when available.

Estimator must not mutate request body; forwarding may only apply explicit model identifier rewrite according to route `model_rewrite_policy`.

---

# 9. Upstream config

## 9.1. Upstream timeout

```text
TOKENIO_UPSTREAM_TIMEOUT
```

Default:

```text
90s
```

Validation:

```text
must be positive duration
```

## 9.2. Upstream retry policy

Global defaults may exist, but final retry decision is route/provider aware.

Recommended config:

```text
TOKENIO_UPSTREAM_MAX_ATTEMPTS
TOKENIO_UPSTREAM_MAX_BACKOFF
```

Defaults:

```text
TOKENIO_UPSTREAM_MAX_ATTEMPTS=3
TOKENIO_UPSTREAM_MAX_BACKOFF=2s
```

Validation:

```text
max attempts >= 1
max backoff > 0
```

Retry is allowed only if routing/retry boundary permits it.

---

# 10. Route cooldown config

## 10.1. Rate limit cooldown

```text
TOKENIO_ROUTE_COOLDOWN_RATE_LIMIT
```

Default:

```text
60s
```

## 10.2. Quota exceeded cooldown

```text
TOKENIO_ROUTE_COOLDOWN_QUOTA_EXCEEDED
```

Default:

```text
24h
```

## 10.3. Provider 5xx cooldown

```text
TOKENIO_ROUTE_COOLDOWN_5XX
```

Default:

```text
30s
```

## 10.4. Timeout cooldown

```text
TOKENIO_ROUTE_COOLDOWN_TIMEOUT
```

Default:

```text
30s
```

## 10.5. Auth error cooldown

```text
TOKENIO_ROUTE_COOLDOWN_AUTH_ERROR
```

Default:

```text
24h
```

## 10.6. Validation

All cooldown values:

```text
must be positive duration
```

Zero cooldown is forbidden unless explicitly supported by a test/dev mode.

---

# 11. Rate limiter config

Route-level limits are stored in Postgres:

```text
requests_per_minute
tokens_per_minute
concurrent_requests
```

Global limiter config may define default waiting behavior:

```text
TOKENIO_RATE_LIMIT_MAX_WAIT
```

Default:

```text
5s
```

Validation:

```text
must be positive duration
```

For first version:

```text
single instance
```

Therefore in-memory route limiter is acceptable.

Postgres remains source of truth for route metadata and reseller balance.

---

# 12. Admin config

## 12.1. Admin token

```text
TOKENIO_ADMIN_TOKEN
```

Required.

Used for:

```http
Authorization: Bearer <TOKENIO_ADMIN_TOKEN>
```

on:

```text
/admin/v1/*
```

Forbidden:

```text
logging value
returning value through API
storing value in Postgres
accepting user API key as admin auth
```

## 12.2. Admin token minimum length

Recommended validation:

```text
length >= 32 characters
```

If shorter in production:

```text
startup error
```

In local dev, short token may be allowed only if explicit dev mode is introduced.

No implicit dev fallback.

---

# 13. API key hashing config

## 13.1. HMAC secret

```text
TOKENIO_API_KEY_HASH_SECRET
```

Required for runtime API key hashing.

API key hash:

```text
HMAC-SHA256(TOKENIO_API_KEY_HASH_SECRET, raw_api_key)
```

SHA-256 without secret is forbidden as runtime fallback.

## 13.2. No silent downgrade

Missing `TOKENIO_API_KEY_HASH_SECRET` must be startup error for any runtime mode that validates persisted API keys.

Only isolated unit tests may use an explicit test hasher that does not read production env.

Environment mode must be explicit if used.

---

# 14. Environment mode

## 14.1. Optional env mode

Optional config:

```text
TOKENIO_ENV
```

Allowed values:

```text
production
development
test
```

Default:

```text
production
```

## 14.2. Production behavior

In production:

```text
strict startup validation
TOKENIO_API_KEY_HASH_SECRET required
admin token minimum length enforced
no debug response bodies
no full request/response logging
```

## 14.3. Development behavior

Development mode may allow:

```text
shorter admin token
local database DSN
verbose logs without secrets
```

Development mode must not allow:

```text
logging secrets
storing raw API keys
public /billing/flush
fallback between API families
```

---

# 15. Telegram alert config

## 15.1. Telegram bot token

```text
TOKENIO_TELEGRAM_BOT_TOKEN
```

Optional.

If absent:

```text
Telegram alerts disabled
```

## 15.2. Telegram chat id

```text
TOKENIO_TELEGRAM_CHAT_ID
```

Optional.

If bot token is set but chat id is missing:

```text
startup error
```

or:

```text
alerts disabled with explicit warning
```

Decision first version:

```text
if one of TOKENIO_TELEGRAM_BOT_TOKEN or TOKENIO_TELEGRAM_CHAT_ID is set, both are required
```

## 15.3. Reseller balance alert threshold

```text
TOKENIO_RESELLER_BALANCE_ALERT_CENTS
```

Default:

```text
10000
```

Validation:

```text
must be >= 0
```

## 15.4. Alert dedup interval

```text
TOKENIO_TELEGRAM_ALERT_DEDUPE_INTERVAL
```

Default:

```text
1h
```

Validation:

```text
must be positive duration
```

---

# 16. Reseller secret config

## 16.1. Reseller API keys

Reseller API keys are not declared as fixed config keys.

They are referenced dynamically by:

```text
tokenio_resellers.api_key_env
```

Example:

```text
api_key_env = OPENROUTER_PRIMARY_API_KEY
```

The gateway resolves:

```text
os.Getenv(api_key_env)
```

through a dedicated secret resolver.

## 16.2. Missing reseller secret

If `api_key_env` is missing or empty:

```text
routes for that reseller are unavailable
```

Public client error:

```text
HTTP 503
error.code = no_route_available
```

Admin API may show:

```json
{
  "api_key_env": "OPENROUTER_PRIMARY_API_KEY",
  "api_key_env_present": false
}
```

Admin API must not show the secret value.

---

# 17. Database config

## 17.1. DSN

```text
TOKENIO_DATABASE_DSN
```

Required.

Used for Postgres.

Validation:

```text
must be non-empty
must connect successfully at startup, unless migrations-only command explicitly says otherwise
```

## 17.2. Startup DB check

Gateway should fail startup if:

```text
database unavailable
required migrations not applied
schema version incompatible
```

## 17.3. Migrations config

Recommended optional config:

```text
TOKENIO_MIGRATIONS_DIR
```

Default:

```text
db/migrations
```

Gateway runtime should not silently run destructive migrations.

Migration execution should be explicit command or controlled startup mode.

---

# 18. HTTP server config

## 18.1. Read header timeout

```text
TOKENIO_HTTP_READ_HEADER_TIMEOUT
```

Default:

```text
10s
```

Validation:

```text
must be positive duration
```

## 18.2. Read timeout

```text
TOKENIO_HTTP_READ_TIMEOUT
```

Default:

```text
120s
```

## 18.3. Write timeout

```text
TOKENIO_HTTP_WRITE_TIMEOUT
```

Default:

```text
120s
```

## 18.4. Idle timeout

```text
TOKENIO_HTTP_IDLE_TIMEOUT
```

Default:

```text
60s
```

---

# 19. Logging config

## 19.1. Log level

```text
TOKENIO_LOG_LEVEL
```

Allowed:

```text
debug
info
warn
error
```

Default:

```text
info
```

## 19.2. Log format

```text
TOKENIO_LOG_FORMAT
```

Allowed:

```text
text
json
```

Default:

```text
text
```

## 19.3. Body logging

Request/response body logging is disabled by default.

Optional config:

```text
TOKENIO_LOG_BODIES
```

Default:

```text
false
```

Production rule:

```text
TOKENIO_LOG_BODIES=true is forbidden in production
```

Even in development, body logging must redact secrets and should be bounded.

---

# 20. Secret redaction

Config loader and logger must redact values for keys containing:

```text
TOKEN
SECRET
KEY
PASSWORD
DSN
AUTHORIZATION
```

Allowed to log only:

```text
key name
whether value is present
non-secret parsed config values
```

Example allowed log:

```text
TOKENIO_BILLING_SERVICE_TOKEN present=true
```

Forbidden log:

```text
TOKENIO_BILLING_SERVICE_TOKEN=...
```

---

# 21. Startup validation

Startup validation must check:

```text
required env present
currency = RUB
numeric values parse
numeric values within allowed ranges
duration values parse
duration values positive
admin token valid for env mode
billing JWT signing key present
billing JWT TTL valid
database connectivity
migration version compatible
Telegram config consistency
```

If validation fails:

```text
process exits non-zero
clear error is logged without secret values
```

---

# 22. Runtime config immutability

Typed config loaded at startup is immutable.

Changing env variables after process start does not change runtime behavior.

Mutable operational state must live in Postgres:

```text
resellers
routes
prices
balances
cooldowns
users
api_keys
```

---

# 23. Config table

## 23.1. Core

```text
TOKENIO_ENV                              default production
TOKENIO_GATEWAY_ADDR                     default :8880
TOKENIO_DATABASE_DSN                     required
TOKENIO_COST_CURRENCY                    default RUB
TOKENIO_REQUEST_BODY_MAX_BYTES           default 67108864
```

## 23.2. Billing

```text
TOKENIO_BILLING_BASE_URL                 required
TOKENIO_BILLING_SERVICE_TOKEN            required
TOKENIO_BILLING_JWT_SIGNING_KEY          required
TOKENIO_BILLING_JWT_TTL                  default 15m
TOKENIO_BILLING_TIMEOUT                  default 30s
```

## 23.3. Admin

```text
TOKENIO_ADMIN_TOKEN                      required
```

## 23.4. API keys and provisioning

```text
TOKENIO_API_KEY_HASH_SECRET                    required
TOKENIO_PROVISIONING_SERVICE_TOKEN             required
TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY    required, base64-encoded 32 bytes
TOKENIO_API_KEY_PROVISIONING_KEY_VERSION             default v1
TOKENIO_API_KEY_PROVISIONING_TTL                     default 24h
TOKENIO_API_KEY_PROVISIONING_EXPIRATION_INTERVAL     default 1m
TOKENIO_API_KEY_PROVISIONING_EXPIRATION_BATCH_SIZE   default 100
```

Provisioning encryption key must differ from API key HMAC secret.

First-version encryption algorithm:

```text
AES-256-GCM
```

Rotation to a new provisioning encryption key is allowed only when no pending delivery record requires the old key version.

Provisioning expiration worker contract:

```text
TOKENIO_API_KEY_PROVISIONING_EXPIRATION_INTERVAL must be positive duration
TOKENIO_API_KEY_PROVISIONING_EXPIRATION_BATCH_SIZE must be >= 1
worker executes one cycle immediately at startup and then by interval
worker calls the provisioning application service, not Postgres directly
```

## 23.5. Pricing and balance

```text
TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS      default 1000
TOKENIO_MIN_CHARGE_AMOUNT_CENTS          default 100
TOKENIO_MIN_REQUEST_BALANCE_CENTS        default 500
TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR   default 1.25
TOKENIO_COST_ESTIMATION_SAFETY_FACTOR    default 1.10
```

## 23.6. Upstream

```text
TOKENIO_UPSTREAM_TIMEOUT                 default 90s
TOKENIO_UPSTREAM_MAX_ATTEMPTS            default 3
TOKENIO_UPSTREAM_MAX_BACKOFF             default 2s
```

## 23.7. Cooldowns

```text
TOKENIO_ROUTE_COOLDOWN_RATE_LIMIT        default 60s
TOKENIO_ROUTE_COOLDOWN_QUOTA_EXCEEDED    default 24h
TOKENIO_ROUTE_COOLDOWN_5XX               default 30s
TOKENIO_ROUTE_COOLDOWN_TIMEOUT           default 30s
TOKENIO_ROUTE_COOLDOWN_AUTH_ERROR        default 24h
```

## 23.8. Telegram

```text
TOKENIO_TELEGRAM_BOT_TOKEN               optional
TOKENIO_TELEGRAM_CHAT_ID                 optional
TOKENIO_RESELLER_BALANCE_ALERT_CENTS     default 10000
TOKENIO_TELEGRAM_ALERT_DEDUPE_INTERVAL   default 1h
```

## 23.9. HTTP

```text
TOKENIO_HTTP_READ_HEADER_TIMEOUT         default 10s
TOKENIO_HTTP_READ_TIMEOUT                default 120s
TOKENIO_HTTP_WRITE_TIMEOUT               default 120s
TOKENIO_HTTP_IDLE_TIMEOUT                default 60s
```

## 23.10. Logging

```text
TOKENIO_LOG_LEVEL                        default info
TOKENIO_LOG_FORMAT                       default text
TOKENIO_LOG_BODIES                       default false
```

---

# 24. Example production env

```bash
export TOKENIO_ENV='production'
export TOKENIO_GATEWAY_ADDR=':8880'

export TOKENIO_DATABASE_DSN='host=localhost user=tokenio password=... dbname=tokenio_gateway port=5432 sslmode=disable TimeZone=UTC'

export TOKENIO_BILLING_BASE_URL='https://billing.example.com'
export TOKENIO_BILLING_SERVICE_TOKEN='...'
export TOKENIO_BILLING_JWT_SIGNING_KEY='...'
export TOKENIO_BILLING_JWT_TTL='15m'

export TOKENIO_ADMIN_TOKEN='...'
export TOKENIO_API_KEY_HASH_SECRET='...'

export TOKENIO_PROVISIONING_SERVICE_TOKEN='...'
export TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY='<base64-encoded-32-byte-key>'
export TOKENIO_API_KEY_PROVISIONING_KEY_VERSION='v1'
export TOKENIO_API_KEY_PROVISIONING_TTL='24h'
export TOKENIO_API_KEY_PROVISIONING_EXPIRATION_INTERVAL='1m'
export TOKENIO_API_KEY_PROVISIONING_EXPIRATION_BATCH_SIZE='100'

export TOKENIO_COST_CURRENCY='RUB'
export TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS='1000'
export TOKENIO_MIN_CHARGE_AMOUNT_CENTS='100'
export TOKENIO_MIN_REQUEST_BALANCE_CENTS='500'

export TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR='1.25'
export TOKENIO_COST_ESTIMATION_SAFETY_FACTOR='1.10'
export TOKENIO_REQUEST_BODY_MAX_BYTES='67108864'

export TOKENIO_UPSTREAM_TIMEOUT='90s'
export TOKENIO_BILLING_TIMEOUT='30s'

export TOKENIO_ROUTE_COOLDOWN_RATE_LIMIT='60s'
export TOKENIO_ROUTE_COOLDOWN_QUOTA_EXCEEDED='24h'
export TOKENIO_ROUTE_COOLDOWN_5XX='30s'
export TOKENIO_ROUTE_COOLDOWN_TIMEOUT='30s'
export TOKENIO_ROUTE_COOLDOWN_AUTH_ERROR='24h'

export TOKENIO_RESELLER_BALANCE_ALERT_CENTS='10000'

export TOKENIO_LOG_LEVEL='info'
export TOKENIO_LOG_FORMAT='json'
export TOKENIO_LOG_BODIES='false'
```

Reseller secrets are declared separately according to DB `api_key_env` values:

```bash
export OPENROUTER_PRIMARY_API_KEY='...'
export TOGETHER_PRIMARY_API_KEY='...'
export GEMINI_PRIMARY_API_KEY='...'
```

---

# 25. Acceptance criteria

Configuration layer считается реализованным, если:

```text
1. Config загружается в одном месте.
2. Required env валидируется на старте.
3. Invalid config приводит к startup failure.
4. Currency первой версии строго RUB.
5. Durations and numeric values validate ranges.
6. Admin token required.
7. Billing service token required.
8. Billing JWT signing key required.
9. API key HMAC secret required in production.
10. Request body max bytes configurable.
11. Auto-charge threshold configurable.
12. Cooldowns configurable.
13. Upstream and billing timeouts configurable.
14. Telegram config validates all-or-none token/chat id.
15. Reseller API keys resolve only through api_key_env.
16. Secret values are never logged.
17. Runtime config is immutable after startup.
18. Mutable operational state lives in Postgres.
19. Tests cover required env, defaults, invalid values, secret redaction and production strictness.
20. Provisioning service token is required.
21. Provisioning encryption key decodes from base64 to exactly 32 bytes.
22. Provisioning encryption key differs from API key HMAC secret.
23. Provisioning TTL is a positive duration.
24. Provisioning key version is non-empty.
```
