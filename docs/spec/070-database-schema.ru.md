# 070. Database Schema

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение документа

Этот документ описывает database schema Tokenio Gateway.

Документ фиксирует:

```text
users
api_keys
resellers
routes
route_prices
usage_records
billing_sessions
billing_charge_batches
billing_charge_allocations
route_events
telegram_alerts
admin_audit_log
indexes
constraints
migration policy
```

Документ не описывает:

```text
public HTTP API
admin HTTP endpoints
route selection algorithm details
pricing formula details
provider adapter implementation
```

Эти темы описываются в отдельных спецификациях.

---

# 2. Главный database invariant

Postgres является source of truth для:

```text
user identity
API key records
reseller registry
route registry
route prices
reseller balance
usage ledger
billing charge history
admin changes
```

Запрещено:

```text
хранить raw user API key
хранить raw reseller API key
хранить billing JWT
хранить billing service token
хранить admin token
полагаться только на in-memory ledger
делать public billing state без локальной usage записи
```

---

# 3. Migration policy

## 3.1. Explicit SQL migrations

Schema должна управляться явными SQL migrations.

Рекомендуемая папка:

```text
db/migrations/
```

Формат:

```text
000001_init.up.sql
000001_init.down.sql
000002_add_route_events.up.sql
000002_add_route_events.down.sql
```

## 3.2. AutoMigrate forbidden

`AutoMigrate` не должен быть production source of truth.

Разрешено использовать Go structs для mapping, но не для неявного изменения production schema.

## 3.3. Prefix

Все таблицы Tokenio используют prefix:

```text
tokenio_
```

---

# 4. IDs and timestamps

## 4.1. IDs

Рекомендуемый тип ID:

```text
TEXT
```

Причина:

```text
id должен быть стабильным, читаемым в admin/debug и удобным для external correlation.
```

Примеры:

```text
usr_...
ak_...
reseller_openrouter_primary
route_...
llmreq_...
charge_...
```

## 4.2. Timestamps

Все timestamps хранятся в UTC.

Каждая основная таблица должна иметь:

```text
created_at
updated_at
```

Для terminal state timestamps используются отдельные поля:

```text
revoked_at
disabled_at
released_at
billable_at
charged_at
failed_at
cooldown_until
```

---

# 5. Table: tokenio_users

## 5.1. Purpose

`tokenio_users` хранит локальную identity пользователя Tokenio.

## 5.2. Columns

```sql
CREATE TABLE tokenio_users (
    id TEXT PRIMARY KEY,
    external_billing_user_id TEXT NOT NULL,
    email TEXT,
    name TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ
);
```

## 5.3. Constraints

```sql
CREATE UNIQUE INDEX tokenio_users_external_billing_user_id_uq
    ON tokenio_users (external_billing_user_id);

CREATE INDEX tokenio_users_enabled_idx
    ON tokenio_users (enabled);
```

## 5.4. Rules

Если `enabled = false`, все API keys пользователя считаются недействительными.

`external_billing_user_id` используется для billing JWT subject.

---

# 6. Table: tokenio_api_keys

## 6.1. Purpose

`tokenio_api_keys` хранит hashed user API keys.

Raw API key не хранится.

## 6.2. Columns

```sql
CREATE TABLE tokenio_api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES tokenio_users(id),
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    key_prefix TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ
);
```

## 6.3. Constraints and indexes

```sql
CREATE UNIQUE INDEX tokenio_api_keys_key_hash_uq
    ON tokenio_api_keys (key_hash);

CREATE INDEX tokenio_api_keys_user_id_idx
    ON tokenio_api_keys (user_id);

CREATE INDEX tokenio_api_keys_enabled_idx
    ON tokenio_api_keys (enabled);

CREATE INDEX tokenio_api_keys_key_prefix_idx
    ON tokenio_api_keys (key_prefix);
```

## 6.4. Rules

API key valid only if:

```text
enabled = true
revoked_at IS NULL
expires_at IS NULL OR expires_at > now()
user.enabled = true
```

`key_prefix` нужен только для admin display.
## 6.5. API key hash rule

`key_hash` stores only:

```text
HMAC-SHA256(TOKENIO_API_KEY_HASH_SECRET, raw_api_key)
```

Forbidden values:

```text
raw API key
unsalted SHA-256(raw_api_key)
any reversible encrypted API key
```

Reason:

