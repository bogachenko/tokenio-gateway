ALTER TABLE tokenio_telegram_delivery_attempts
    DROP CONSTRAINT IF EXISTS tokenio_telegram_delivery_attempts_message_id_shape_chk;

ALTER TABLE tokenio_telegram_delivery_attempts
    DROP COLUMN IF EXISTS telegram_message_id;
