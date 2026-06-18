ALTER TABLE tokenio_admin_audit_log
    DROP CONSTRAINT tokenio_admin_audit_reason_non_blank;

ALTER TABLE tokenio_admin_audit_log
    DROP COLUMN reason;
