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
billing_charge_expected_records
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
API key provisioning delivery state
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

Forbidden values inside `tokenio_api_keys`:

```text
raw API key
unsalted SHA-256(raw_api_key)
any reversible encrypted API key
```

Reason:

```text
database compromise must not allow offline API key matching without TOKENIO_API_KEY_HASH_SECRET.
```

Retry-safe initial delivery uses a separate temporary table:

```text
tokenio_api_key_provisionings
```

That table may contain only AEAD-encrypted raw key material while status is `pending_delivery`.

It is not an authentication source of truth, and encrypted fields must be cleared on `delivered` or `expired`.

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

    markup_coefficient DOUBLE PRECISION NOT NULL DEFAULT 1.0,

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
    CHECK (
        markup_coefficient > 0
        AND markup_coefficient < 'Infinity'::double precision
    )
);
```

## 9.3. Rules

Prices are:

```text
RUB cents per 1,000,000 tokens
```

`markup_coefficient` применяется к client amount, но не меняет upstream cost.

## 9.4. Canonical float64 persistence

Canonical application type:

```text
domain.RoutePrice.MarkupCoefficient = Go float64
```

Application validation принимает любой finite positive `float64`.

Поэтому physical type обязан быть:

```sql
DOUBLE PRECISION
```

а не fixed-scale `NUMERIC(p, s)`.

`NUMERIC(12, 6)` запрещён, потому что округляет valid values с более чем шестью fractional digits и ограничивает допустимый finite `float64` range.

Constraint:

```sql
CHECK (
    markup_coefficient > 0
    AND markup_coefficient < 'Infinity'::double precision
)
```

отклоняет:

```text
0
negative values
-Infinity
Infinity
NaN
```

PostgreSQL adapter обязан bind/scan значение как binary64-compatible `double precision` без decimal quantization.

Round-trip invariant:

```text
persisted MarkupCoefficient
    == canonical input float64

returned RoutePrice
    == exact committed RoutePrice

audit after_state
    == exact committed RoutePrice
```

CAS и audit comparison используют decoded canonical `float64`, а не округлённую decimal representation.

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
    estimated_cached_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_output_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_reasoning_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_image_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_audio_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_audio_output_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_file_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_video_input_tokens BIGINT NOT NULL DEFAULT 0,
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
    CHECK (estimated_cached_input_tokens >= 0),
    CHECK (estimated_output_tokens >= 0),
    CHECK (estimated_reasoning_tokens >= 0),
    CHECK (estimated_image_input_tokens >= 0),
    CHECK (estimated_audio_input_tokens >= 0),
    CHECK (estimated_audio_output_tokens >= 0),
    CHECK (estimated_file_input_tokens >= 0),
    CHECK (estimated_video_input_tokens >= 0),
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

## 10.2A. Complete EstimatedUsage mapping

Все десять dimensions `domain.UsageRecord.EstimatedUsage` сохраняются без потерь:

```text
EstimatedUsage.InputTokens
    -> estimated_input_tokens
EstimatedUsage.CachedInputTokens
    -> estimated_cached_input_tokens
EstimatedUsage.OutputTokens
    -> estimated_output_tokens
EstimatedUsage.ReasoningTokens
    -> estimated_reasoning_tokens
EstimatedUsage.ImageInputTokens
    -> estimated_image_input_tokens
EstimatedUsage.AudioInputTokens
    -> estimated_audio_input_tokens
EstimatedUsage.AudioOutputTokens
    -> estimated_audio_output_tokens
EstimatedUsage.FileInputTokens
    -> estimated_file_input_tokens
EstimatedUsage.VideoInputTokens
    -> estimated_video_input_tokens
EstimatedUsage.ImageGenerationUnits
    -> estimated_image_generation_units
