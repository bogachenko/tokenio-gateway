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

CREATE UNIQUE INDEX tokenio_usage_records_idempotency_uq
    ON tokenio_usage_records (user_id, endpoint_kind, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX tokenio_usage_records_pending_idx
    ON tokenio_usage_records (user_id, status, created_at)
    WHERE status IN ('reserved', 'billable', 'partially_charged', 'pricing_failed');

CREATE INDEX tokenio_usage_records_chargeable_idx
    ON tokenio_usage_records (user_id, status, created_at)
    WHERE status IN ('billable', 'partially_charged');

CREATE INDEX tokenio_usage_records_route_idx
    ON tokenio_usage_records (selected_route_id, created_at);

CREATE INDEX tokenio_usage_records_billing_group_idx
    ON tokenio_usage_records (
        user_id,
        provider_type,
        client_model,
        currency,
        status
    );

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

ALTER TABLE tokenio_billing_charge_batches
ADD CONSTRAINT tokenio_billing_charge_batches_status_chk
CHECK (billing_status IN ('pending', 'succeeded', 'failed'));

CREATE INDEX tokenio_billing_charge_batches_user_idx
    ON tokenio_billing_charge_batches (user_id, created_at);

CREATE INDEX tokenio_billing_charge_batches_model_idx
    ON tokenio_billing_charge_batches (
        provider_type,
        client_model,
        created_at
    );

CREATE TABLE tokenio_billing_charge_allocations (
    id TEXT PRIMARY KEY,

    batch_id TEXT NOT NULL REFERENCES tokenio_billing_charge_batches(id),
    local_request_id TEXT NOT NULL
        REFERENCES tokenio_usage_records(local_request_id),
    position INTEGER NOT NULL,

    charged_amount_cents BIGINT NOT NULL,
    remaining_amount_cents BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (position >= 0),
    CHECK (charged_amount_cents > 0),
    CHECK (remaining_amount_cents >= 0)
);

CREATE UNIQUE INDEX tokenio_billing_charge_allocations_batch_usage_uq
    ON tokenio_billing_charge_allocations (batch_id, local_request_id);

CREATE UNIQUE INDEX tokenio_billing_charge_allocations_batch_position_uq
    ON tokenio_billing_charge_allocations (batch_id, position);

CREATE INDEX tokenio_billing_charge_allocations_usage_idx
    ON tokenio_billing_charge_allocations (local_request_id);

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

CREATE INDEX tokenio_billing_charge_expected_records_request_idx
    ON tokenio_billing_charge_expected_records (local_request_id);