```text
database compromise must not allow offline API key matching without TOKENIO_API_KEY_HASH_SECRET.
```

The database schema stores `key_hash` as TEXT, but the hashing algorithm is a runtime invariant from `docs/spec/020-auth-and-billing-identity.ru.md`.


---

# 7. Table: tokenio_resellers

## 7.1. Purpose

`tokenio_resellers` хранит конкретные upstream accounts/base URLs/balances.

## 7.2. Columns

```sql
CREATE TABLE tokenio_resellers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    provider_type TEXT NOT NULL,
    base_url TEXT NOT NULL,
    api_key_env TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,

    balance_cents BIGINT NOT NULL DEFAULT 0,
    reserved_cents BIGINT NOT NULL DEFAULT 0,
    minimum_balance_cents BIGINT NOT NULL DEFAULT 0,

    last_balance_alert_at TIMESTAMPTZ,
    last_healthcheck_at TIMESTAMPTZ,
    last_healthcheck_status TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ,

    CHECK (reserved_cents >= 0),
    CHECK (minimum_balance_cents >= 0)
);
```

`balance_cents` допускает отрицательное значение после actual cost reconciliation, если actual оказался выше estimate.

## 7.3. Provider type constraint

Allowed values:

```text
openai
openrouter
together
groq
ollama
lmstudio
vllm
gemini
anthropic
hydra
```

SQL check:

```sql
ALTER TABLE tokenio_resellers
ADD CONSTRAINT tokenio_resellers_provider_type_chk
CHECK (
    provider_type IN (
        'openai',
        'openrouter',
        'together',
        'groq',
        'ollama',
        'lmstudio',
        'vllm',
        'gemini',
        'anthropic',
        'hydra'
    )
);
```

## 7.4. Indexes

```sql
CREATE INDEX tokenio_resellers_provider_type_idx
    ON tokenio_resellers (provider_type);

CREATE INDEX tokenio_resellers_enabled_idx
    ON tokenio_resellers (enabled);
```

## 7.5. Secret rule

`api_key_env` хранит имя environment variable.

Запрещено хранить actual API key value в этой таблице.

---

# 8. Table: tokenio_routes

## 8.1. Purpose

`tokenio_routes` хранит routes: возможность обслужить конкретный `client_model` у конкретного reseller через конкретный API family и endpoint kind.

## 8.2. Columns

```sql
CREATE TABLE tokenio_routes (
    id TEXT PRIMARY KEY,
    reseller_id TEXT NOT NULL REFERENCES tokenio_resellers(id),

    provider_type TEXT NOT NULL,
    api_family TEXT NOT NULL,
    endpoint_kind TEXT NOT NULL,

    client_model TEXT NOT NULL,
    provider_model TEXT NOT NULL,
    model_rewrite_policy TEXT NOT NULL DEFAULT 'none',

    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    priority INTEGER NOT NULL DEFAULT 100,

    requests_per_minute INTEGER NOT NULL DEFAULT 0,
    tokens_per_minute INTEGER NOT NULL DEFAULT 0,
    concurrent_requests INTEGER NOT NULL DEFAULT 0,

    default_max_output_tokens BIGINT NOT NULL DEFAULT 0,

    capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,

    cooldown_until TIMESTAMPTZ,
    cooldown_reason TEXT,

    last_error_code TEXT,
    last_error_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ,

    CHECK (priority >= 0),
    CHECK (requests_per_minute >= 0),
    CHECK (tokens_per_minute >= 0),
    CHECK (concurrent_requests >= 0),
    CHECK (default_max_output_tokens >= 0)
);
```

## 8.3. API family constraint

Allowed values:

```text
openai_compatible
gemini_native
anthropic_native
ollama_native
```

```sql
ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_api_family_chk
CHECK (
    api_family IN (
        'openai_compatible',
        'gemini_native',
        'anthropic_native',
        'ollama_native'
    )
);
```

## 8.4. Endpoint kind constraint

Allowed values:

```text
chat
embeddings
images_generation
```

```sql
ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_endpoint_kind_chk
CHECK (
    endpoint_kind IN (
        'chat',
        'embeddings',
        'images_generation'
    )
);
```

## 8.5. Model rewrite policy constraint

Allowed values:

```text
none
provider_model
```

```sql
ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_model_rewrite_policy_chk
CHECK (
    model_rewrite_policy IN (
        'none',
        'provider_model'
    )
);
```

