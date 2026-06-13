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

CREATE UNIQUE INDEX tokenio_users_external_billing_user_id_uq
    ON tokenio_users (external_billing_user_id);

CREATE INDEX tokenio_users_enabled_idx
    ON tokenio_users (enabled);

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

CREATE UNIQUE INDEX tokenio_api_keys_key_hash_uq
    ON tokenio_api_keys (key_hash);

CREATE INDEX tokenio_api_keys_user_id_idx
    ON tokenio_api_keys (user_id);

CREATE INDEX tokenio_api_keys_enabled_idx
    ON tokenio_api_keys (enabled);

CREATE INDEX tokenio_api_keys_key_prefix_idx
    ON tokenio_api_keys (key_prefix);

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

CREATE INDEX tokenio_resellers_provider_type_idx
    ON tokenio_resellers (provider_type);

CREATE INDEX tokenio_resellers_enabled_idx
    ON tokenio_resellers (enabled);

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

ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_endpoint_kind_chk
CHECK (
    endpoint_kind IN (
        'chat',
        'embeddings',
        'images_generation'
    )
);

ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_model_rewrite_policy_chk
CHECK (
    model_rewrite_policy IN (
        'none',
        'provider_model'
    )
);

CREATE UNIQUE INDEX tokenio_routes_unique_provider_model_route_uq
    ON tokenio_routes (
        reseller_id,
        api_family,
        endpoint_kind,
        client_model,
        provider_model
    );

CREATE INDEX tokenio_routes_lookup_idx
    ON tokenio_routes (
        api_family,
        endpoint_kind,
        client_model,
        enabled
    );

CREATE INDEX tokenio_routes_cooldown_idx
    ON tokenio_routes (cooldown_until);

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
