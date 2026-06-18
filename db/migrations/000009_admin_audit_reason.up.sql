ALTER TABLE tokenio_admin_audit_log
    ADD COLUMN reason TEXT;

ALTER TABLE tokenio_admin_audit_log
    ADD CONSTRAINT tokenio_admin_audit_reason_non_blank
    CHECK (reason IS NULL OR btrim(reason) <> '');