Route validation in application layer must enforce:

```text
if provider_model != client_model, model_rewrite_policy = provider_model
```



## 8.6. Unique route identity

```sql
CREATE UNIQUE INDEX tokenio_routes_unique_provider_model_route_uq
    ON tokenio_routes (
        reseller_id,
        api_family,
        endpoint_kind,
        client_model,
        provider_model
    );
```

## 8.7. Lookup index

Route selector lookup:

```sql
CREATE INDEX tokenio_routes_lookup_idx
    ON tokenio_routes (
        api_family,
        endpoint_kind,
        client_model,
        enabled
    );
```

## 8.8. Cooldown index

```sql
CREATE INDEX tokenio_routes_cooldown_idx
    ON tokenio_routes (cooldown_until);
```

## 8.9. Capabilities JSON

Minimal JSON shape:

```json
{
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
```

Capability validation в application layer.

---

# 9. Table: tokenio_route_prices

## 9.1. Purpose

`tokenio_route_prices` хранит pricing catalog на уровне route.

## 9.2. Columns

```sql
CREATE TABLE tokenio_route_prices (
    route_id TEXT PRIMARY KEY REFERENCES tokenio_routes(id),

    currency TEXT NOT NULL DEFAULT 'RUB',

    input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    cached_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    output_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    reasoning_output_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,

    image_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    audio_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    audio_output_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    file_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,
    video_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,

    image_generation_price_per_unit_cents BIGINT NOT NULL DEFAULT 0,
    image_generation_unit_kind TEXT NOT NULL DEFAULT 'none',

    markup_coefficient NUMERIC(12, 6) NOT NULL DEFAULT 1.0,

    enabled BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (currency = 'RUB'),
    CHECK (input_price_per_1m_tokens_cents >= 0),
    CHECK (cached_input_price_per_1m_tokens_cents >= 0),
    CHECK (output_price_per_1m_tokens_cents >= 0),
    CHECK (reasoning_output_price_per_1m_tokens_cents >= 0),
    CHECK (image_input_price_per_1m_tokens_cents >= 0),
    CHECK (audio_input_price_per_1m_tokens_cents >= 0),
    CHECK (audio_output_price_per_1m_tokens_cents >= 0),
    CHECK (file_input_price_per_1m_tokens_cents >= 0),
    CHECK (video_input_price_per_1m_tokens_cents >= 0),
    CHECK (image_generation_price_per_unit_cents >= 0),
    CHECK (image_generation_unit_kind IN ('none', 'generated_image')),
    CHECK (markup_coefficient > 0)
);
```

## 9.3. Rules

Prices are:

```text
RUB cents per 1,000,000 tokens
```

`markup_coefficient` применяется к client amount, но не меняет upstream cost.

---

# 10. Table: tokenio_usage_records

## 10.1. Purpose

`tokenio_usage_records` — основной local ledger.

Каждый LLM request должен иметь запись в этой таблице.

## 10.2. Columns

