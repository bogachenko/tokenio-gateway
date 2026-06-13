package postgres

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const findRoutePricesByIDsSQL = `
SELECT
    route_id,
    currency,
    input_price_per_1m_tokens_cents,
    cached_input_price_per_1m_tokens_cents,
    output_price_per_1m_tokens_cents,
    reasoning_output_price_per_1m_tokens_cents,
    image_input_price_per_1m_tokens_cents,
    audio_input_price_per_1m_tokens_cents,
    audio_output_price_per_1m_tokens_cents,
    file_input_price_per_1m_tokens_cents,
    video_input_price_per_1m_tokens_cents,
    image_generation_price_per_unit_cents,
    image_generation_unit_kind,
    markup_coefficient,
    enabled,
    created_at,
    updated_at
FROM tokenio_route_prices
WHERE route_id = ANY($1::text[])
ORDER BY route_id ASC
`

type RoutePriceRepository struct {
	db DBTX
}

var _ ports.RoutePriceRepository = (*RoutePriceRepository)(nil)

func NewRoutePriceRepository(db DBTX) (*RoutePriceRepository, error) {
	if db == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &RoutePriceRepository{db: db}, nil
}

func (r *RoutePriceRepository) FindByRouteIDs(
	ctx context.Context,
	routeIDs []string,
) (map[string]domain.RoutePrice, error) {
	ids := uniqueIDs(routeIDs)
	result := make(map[string]domain.RoutePrice, len(ids))
	if len(ids) == 0 {
		return result, nil
	}

	rows, err := r.db.Query(ctx, findRoutePricesByIDsSQL, ids)
	if err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	defer rows.Close()

	for rows.Next() {
		value, err := scanRoutePrice(rows)
		if err != nil {
			return nil, err
		}
		if _, exists := result[value.RouteID]; exists {
			return nil, ports.ErrStoreContractViolation
		}
		result[value.RouteID] = value
	}
	if err := rows.Err(); err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	return result, nil
}

func scanRoutePrice(row pgx.Row) (domain.RoutePrice, error) {
	var value domain.RoutePrice
	var imageGenerationUnitKind string

	if err := row.Scan(
		&value.RouteID,
		&value.Currency,
		&value.InputPricePer1MTokensCents,
		&value.CachedInputPricePer1MTokensCents,
		&value.OutputPricePer1MTokensCents,
		&value.ReasoningOutputPricePer1MTokensCents,
		&value.ImageInputPricePer1MTokensCents,
		&value.AudioInputPricePer1MTokensCents,
		&value.AudioOutputPricePer1MTokensCents,
		&value.FileInputPricePer1MTokensCents,
		&value.VideoInputPricePer1MTokensCents,
		&value.ImageGenerationPricePerUnitCents,
		&imageGenerationUnitKind,
		&value.MarkupCoefficient,
		&value.Enabled,
		&value.CreatedAt,
		&value.UpdatedAt,
	); err != nil {
		return domain.RoutePrice{}, normalizeRegistryReadError(err)
	}

	value.ImageGenerationUnitKind =
		domain.ImageGenerationUnitKind(imageGenerationUnitKind)
	value.CreatedAt = canonicalTime(value.CreatedAt)
	value.UpdatedAt = canonicalTime(value.UpdatedAt)

	if err := validateRoutePricePersistence(value); err != nil {
		return domain.RoutePrice{}, err
	}
	return value, nil
}
