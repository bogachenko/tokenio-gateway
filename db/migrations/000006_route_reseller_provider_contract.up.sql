DO $$
DECLARE
    invalid_count BIGINT;
BEGIN
    SELECT COUNT(*)
    INTO invalid_count
    FROM tokenio_routes AS route
    JOIN tokenio_resellers AS reseller
      ON reseller.id = route.reseller_id
    WHERE route.provider_type <> reseller.provider_type;

    IF invalid_count > 0 THEN
        RAISE EXCEPTION
            'cannot enforce route reseller provider contract: % invalid tokenio_routes rows',
            invalid_count;
    END IF;
END
$$;

CREATE FUNCTION tokenio_validate_route_reseller_provider_contract(
    checked_reseller_id TEXT
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    reseller_provider_type TEXT;
    invalid_count BIGINT;
BEGIN
    PERFORM pg_advisory_xact_lock(
        hashtextextended(checked_reseller_id, 0)
    );

    SELECT provider_type
    INTO reseller_provider_type
    FROM tokenio_resellers
    WHERE id = checked_reseller_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT COUNT(*)
    INTO invalid_count
    FROM tokenio_routes
    WHERE reseller_id = checked_reseller_id
      AND provider_type <> reseller_provider_type;

    IF invalid_count > 0 THEN
        RAISE EXCEPTION
            'reseller % provider_type is incompatible with % route rows',
            checked_reseller_id,
            invalid_count;
    END IF;
END
$$;

CREATE FUNCTION tokenio_enforce_route_reseller_provider_contract()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    checked_reseller_id TEXT;
BEGIN
    IF TG_TABLE_NAME = 'tokenio_routes' THEN
        checked_reseller_id := NEW.reseller_id;
    ELSIF TG_TABLE_NAME = 'tokenio_resellers' THEN
        checked_reseller_id := NEW.id;
    ELSE
        RAISE EXCEPTION
            'unexpected table % for route reseller provider contract',
            TG_TABLE_NAME;
    END IF;

    PERFORM tokenio_validate_route_reseller_provider_contract(
        checked_reseller_id
    );
    RETURN NULL;
END
$$;

CREATE CONSTRAINT TRIGGER tokenio_routes_reseller_provider_contract_trg
AFTER INSERT OR UPDATE OF reseller_id, provider_type
ON tokenio_routes
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION tokenio_enforce_route_reseller_provider_contract();

CREATE CONSTRAINT TRIGGER tokenio_resellers_route_provider_contract_trg
AFTER UPDATE OF provider_type
ON tokenio_resellers
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION tokenio_enforce_route_reseller_provider_contract();