```sql
CREATE TABLE tokenio_usage_records (
    local_request_id TEXT PRIMARY KEY,

    idempotency_key TEXT,
    user_id TEXT NOT NULL REFERENCES tokenio_users(id),
    api_key_id TEXT REFERENCES tokenio_api_keys(id),

    api_family TEXT NOT NULL,
    endpoint_kind TEXT NOT NULL,
    client_model TEXT NOT NULL,
    billing_model TEXT NOT NULL,

    selected_reseller_id TEXT REFERENCES tokenio_resellers(id),
    selected_route_id TEXT REFERENCES tokenio_routes(id),

    provider_type TEXT NOT NULL,
    provider_model TEXT NOT NULL,

    provider_request_id TEXT,
    provider_response_model TEXT,

    estimated_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_output_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_image_generation_units BIGINT NOT NULL DEFAULT 0,
    estimated_client_amount_cents BIGINT NOT NULL DEFAULT 0,
    estimated_upstream_cost_cents BIGINT NOT NULL DEFAULT 0,

    input_tokens BIGINT NOT NULL DEFAULT 0,
    cached_input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    reasoning_tokens BIGINT NOT NULL DEFAULT 0,
    image_input_tokens BIGINT NOT NULL DEFAULT 0,
    audio_input_tokens BIGINT NOT NULL DEFAULT 0,
    audio_output_tokens BIGINT NOT NULL DEFAULT 0,
    file_input_tokens BIGINT NOT NULL DEFAULT 0,
    video_input_tokens BIGINT NOT NULL DEFAULT 0,
    image_generation_units BIGINT NOT NULL DEFAULT 0,

    client_amount_cents BIGINT NOT NULL DEFAULT 0,
    charged_amount_cents BIGINT NOT NULL DEFAULT 0,
    remaining_amount_cents BIGINT NOT NULL DEFAULT 0,

    actual_upstream_cost_cents BIGINT NOT NULL DEFAULT 0,

    currency TEXT NOT NULL DEFAULT 'RUB',

    usage_completeness TEXT NOT NULL DEFAULT 'missing',
    status TEXT NOT NULL,

    failure_reason TEXT,
    billing_charge_request_id TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reserved_at TIMESTAMPTZ,
    released_at TIMESTAMPTZ,
    billable_at TIMESTAMPTZ,
    charged_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (currency = 'RUB'),

    CHECK (estimated_input_tokens >= 0),
    CHECK (estimated_output_tokens >= 0),
    CHECK (estimated_image_generation_units >= 0),
    CHECK (estimated_client_amount_cents >= 0),
    CHECK (estimated_upstream_cost_cents >= 0),

    CHECK (input_tokens >= 0),
    CHECK (cached_input_tokens >= 0),
    CHECK (output_tokens >= 0),
    CHECK (reasoning_tokens >= 0),
    CHECK (image_input_tokens >= 0),
    CHECK (audio_input_tokens >= 0),
    CHECK (audio_output_tokens >= 0),
    CHECK (file_input_tokens >= 0),
    CHECK (video_input_tokens >= 0),
    CHECK (image_generation_units >= 0),

    CHECK (client_amount_cents >= 0),
    CHECK (charged_amount_cents >= 0),
    CHECK (remaining_amount_cents >= 0),
    CHECK (actual_upstream_cost_cents >= 0)
);
```

## 10.3. Status constraint

Allowed statuses:

```text
reserved
released
billable
partially_charged
charged
failed
pricing_failed
```

```sql
ALTER TABLE tokenio_usage_records
ADD CONSTRAINT tokenio_usage_records_status_chk
CHECK (
    status IN (
        'reserved',
        'released',
        'billable',
        'partially_charged',
        'charged',
        'failed',
        'pricing_failed'
    )
);
```

## 10.4. Usage completeness constraint

Allowed values:

```text
detailed
aggregate
estimated
missing
failed
```

```sql
ALTER TABLE tokenio_usage_records
ADD CONSTRAINT tokenio_usage_records_usage_completeness_chk
CHECK (
    usage_completeness IN (
        'detailed',
        'aggregate',
        'estimated',
        'missing',
        'failed'
    )
);
```

## 10.5. Idempotency unique index

```sql
CREATE UNIQUE INDEX tokenio_usage_records_idempotency_uq
    ON tokenio_usage_records (user_id, endpoint_kind, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
```

## 10.6. Pending lookup index

```sql
CREATE INDEX tokenio_usage_records_pending_idx
    ON tokenio_usage_records (user_id, status, created_at)
    WHERE status IN ('reserved', 'billable', 'partially_charged', 'pricing_failed');
```

## 10.7. Chargeable lookup index

```sql
CREATE INDEX tokenio_usage_records_chargeable_idx
    ON tokenio_usage_records (user_id, status, created_at)
    WHERE status IN ('billable', 'partially_charged');
```

## 10.8. Route usage index

```sql
CREATE INDEX tokenio_usage_records_route_idx
    ON tokenio_usage_records (selected_route_id, created_at);
```

## 10.9. Billing group index

```sql
CREATE INDEX tokenio_usage_records_billing_group_idx
    ON tokenio_usage_records (user_id, provider_type, client_model, currency, status);
```

---

# 11. Table: tokenio_billing_sessions

## 11.1. Purpose

`tokenio_billing_sessions` хранит cached remote billing balance.

Source of truth для pending — `tokenio_usage_records`, а не эта таблица.

## 11.2. Columns

```sql
CREATE TABLE tokenio_billing_sessions (
    user_id TEXT PRIMARY KEY REFERENCES tokenio_users(id),
    billing_subject_user_id TEXT NOT NULL,

    remote_balance_cents BIGINT NOT NULL DEFAULT 0,
    pending_amount_cents_cached BIGINT NOT NULL DEFAULT 0,

    currency TEXT NOT NULL DEFAULT 'RUB',

    fetched_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (currency = 'RUB'),
    CHECK (remote_balance_cents >= 0),
    CHECK (pending_amount_cents_cached >= 0)
);
```

