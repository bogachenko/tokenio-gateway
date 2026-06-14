DO $$
DECLARE
    invalid_count BIGINT;
BEGIN
    SELECT COUNT(*)
    INTO invalid_count
    FROM tokenio_routes
    WHERE
        jsonb_typeof(capabilities) <> 'object'
        OR NOT (
            (
                endpoint_kind = 'chat'
                AND default_max_output_tokens > 0
                AND COALESCE(capabilities -> 'chat', 'false'::jsonb) = 'true'::jsonb
                AND COALESCE(capabilities -> 'embeddings', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'images_generation', 'false'::jsonb) = 'false'::jsonb
                AND (
                    COALESCE(capabilities -> 'tool_choice', 'false'::jsonb) = 'false'::jsonb
                    OR COALESCE(capabilities -> 'tools', 'false'::jsonb) = 'true'::jsonb
                )
                AND (
                    COALESCE(capabilities -> 'json_schema', 'false'::jsonb) = 'false'::jsonb
                    OR COALESCE(capabilities -> 'response_format', 'false'::jsonb) = 'true'::jsonb
                )
            )
            OR
            (
                endpoint_kind = 'embeddings'
                AND default_max_output_tokens = 0
                AND COALESCE(capabilities -> 'chat', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'embeddings', 'false'::jsonb) = 'true'::jsonb
                AND COALESCE(capabilities -> 'images_generation', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'tools', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'tool_choice', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'response_format', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'json_schema', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'image_input', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'audio_input', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'file_input', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'video_input', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'reasoning', 'false'::jsonb) = 'false'::jsonb
            )
            OR
            (
                endpoint_kind = 'images_generation'
                AND default_max_output_tokens = 0
                AND COALESCE(capabilities -> 'chat', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'embeddings', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'images_generation', 'false'::jsonb) = 'true'::jsonb
                AND COALESCE(capabilities -> 'tools', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'tool_choice', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'response_format', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'json_schema', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'image_input', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'audio_input', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'file_input', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'video_input', 'false'::jsonb) = 'false'::jsonb
                AND COALESCE(capabilities -> 'reasoning', 'false'::jsonb) = 'false'::jsonb
            )
        );

    IF invalid_count > 0 THEN
        RAISE EXCEPTION
            'cannot enforce route endpoint contracts: % invalid tokenio_routes rows',
            invalid_count;
    END IF;
END
$$;

ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_capabilities_object_chk
CHECK (jsonb_typeof(capabilities) = 'object');

ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_endpoint_configuration_chk
CHECK (
    (
        endpoint_kind = 'chat'
        AND default_max_output_tokens > 0
        AND COALESCE(capabilities -> 'chat', 'false'::jsonb) = 'true'::jsonb
        AND COALESCE(capabilities -> 'embeddings', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'images_generation', 'false'::jsonb) = 'false'::jsonb
        AND (
            COALESCE(capabilities -> 'tool_choice', 'false'::jsonb) = 'false'::jsonb
            OR COALESCE(capabilities -> 'tools', 'false'::jsonb) = 'true'::jsonb
        )
        AND (
            COALESCE(capabilities -> 'json_schema', 'false'::jsonb) = 'false'::jsonb
            OR COALESCE(capabilities -> 'response_format', 'false'::jsonb) = 'true'::jsonb
        )
    )
    OR
    (
        endpoint_kind = 'embeddings'
        AND default_max_output_tokens = 0
        AND COALESCE(capabilities -> 'chat', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'embeddings', 'false'::jsonb) = 'true'::jsonb
        AND COALESCE(capabilities -> 'images_generation', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'tools', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'tool_choice', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'response_format', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'json_schema', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'image_input', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'audio_input', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'file_input', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'video_input', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'reasoning', 'false'::jsonb) = 'false'::jsonb
    )
    OR
    (
        endpoint_kind = 'images_generation'
        AND default_max_output_tokens = 0
        AND COALESCE(capabilities -> 'chat', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'embeddings', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'images_generation', 'false'::jsonb) = 'true'::jsonb
        AND COALESCE(capabilities -> 'tools', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'tool_choice', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'response_format', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'json_schema', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'image_input', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'audio_input', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'file_input', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'video_input', 'false'::jsonb) = 'false'::jsonb
        AND COALESCE(capabilities -> 'reasoning', 'false'::jsonb) = 'false'::jsonb
    )
);
