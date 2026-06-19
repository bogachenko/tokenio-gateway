ALTER TABLE tokenio_telegram_delivery_attempts
    ADD COLUMN telegram_message_id TEXT;

ALTER TABLE tokenio_telegram_delivery_attempts
    ADD CONSTRAINT tokenio_telegram_delivery_attempts_message_id_shape_chk
    CHECK (
        (
            status = 'succeeded'
            AND (
                telegram_message_id IS NULL
                OR btrim(telegram_message_id) <> ''
            )
        )
        OR
        (
            status <> 'succeeded'
            AND telegram_message_id IS NULL
        )
    );