## 11.3. Rules

`pending_amount_cents_cached` is optimization only.

Effective balance must be computed from:

```text
remote_balance_cents - pending amount from usage ledger
```

---

# 12. Table: tokenio_billing_charge_batches

## 12.1. Purpose

`tokenio_billing_charge_batches` хранит каждый вызов billing service charge.

## 12.2. Columns

```sql
CREATE TABLE tokenio_billing_charge_batches (
    id TEXT PRIMARY KEY,

    user_id TEXT NOT NULL REFERENCES tokenio_users(id),
    billing_subject_user_id TEXT NOT NULL,

    provider_type TEXT NOT NULL,
    client_model TEXT NOT NULL,
    billing_model TEXT NOT NULL,

    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,

    amount_cents BIGINT NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'RUB',

    billing_status TEXT NOT NULL,

    billing_response_balance_cents BIGINT,
    billing_error_code TEXT,
    billing_error_body TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    charged_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,

    CHECK (currency = 'RUB'),
    CHECK (input_tokens >= 0),
    CHECK (output_tokens >= 0),
    CHECK (amount_cents >= 0)
);
```

## 12.3. Billing status constraint

Allowed values:

```text
pending
succeeded
failed
```

```sql
ALTER TABLE tokenio_billing_charge_batches
ADD CONSTRAINT tokenio_billing_charge_batches_status_chk
CHECK (billing_status IN ('pending', 'succeeded', 'failed'));
```

## 12.4. Indexes

```sql
CREATE INDEX tokenio_billing_charge_batches_user_idx
    ON tokenio_billing_charge_batches (user_id, created_at);

CREATE INDEX tokenio_billing_charge_batches_model_idx
    ON tokenio_billing_charge_batches (provider_type, client_model, created_at);
```

---

# 13. Table: tokenio_billing_charge_allocations

## 13.1. Purpose

`tokenio_billing_charge_allocations` связывает charge batch с usage records.

Нужна для partial charge и audit.

## 13.2. Columns

```sql
CREATE TABLE tokenio_billing_charge_allocations (
    id TEXT PRIMARY KEY,

    batch_id TEXT NOT NULL REFERENCES tokenio_billing_charge_batches(id),
    local_request_id TEXT NOT NULL REFERENCES tokenio_usage_records(local_request_id),

    charged_amount_cents BIGINT NOT NULL,
    remaining_amount_cents BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (charged_amount_cents >= 0),
    CHECK (remaining_amount_cents >= 0)
);
```

## 13.3. Constraints

```sql
CREATE UNIQUE INDEX tokenio_billing_charge_allocations_batch_usage_uq
    ON tokenio_billing_charge_allocations (batch_id, local_request_id);

CREATE INDEX tokenio_billing_charge_allocations_usage_idx
    ON tokenio_billing_charge_allocations (local_request_id);
```

---

# 14. Table: tokenio_route_events

## 14.1. Purpose

`tokenio_route_events` хранит routing/cooldown/error events.

## 14.2. Columns

```sql
CREATE TABLE tokenio_route_events (
    id TEXT PRIMARY KEY,

    route_id TEXT REFERENCES tokenio_routes(id),
    reseller_id TEXT REFERENCES tokenio_resellers(id),

    provider_type TEXT,
    api_family TEXT,
    endpoint_kind TEXT,
    client_model TEXT,

    event_type TEXT NOT NULL,
    reason TEXT,
    local_request_id TEXT,

    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## 14.3. Event types

Allowed event types:

```text
selected
skipped
cooldown_set
cooldown_expired
retry
failure
success
healthcheck_failed
healthcheck_recovered
balance_low
```

## 14.4. Indexes

```sql
CREATE INDEX tokenio_route_events_route_idx
    ON tokenio_route_events (route_id, created_at);

CREATE INDEX tokenio_route_events_reseller_idx
    ON tokenio_route_events (reseller_id, created_at);

CREATE INDEX tokenio_route_events_request_idx
    ON tokenio_route_events (local_request_id);
