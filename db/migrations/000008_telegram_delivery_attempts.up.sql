CREATE TABLE tokenio_telegram_delivery_attempts (
    id TEXT PRIMARY KEY,

    alert_id TEXT NOT NULL
        REFERENCES tokenio_telegram_alerts(id),

    attempt_number INTEGER NOT NULL,

    status TEXT NOT NULL,
    attempt_state TEXT,

    failure_code TEXT,

    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,

    UNIQUE (alert_id, attempt_number),

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
        (
            status = 'started'
            AND attempt_state IS NULL
            AND failure_code IS NULL
            AND completed_at IS NULL
        )
        OR
        (
            status = 'succeeded'
            AND attempt_state = 'response_received'
            AND failure_code IS NULL
            AND completed_at IS NOT NULL
        )
        OR
        (
            status = 'failed'
            AND attempt_state IS NOT NULL
            AND failure_code IS NOT NULL
            AND completed_at IS NOT NULL
        )
    ),

    CHECK (
        completed_at IS NULL
        OR completed_at >= started_at
    )
);

CREATE INDEX tokenio_telegram_delivery_attempts_alert_idx
    ON tokenio_telegram_delivery_attempts (
        alert_id,
        attempt_number
    );

CREATE INDEX tokenio_telegram_delivery_attempts_open_idx
    ON tokenio_telegram_delivery_attempts (
        started_at,
        alert_id,
        attempt_number
    )
    WHERE status = 'started';

CREATE INDEX tokenio_telegram_delivery_attempts_recovery_idx
    ON tokenio_telegram_delivery_attempts (
        attempt_state,
        completed_at,
        alert_id,
        attempt_number
    )
    WHERE status = 'failed';
