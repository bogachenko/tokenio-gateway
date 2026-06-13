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

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (
        event_type IN (
            'selected',
            'skipped',
            'cooldown_set',
            'cooldown_expired',
            'retry',
            'failure',
            'success',
            'healthcheck_failed',
            'healthcheck_recovered',
            'balance_low'
        )
    )
);

CREATE INDEX tokenio_route_events_route_idx
    ON tokenio_route_events (route_id, created_at);

CREATE INDEX tokenio_route_events_reseller_idx
    ON tokenio_route_events (reseller_id, created_at);

CREATE INDEX tokenio_route_events_request_idx
    ON tokenio_route_events (local_request_id);

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
    sent_at TIMESTAMPTZ,

    CHECK (
        status IN (
            'pending',
            'sent',
            'failed',
            'suppressed'
        )
    )
);

CREATE INDEX tokenio_telegram_alerts_dedupe_idx
    ON tokenio_telegram_alerts (alert_type, dedupe_key, created_at);

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

CREATE UNIQUE INDEX tokenio_api_key_provisionings_idempotency_uq
    ON tokenio_api_key_provisionings (idempotency_key);

CREATE UNIQUE INDEX tokenio_api_key_provisionings_user_pending_uq
    ON tokenio_api_key_provisionings (user_id)
    WHERE status = 'pending_delivery';

CREATE INDEX tokenio_api_key_provisionings_external_billing_user_idx
    ON tokenio_api_key_provisionings (
        external_billing_user_id,
        created_at
    );

CREATE INDEX tokenio_api_key_provisionings_api_key_idx
    ON tokenio_api_key_provisionings (api_key_id);

CREATE INDEX tokenio_api_key_provisionings_expiry_idx
    ON tokenio_api_key_provisionings (
        status,
        expires_at
    )
    WHERE status = 'pending_delivery';