```

---

# 15. Table: tokenio_telegram_alerts

## 15.1. Purpose

`tokenio_telegram_alerts` хранит alert deduplication и history.

## 15.2. Columns

```sql
CREATE TABLE tokenio_telegram_alerts (
    id TEXT PRIMARY KEY,

    alert_type TEXT NOT NULL,
    dedupe_key TEXT NOT NULL,

    reseller_id TEXT REFERENCES tokenio_resellers(id),
    route_id TEXT REFERENCES tokenio_routes(id),

    message TEXT NOT NULL,

    status TEXT NOT NULL,
    error TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ
);
```

## 15.3. Status values

```text
pending
sent
failed
suppressed
```

## 15.4. Dedupe index

```sql
CREATE INDEX tokenio_telegram_alerts_dedupe_idx
    ON tokenio_telegram_alerts (alert_type, dedupe_key, created_at);
```

---

# 16. Table: tokenio_admin_audit_log

## 16.1. Purpose

`tokenio_admin_audit_log` хранит audit trail admin actions.

## 16.2. Columns

```sql
CREATE TABLE tokenio_admin_audit_log (
    id TEXT PRIMARY KEY,

    admin_subject TEXT NOT NULL,

    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,

    before_state JSONB,
    after_state JSONB,

    request_id TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## 16.3. Rules

Admin actions must be audit logged for:

```text
user enable/disable
API key create/revoke
reseller create/update/disable
reseller balance update
route create/update/disable
route price update
cooldown clear/set
pricing_failed resolution
stuck reserved resolution
```

---

# 17. Transactions

## 17.1. Required transaction boundaries

Transactions required for:

```text
create usage reserve
commit reserved to billable
release reserved
mark pricing_failed
create billing charge batch
mark charge allocations
update reseller reserved/balance
admin manual resolution
```

## 17.2. Reseller reserve transaction

Route selection + reseller reserve must be atomic enough to prevent overspending.

For single instance, application-level lock may work, but DB row lock is preferred.

Required DB behavior:

```sql
SELECT ...
FROM tokenio_resellers
WHERE id = $1
FOR UPDATE;
```

Then update:

```sql
UPDATE tokenio_resellers
SET reserved_cents = reserved_cents + $estimated_upstream_cost_cents
WHERE id = $reseller_id;
```

## 17.3. Usage state transition transaction

State transition must verify current state.

Example:

```sql
UPDATE tokenio_usage_records
SET status = 'billable',
    ...
WHERE local_request_id = $1
  AND status = 'reserved';
```

If affected rows = 0, operation must fail with state conflict.

---

# 18. Denormalization policy

Allowed denormalized fields in `tokenio_usage_records`:

```text
provider_type
client_model
billing_model
selected_reseller_id
selected_route_id
provider_model
```

Reason:

```text
usage record must preserve historical truth even if route/reseller changes later.
```

Historical usage records must not change when route metadata changes.

---

# 19. Secrets policy

Forbidden columns:

```text
raw_api_key
reseller_api_key
billing_jwt
billing_service_token
admin_token
```

Allowed references:

```text
key_hash
key_prefix
api_key_env
admin_subject
```

---

# 20. Data retention

Default retention:

```text
usage_records: keep indefinitely until explicit retention policy is introduced
route_events: keep 90 days minimum
telegram_alerts: keep 90 days minimum
admin_audit_log: keep indefinitely
```

Retention jobs are out of scope for first implementation unless required operationally.

---

# 21. Acceptance criteria

Database schema считается реализованной, если:

```text
1. Все таблицы используют prefix tokenio_.
2. Raw user API keys не хранятся.
3. API key hash stores HMAC-SHA256 digest, not unsalted SHA-256.
4. Raw reseller API keys не хранятся.
5. Billing JWT не хранится.
6. Users and API keys support enabled/revoked/expired states.
7. Resellers store api_key_env, balance_cents, reserved_cents.
8. Routes are keyed by api_family + endpoint_kind + client_model.
9. Route prices support all token categories and image generation unit pricing.
10. Usage records support all ledger statuses.
11. Usage records support idempotency unique scope.
12. Pending/chargeable records have indexes.
13. Billing charge batches and allocations support partial charge.
14. Route events support cooldown/debug history.
15. Admin audit log exists.
16. State transitions verify current status.
17. Reseller reserve/reconcile can be done transactionally.
18. SQL migrations are explicit and reviewable.
19. go test or migration tests validate schema creation.
```