```

Synthetic zero reconstruction для отсутствующей dimension запрещён.

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

`tokenio_billing_charge_batches` хранит durable billing charge command state и результат reconciliation.

Raw Billing response body не хранится.

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

    amount_cents BIGINT NOT NULL,
    currency TEXT NOT NULL DEFAULT 'RUB',

    billing_status TEXT NOT NULL,

    billing_response_balance_cents BIGINT,
    billing_error_code TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    charged_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (currency = 'RUB'),
    CHECK (input_tokens >= 0),
    CHECK (output_tokens >= 0),
    CHECK (amount_cents > 0),
    CHECK (
        billing_response_balance_cents IS NULL
        OR billing_response_balance_cents >= 0
    ),
    CHECK (
        (
            billing_status = 'pending'
            AND charged_at IS NULL
            AND failed_at IS NULL
            AND billing_error_code = ''
        )
        OR
        (
            billing_status = 'failed'
            AND charged_at IS NULL
            AND failed_at IS NOT NULL
            AND billing_error_code <> ''
        )
        OR
        (
            billing_status = 'succeeded'
            AND charged_at IS NOT NULL
            AND failed_at IS NULL
            AND billing_error_code = ''
        )
    )
);
```

Column `billing_error_body` запрещён. Adapter сохраняет только normalized `billing_error_code`, необходимый canonical contract.

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

## 12.5. UpdatedAt persistence

`updated_at` хранит exact `domain.BillingChargeBatch.UpdatedAt` и не вычисляется при чтении из `created_at`, `charged_at` или `failed_at`.

Rules определены ledger semantics из `docs/spec/050-ledger-and-auto-charge.ru.md`:

```text
create:
    updated_at = created_at

automatic pending -> failed:
    updated_at = failed_at

automatic identical failed replay через MarkChargeBatchFailed:
    failed_at unchanged
    updated_at unchanged

explicit failed retry outcome через MarkChargeRetryFailedWithAudit:
    failed_at = retry_failed_at
    updated_at = retry_failed_at

pending|failed -> succeeded:
    updated_at = charged_at

succeeded identical replay:
    updated_at unchanged
```

## 12.6. Initial timestamps and existing replay

Для initial insert:

```text
incoming Status = pending
incoming CreatedAt = incoming UpdatedAt
created_at = incoming CreatedAt
updated_at = incoming UpdatedAt
```

Для existing batch replay incoming `Status`, `CreatedAt` и `UpdatedAt` сначала проходят structural validation из spec 050, но не сравниваются с persisted lifecycle state/timestamps, потому что timestamps создаются текущим clock и не входят в `StableBatchID`.

Persisted timestamps первой successful preparation:

```text
не заменяются replay command
являются authoritative
возвращаются в exact persisted snapshot
```

Mutable status/result fields также не входят в immutable existing-command equality.

---

# 13. Table: tokenio_billing_charge_allocations

## 13.1. Purpose

`tokenio_billing_charge_allocations` хранит immutable ordered allocations durable billing command.

Таблица нужна для partial charge, exact replay и audit.

## 13.2. Columns

```sql
CREATE TABLE tokenio_billing_charge_allocations (
    id TEXT PRIMARY KEY,

    batch_id TEXT NOT NULL REFERENCES tokenio_billing_charge_batches(id),
    local_request_id TEXT NOT NULL REFERENCES tokenio_usage_records(local_request_id),
    position INTEGER NOT NULL,

    charged_amount_cents BIGINT NOT NULL,
    remaining_amount_cents BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (position >= 0),
    CHECK (charged_amount_cents > 0),
    CHECK (remaining_amount_cents >= 0)
);
```

## 13.3. Constraints and indexes

```sql
CREATE UNIQUE INDEX tokenio_billing_charge_allocations_batch_usage_uq
    ON tokenio_billing_charge_allocations (batch_id, local_request_id);

CREATE UNIQUE INDEX tokenio_billing_charge_allocations_batch_position_uq
    ON tokenio_billing_charge_allocations (batch_id, position);

CREATE INDEX tokenio_billing_charge_allocations_usage_idx
    ON tokenio_billing_charge_allocations (local_request_id);
```

`charged_amount_cents <= 0` является invalid charge plan и не может быть persisted.

## 13.4. Initial timestamp and replay equality

