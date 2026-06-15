CREATE TABLE tokenio_forwarding_attempts (
    local_request_id TEXT NOT NULL
        REFERENCES tokenio_usage_records(local_request_id)
        ON DELETE CASCADE,

    attempt_number INTEGER NOT NULL,

    route_id TEXT NOT NULL
        REFERENCES tokenio_routes(id),
    reseller_id TEXT NOT NULL
        REFERENCES tokenio_resellers(id),

    api_family TEXT NOT NULL,
    endpoint_kind TEXT NOT NULL,
    client_model TEXT NOT NULL,

    provider_type TEXT NOT NULL,
    provider_model TEXT NOT NULL,

    status TEXT NOT NULL,
    attempt_state TEXT,

    upstream_status_code INTEGER,
    failure_kind TEXT,
    route_retry_candidate BOOLEAN NOT NULL DEFAULT FALSE,

    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,

    PRIMARY KEY (local_request_id, attempt_number),

    CHECK (attempt_number > 0),

    CHECK (
        status IN (
            'started',
            'succeeded',
            'failed'
        )
    ),

    CHECK (
        attempt_state IS NULL
        OR attempt_state IN (
            'not_sent',
            'sent_no_response',
            'response_received'
        )
    ),

    CHECK (
        upstream_status_code IS NULL
        OR (
            upstream_status_code >= 100
            AND upstream_status_code <= 599
        )
    ),

    CHECK (
        (
            status = 'started'
            AND completed_at IS NULL
            AND attempt_state IS NULL
            AND upstream_status_code IS NULL
            AND failure_kind IS NULL
            AND route_retry_candidate = FALSE
        )
        OR
        (
            status = 'succeeded'
            AND completed_at IS NOT NULL
            AND attempt_state = 'response_received'
            AND upstream_status_code BETWEEN 200 AND 299
            AND failure_kind IS NULL
            AND route_retry_candidate = FALSE
        )
        OR
        (
            status = 'failed'
            AND completed_at IS NOT NULL
            AND attempt_state IS NOT NULL
            AND failure_kind IS NOT NULL
        )
    ),

    CHECK (
        completed_at IS NULL
        OR completed_at >= started_at
    )
);

CREATE INDEX tokenio_forwarding_attempts_request_idx
    ON tokenio_forwarding_attempts (
        local_request_id,
        attempt_number
    );

CREATE INDEX tokenio_forwarding_attempts_route_idx
    ON tokenio_forwarding_attempts (
        route_id,
        started_at
    );

CREATE INDEX tokenio_forwarding_attempts_open_idx
    ON tokenio_forwarding_attempts (
        started_at,
        local_request_id,
        attempt_number
    )
    WHERE status = 'started';
