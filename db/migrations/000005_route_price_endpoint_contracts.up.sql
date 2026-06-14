DO $$
DECLARE
    invalid_count BIGINT;
BEGIN
    SELECT COUNT(*)
    INTO invalid_count
    FROM tokenio_route_prices AS price
    JOIN tokenio_routes AS route
      ON route.id = price.route_id
    WHERE NOT (
        (
            route.endpoint_kind = 'chat'
            AND price.image_generation_price_per_unit_cents = 0
            AND price.image_generation_unit_kind = 'none'
        )
        OR
        (
            route.endpoint_kind = 'embeddings'
            AND price.cached_input_price_per_1m_tokens_cents = 0
            AND price.output_price_per_1m_tokens_cents = 0
            AND price.reasoning_output_price_per_1m_tokens_cents = 0
            AND price.image_input_price_per_1m_tokens_cents = 0
            AND price.audio_input_price_per_1m_tokens_cents = 0
            AND price.audio_output_price_per_1m_tokens_cents = 0
            AND price.file_input_price_per_1m_tokens_cents = 0
            AND price.video_input_price_per_1m_tokens_cents = 0
            AND price.image_generation_price_per_unit_cents = 0
            AND price.image_generation_unit_kind = 'none'
        )
        OR
        (
            route.endpoint_kind = 'images_generation'
            AND price.input_price_per_1m_tokens_cents = 0
            AND price.cached_input_price_per_1m_tokens_cents = 0
            AND price.output_price_per_1m_tokens_cents = 0
            AND price.reasoning_output_price_per_1m_tokens_cents = 0
            AND price.image_input_price_per_1m_tokens_cents = 0
            AND price.audio_input_price_per_1m_tokens_cents = 0
            AND price.audio_output_price_per_1m_tokens_cents = 0
            AND price.file_input_price_per_1m_tokens_cents = 0
            AND price.video_input_price_per_1m_tokens_cents = 0
            AND price.image_generation_unit_kind = 'generated_image'
        )
    );

    IF invalid_count > 0 THEN
        RAISE EXCEPTION
            'cannot enforce route price endpoint contracts: % invalid tokenio_route_prices rows',
            invalid_count;
    END IF;
END
$$;

CREATE FUNCTION tokenio_validate_route_price_endpoint_contract(
    checked_route_id TEXT
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    route_endpoint_kind TEXT;
    price tokenio_route_prices%ROWTYPE;
BEGIN
    PERFORM pg_advisory_xact_lock(
        hashtextextended(checked_route_id, 0)
    );

    SELECT endpoint_kind
    INTO route_endpoint_kind
    FROM tokenio_routes
    WHERE id = checked_route_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT *
    INTO price
    FROM tokenio_route_prices
    WHERE route_id = checked_route_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF route_endpoint_kind = 'chat' THEN
        IF price.image_generation_price_per_unit_cents <> 0
           OR price.image_generation_unit_kind <> 'none' THEN
            RAISE EXCEPTION
                'route price % is incompatible with chat endpoint',
                checked_route_id;
        END IF;
        RETURN;
    END IF;

    IF route_endpoint_kind = 'embeddings' THEN
        IF price.cached_input_price_per_1m_tokens_cents <> 0
           OR price.output_price_per_1m_tokens_cents <> 0
           OR price.reasoning_output_price_per_1m_tokens_cents <> 0
           OR price.image_input_price_per_1m_tokens_cents <> 0
           OR price.audio_input_price_per_1m_tokens_cents <> 0
           OR price.audio_output_price_per_1m_tokens_cents <> 0
           OR price.file_input_price_per_1m_tokens_cents <> 0
           OR price.video_input_price_per_1m_tokens_cents <> 0
           OR price.image_generation_price_per_unit_cents <> 0
           OR price.image_generation_unit_kind <> 'none' THEN
            RAISE EXCEPTION
                'route price % is incompatible with embeddings endpoint',
                checked_route_id;
        END IF;
        RETURN;
    END IF;

    IF route_endpoint_kind = 'images_generation' THEN
        IF price.input_price_per_1m_tokens_cents <> 0
           OR price.cached_input_price_per_1m_tokens_cents <> 0
           OR price.output_price_per_1m_tokens_cents <> 0
           OR price.reasoning_output_price_per_1m_tokens_cents <> 0
           OR price.image_input_price_per_1m_tokens_cents <> 0
           OR price.audio_input_price_per_1m_tokens_cents <> 0
           OR price.audio_output_price_per_1m_tokens_cents <> 0
           OR price.file_input_price_per_1m_tokens_cents <> 0
           OR price.video_input_price_per_1m_tokens_cents <> 0
           OR price.image_generation_unit_kind <> 'generated_image' THEN
            RAISE EXCEPTION
                'route price % is incompatible with images_generation endpoint',
                checked_route_id;
        END IF;
        RETURN;
    END IF;

    RAISE EXCEPTION
        'route % has unsupported endpoint kind %',
        checked_route_id,
        route_endpoint_kind;
END
$$;

CREATE FUNCTION tokenio_enforce_route_price_endpoint_contract()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    checked_route_id TEXT;
BEGIN
    IF TG_TABLE_NAME = 'tokenio_route_prices' THEN
        checked_route_id := NEW.route_id;
    ELSIF TG_TABLE_NAME = 'tokenio_routes' THEN
        checked_route_id := NEW.id;
    ELSE
        RAISE EXCEPTION
            'unexpected table % for route price endpoint contract',
            TG_TABLE_NAME;
    END IF;

    PERFORM tokenio_validate_route_price_endpoint_contract(
        checked_route_id
    );
    RETURN NULL;
END
$$;

CREATE CONSTRAINT TRIGGER tokenio_route_prices_endpoint_contract_trg
AFTER INSERT OR UPDATE ON tokenio_route_prices
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION tokenio_enforce_route_price_endpoint_contract();

CREATE CONSTRAINT TRIGGER tokenio_routes_price_endpoint_contract_trg
AFTER UPDATE OF endpoint_kind ON tokenio_routes
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION tokenio_enforce_route_price_endpoint_contract();