Для initial insert `created_at` сохраняет canonical `BillingChargeAllocation.CreatedAt`.

Каждая incoming allocation обязана иметь valid UTC `CreatedAt`, равный `plan.Batch.CreatedAt`.

Для existing batch replay allocation equality включает:

```text
id
batch_id
local_request_id
position
charged_amount_cents
remaining_amount_cents
```

`created_at` не входит в replay equality, потому что создаётся текущим clock и не входит в `StableBatchID`.

Persisted `created_at` первой successful preparation является authoritative, не заменяется incoming replay timestamp и возвращается в loaded snapshot.

---

# 13A. Table: tokenio_billing_charge_expected_records

## 13A.1. Purpose

`tokenio_billing_charge_expected_records` хранит immutable canonical post-claim `domain.UsageRecord` snapshots durable billing command.

Snapshot:

```text
immutable
не обновляется при reconciliation
не удаляется после success
не пересобирается из mutable tokenio_usage_records
```

Pre-claim records в эту таблицу не сохраняются.

## 13A.2. Columns

```sql
CREATE TABLE tokenio_billing_charge_expected_records (
    batch_id TEXT NOT NULL
        REFERENCES tokenio_billing_charge_batches(id),

    local_request_id TEXT NOT NULL
        REFERENCES tokenio_usage_records(local_request_id),

    position INTEGER NOT NULL,

    expected_record JSONB NOT NULL,

    created_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (batch_id, local_request_id),

    UNIQUE (batch_id, position),

    CHECK (position >= 0),
    CHECK (jsonb_typeof(expected_record) = 'object')
);
```

`ON DELETE CASCADE` не используется.

## 13A.3. Index

```sql
CREATE INDEX tokenio_billing_charge_expected_records_request_idx
    ON tokenio_billing_charge_expected_records (local_request_id);
```

## 13A.4. Position, order and cardinality

Для каждого batch:

```text
одна allocation на каждый expected record
один expected record на каждую allocation
одинаковый local_request_id на одинаковой position
positions составляют непрерывный диапазон 0..N-1
```

Allocations и expected records загружаются отдельно:

```sql
ORDER BY position ASC
```

После загрузки adapter обязан проверить:

```text
len(ExpectedRecords) = len(Allocations)
ExpectedRecords[i].LocalRequestID = Allocations[i].LocalRequestID
```

Нарушение order/cardinality является store contract violation и не исправляется сортировкой, пропуском или synthetic reconstruction.

## 13A.5. Explicit expected-record JSON representation

Database JSON schema не определяется JSON tags или `omitempty` domain struct.

Будущий PostgreSQL adapter использует отдельный explicit persistence DTO и strict encoder/decoder.

Каждый `expected_record` всегда содержит keys:

```text
local_request_id
idempotency_key
user_id
api_key_id
api_family
endpoint_kind
client_model
billing_model
selected_route_id
selected_reseller_id
provider_type
provider_model
provider_request_id
provider_response_model
estimated_usage
usage
usage_completeness
estimated_client_amount_cents
estimated_upstream_cost_cents
client_amount_cents
charged_amount_cents
remaining_amount_cents
actual_upstream_cost_cents
currency
status
failure_reason
billing_charge_request_id
created_at
reserved_at
released_at
billable_at
charged_at
failed_at
updated_at
```

Optional strings всегда представлены explicit JSON string. При отсутствии значения используется `""`.

Это относится как минимум к:

```text
idempotency_key
provider_request_id
provider_response_model
failure_reason
billing_charge_request_id
```

Optional timestamps всегда представлены:

```text
null
или RFC3339Nano UTC string
```

Это относится к:

```text
reserved_at
released_at
billable_at
charged_at
failed_at
```

Required `created_at` и `updated_at` всегда являются непустыми RFC3339Nano UTC strings.

`estimated_usage` и `usage` всегда являются objects с десятью keys:

```text
input_tokens
cached_input_tokens
output_tokens
reasoning_tokens
image_input_tokens
audio_input_tokens
audio_output_tokens
file_input_tokens
video_input_tokens
image_generation_units
```

Все десять keys присутствуют даже при значении `0`.

Strict decoder обязан отклонять:

```text
unknown keys
missing required keys
invalid JSON types
invalid or non-UTC timestamps
invalid enum values
negative token or amount values
```

Любое такое значение является store contract violation. Adapter не default-ит missing values и не выполняет silent repair.

После strict decode infrastructure adapter возвращает exact `domain.UsageRecord` через canonical port.

Infrastructure adapter валидирует только persistence representation и structural storage contract:

```text
required JSON keys присутствуют
unknown JSON keys отсутствуют
JSON types корректны
timestamps имеют RFC3339Nano UTC representation
enum/scalar values могут быть отображены в canonical domain types
числовые значения помещаются в canonical Go types
order/cardinality persisted command не нарушены
```

Infrastructure adapter:

```text
не импортирует internal/application/**
не вызывает ledger.ValidateRecord
не копирует application validator в infrastructure
не принимает business/state-transition decisions
```

Canonical business validation выполняется application layer после получения entity через port и до use-case side effects. Для billing charge snapshot application использует `billing.ValidateChargeSnapshot`, который включает canonical validation batch, allocations и expected usage records. Для остальных port reads соответствующий application use case применяет принадлежащий ему canonical validator.

Если strict decoder не может построить exact canonical entity или обнаруживает structural storage corruption, adapter возвращает normalized store contract error без raw SQL/driver/JSON details. Application layer преобразует такой результат в свой safe contract error.

Layering source of truth:

```text
infrastructure -> domain + ports
application -> domain + ports
infrastructure -/-> application
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

## 17.1. Responsibility boundary

`docs/spec/050-ledger-and-auto-charge.ru.md` является source of truth для:

```text
pre-claim and post-claim semantics
existing batch replay
active, failed and historical claims
automatic failed replay CAS
explicit failed retry outcome and audit
successful reconciliation
```

Эта database specification не переопределяет ledger semantics. Она определяет только physical persistence и transactional enforcement принятых contracts.

## 17.2. PrepareChargeBatch persistence boundary

Одна database transaction обязана атомарно сохранить:

```text
billing batch
ordered allocations
ordered post-claim expected snapshots
usage claims
```

Transaction locks allocated usage rows, выполняет exact pre-claim comparison и проверяет отсутствие другого active pending/failed claim до insert/update.

При ошибке ни одна часть command или claim не сохраняется.

## 17.3. Existing batch load and replay enforcement

Existing command загружается из:

```text
tokenio_billing_charge_batches
tokenio_billing_charge_allocations ORDER BY position ASC
tokenio_billing_charge_expected_records ORDER BY position ASC
```

### Batch immutable replay comparison

Exact comparison включает:

```text
id
user_id
billing_subject_user_id
provider_type
client_model
billing_model
input_tokens
output_tokens
amount_cents
currency
```

Не сравниваются как immutable command identity:

```text
billing_status
billing_response_balance_cents
billing_error_code
created_at
charged_at
failed_at
updated_at
```

### Allocation immutable replay comparison

Каждая allocation сравнивается по canonical position и полям:

```text
id
batch_id
local_request_id
charged_amount_cents
remaining_amount_cents
```

Incoming `BillingChargeAllocation.CreatedAt` не сравнивается с persisted `created_at`.

### Expected-record replay comparison

Incoming pre-claim record преобразуется только так:

```text
comparison_record =
    incoming_record
    with BillingChargeRequestID = batch.ID
```

`comparison_record` exact-сравнивается с persisted post-claim snapshot на той же position по всем canonical `UsageRecord` fields.

Persisted batch/allocation timestamps первой successful preparation являются authoritative.

Exact replay:

```text
не изменяет batch
не изменяет allocations
не изменяет expected snapshots
не обновляет usage claims повторно
возвращает exact persisted snapshot
```

Persisted lifecycle status также возвращается без подмены. Exact `succeeded` snapshot является допустимым результатом `PrepareChargeBatch` replay.

Persistence adapter не имеет права:

```text
возвращать synthetic pending/failed status
скрывать succeeded command как state conflict
повторно применять claim
повторно вызывать Billing
```

Application layer распознаёт exact succeeded snapshot как already reconciled idempotent success и не выполняет новый financial side effect.

Любое отличие canonical identity, position, cardinality или expected record является state/contract conflict.

## 17.4. ApplyChargeSuccess persistence boundary

`ApplyChargeSuccess` использует persisted batch, allocations и expected snapshots как source of truth.

Одна transaction обязана:

```text
lock batch
load ordered immutable command
lock corresponding mutable usage rows
verify each row against persisted post-claim snapshot
apply every allocation exactly once
persist usage results
persist succeeded batch result
commit
```

Store contract violation или CAS conflict приводит к rollback без partial reconciliation.

## 17.5. Automatic MarkChargeBatchFailed enforcement

`UsageLedger.MarkChargeBatchFailed` реализует:

```text
pending -> failed
```

и idempotent no-op replay уже failed batch только для exact того же `billing_error_code`.

При already-failed no-op incoming `failedAt` не изменяет persisted `failed_at` и `updated_at`.

Эта operation не пишет admin audit.

## 17.6. Explicit retry failure audit transaction

`AdminUsageLedger.MarkChargeRetryFailedWithAudit` является отдельной operation.

Одна transaction обязана:

```text
1. Lock batch.
2. Require current status = expectedStatus = failed.
3. Build next failed batch with:
   billing_error_code = caller normalized code
   failed_at = retryFailedAt
   updated_at = retryFailedAt.
4. Preserve immutable command, charged_at and billing_response_balance_cents.
5. Verify audit before_state equals exact current batch.
6. Verify audit after_state equals exact next batch.
7. Persist next batch.
8. Persist retry outcome audit.
9. Commit atomically.
```

Audit entry не может описывать состояние, отличное от committed batch.

Idempotency key этой operation — outcome audit ID:

```text
same audit ID and exact same payload
    -> no-op success with existing committed state

same audit ID and different payload
    -> state/contract conflict
```

## 17.7. RoutePrice float64 enforcement

`tokenio_route_prices.markup_coefficient` сохраняется как `DOUBLE PRECISION`.

Adapter обязан:

```text
bind canonical Go float64 без fixed-scale decimal conversion
scan PostgreSQL float8 обратно в Go float64
reject any decoded non-finite or non-positive value
verify exact canonical value for CAS and audit
```

Округление до decimal scale, включая шесть знаков после запятой, запрещено.

## 17.8. Generic usage state transitions

Остальные usage transitions также проверяют expected current state.

Пример:

```sql
UPDATE tokenio_usage_records
SET status = 'billable',
    ...
WHERE local_request_id = $1
  AND status = 'reserved';
```

Если affected rows = 0, operation возвращает state conflict.

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

# 20. Table: tokenio_api_key_provisionings

## 20.1. Purpose

`tokenio_api_key_provisionings` хранит idempotency и temporary encrypted delivery state для initial API key delivery.

Permanent authentication source of truth остаётся:

```text
tokenio_api_keys.key_hash
```

## 20.2. Columns

```sql
CREATE TABLE tokenio_api_key_provisionings (
    id TEXT PRIMARY KEY,

    idempotency_key TEXT NOT NULL,
    source_reference_hash TEXT NOT NULL,

    external_billing_user_id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES tokenio_users(id),
    api_key_id TEXT NOT NULL REFERENCES tokenio_api_keys(id),

    result_type TEXT NOT NULL,
    status TEXT NOT NULL,

    encrypted_raw_key BYTEA,
    encryption_nonce BYTEA,
    encryption_key_version TEXT,

    delivery_attempts INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    expired_at TIMESTAMPTZ,

    CHECK (result_type IN ('key_created', 'already_provisioned')),
    CHECK (status IN ('pending_delivery', 'delivered', 'expired')),
    CHECK (delivery_attempts >= 0),

    CHECK (
        (
            status = 'pending_delivery'
            AND result_type = 'key_created'
            AND encrypted_raw_key IS NOT NULL
            AND encryption_nonce IS NOT NULL
            AND encryption_key_version IS NOT NULL
            AND expires_at IS NOT NULL
        )
        OR
        (
            status IN ('delivered', 'expired')
            AND encrypted_raw_key IS NULL
            AND encryption_nonce IS NULL
        )
    )
);
```

`result_type = already_provisioned` создаётся сразу в terminal `delivered` state и не содержит encrypted material.

## 20.3. Idempotency constraint

```sql
CREATE UNIQUE INDEX tokenio_api_key_provisionings_idempotency_uq
    ON tokenio_api_key_provisionings (idempotency_key);
```

Same `Idempotency-Key` не может создать второй record или второй API key.

## 20.4. One pending delivery per user

```sql
CREATE UNIQUE INDEX tokenio_api_key_provisionings_user_pending_uq
    ON tokenio_api_key_provisionings (user_id)
    WHERE status = 'pending_delivery';
```

Один user не может иметь несколько одновременно replay-able raw keys.

## 20.5. Lookup indexes

```sql
CREATE INDEX tokenio_api_key_provisionings_external_billing_user_idx
    ON tokenio_api_key_provisionings (
        external_billing_user_id,
        created_at
    );

CREATE INDEX tokenio_api_key_provisionings_api_key_idx
    ON tokenio_api_key_provisionings (
        api_key_id
    );

CREATE INDEX tokenio_api_key_provisionings_expiry_idx
    ON tokenio_api_key_provisionings (
        status,
        expires_at
    )
    WHERE status = 'pending_delivery';
```

## 20.6. Transaction rules

First provisioning transaction must atomically:

```text
find/create user
check active API key
create API key hash record if required
create provisioning record
persist encrypted delivery copy
```

Confirm-delivery transaction must atomically:

```text
pending_delivery -> delivered
encrypted_raw_key = NULL
encryption_nonce = NULL
delivered_at = now()
```

Expiration transaction must atomically:

```text
pending_delivery -> expired
encrypted_raw_key = NULL
encryption_nonce = NULL
associated API key revoked
expired_at = now()
```

## 20.7. Encryption rules

`encrypted_raw_key` contains only AES-256-GCM ciphertext.

`encryption_nonce` must be unique for the encryption key.

AEAD associated data includes:

```text
provisioning_id
api_key_id
user_id
```

Encryption key is stored only in runtime environment/config, never in Postgres.

Full contract:

```text
docs/spec/021-api-key-provisioning.ru.md
```

# 21. Data retention

Default retention:

```text
usage_records: keep indefinitely until explicit retention policy is introduced
route_events: keep 90 days minimum
telegram_alerts: keep 90 days minimum
admin_audit_log: keep indefinitely
api_key_provisionings: keep metadata for audit; encrypted_raw_key and encryption_nonce must be NULL after terminal state
```

Retention jobs are out of scope for first implementation unless required operationally.

---

# 22. Acceptance criteria

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
20. API key provisioning idempotency имеет unique constraint.
21. Terminal provisioning states не содержат encrypted raw key material.
```

## 22.1. Stage 11A persistence acceptance

Stage 11A database contract дополнительно принимается только если:

```text
1. Immutable charge command round-trip не требует synthetic reconstruction.
2. Existing replay сравнивает explicit canonical field sets.
3. Batch CreatedAt/UpdatedAt и allocation CreatedAt не вызывают conflict legitimate replay.
4. Persisted first-write timestamps являются authoritative.
5. Explicit retry failure atomарно обновляет failed_at/updated_at и audit after_state.
6. Automatic identical failed replay не изменяет timestamps.
7. markup_coefficient хранится как finite positive DOUBLE PRECISION.
8. NUMERIC fixed-scale quantization для canonical float64 запрещена.
9. RoutePrice, CAS result и audit after_state round-trip без semantic value loss.
10. Production code и migrations не изменяются на Stage 11A.
```
