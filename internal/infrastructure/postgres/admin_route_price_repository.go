package postgres

import (
	"context"
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const adminRoutePriceColumns = `
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
`

const findAdminRoutePriceSQL = `
SELECT
` + adminRoutePriceColumns + `
FROM tokenio_route_prices
WHERE route_id = $1
`

const findAdminRoutePriceForUpdateSQL = `
SELECT
` + adminRoutePriceColumns + `
FROM tokenio_route_prices
WHERE route_id = $1
FOR UPDATE
`

const insertAdminRoutePriceSQL = `
INSERT INTO tokenio_route_prices (
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
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    $10, $11, $12, $13, $14, $15, $16, $17
)
RETURNING
` + adminRoutePriceColumns

const updateAdminRoutePriceSQL = `
UPDATE tokenio_route_prices
SET
    currency = $2,
    input_price_per_1m_tokens_cents = $3,
    cached_input_price_per_1m_tokens_cents = $4,
    output_price_per_1m_tokens_cents = $5,
    reasoning_output_price_per_1m_tokens_cents = $6,
    image_input_price_per_1m_tokens_cents = $7,
    audio_input_price_per_1m_tokens_cents = $8,
    audio_output_price_per_1m_tokens_cents = $9,
    file_input_price_per_1m_tokens_cents = $10,
    video_input_price_per_1m_tokens_cents = $11,
    image_generation_price_per_unit_cents = $12,
    image_generation_unit_kind = $13,
    markup_coefficient = $14,
    enabled = $15,
    created_at = $16,
    updated_at = $17
WHERE route_id = $1
RETURNING
` + adminRoutePriceColumns

type AdminRoutePriceRepository struct {
	db *DB
}

var _ ports.AdminRoutePriceRepository = (*AdminRoutePriceRepository)(nil)

func NewAdminRoutePriceRepository(
	db *DB,
) (*AdminRoutePriceRepository, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &AdminRoutePriceRepository{db: db}, nil
}

func (r *AdminRoutePriceRepository) FindByRouteIDs(
	ctx context.Context,
	routeIDs []string,
) (map[string]domain.RoutePrice, error) {
	return (&RoutePriceRepository{db: r.db}).FindByRouteIDs(
		ctx,
		routeIDs,
	)
}

func (r *AdminRoutePriceRepository) FindRoutePrice(
	ctx context.Context,
	routeID string,
) (*domain.RoutePrice, error) {
	value, err := scanRoutePrice(
		r.db.QueryRow(ctx, findAdminRoutePriceSQL, routeID),
	)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (r *AdminRoutePriceRepository) UpsertRoutePriceWithAudit(
	ctx context.Context,
	expected *domain.RoutePrice,
	next domain.RoutePrice,
	audit domain.AuditContext,
) (domain.RoutePrice, error) {
	if err := validateAdminRoutePriceRecord(next); err != nil {
		return domain.RoutePrice{}, err
	}

	beforeState := domain.AuditState{}
	if expected == nil {
		if !postgresAdminTime(next.CreatedAt).Equal(
			postgresAdminTime(next.UpdatedAt),
		) {
			return domain.RoutePrice{},
				ports.ErrStoreContractViolation
		}
	} else {
		if err := validateAdminRoutePriceRecord(*expected); err != nil {
			return domain.RoutePrice{}, err
		}
		if expected.RouteID != next.RouteID ||
			expected.Currency != next.Currency ||
			!postgresAdminTime(expected.CreatedAt).Equal(
				postgresAdminTime(next.CreatedAt),
			) ||
			!postgresAdminTime(next.UpdatedAt).After(
				postgresAdminTime(expected.UpdatedAt),
			) {
			return domain.RoutePrice{},
				ports.ErrStoreContractViolation
		}
		beforeState = adminRoutePriceApplicationState(*expected)
	}

	if err := validateAuditForEntity(
		audit,
		domain.AuditActionRoutePriceUpsert,
		"route_price",
		next.RouteID,
		beforeState,
		adminRoutePriceApplicationState(next),
		next.UpdatedAt,
	); err != nil {
		return domain.RoutePrice{}, err
	}

	persistedNext := canonicalAdminRoutePrice(next)
	var result domain.RoutePrice
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			if err := lockRouteForPrice(
				ctx,
				tx,
				next.RouteID,
			); err != nil {
				return err
			}

			if expected == nil {
				_, err := scanRoutePrice(tx.QueryRow(
					ctx,
					findAdminRoutePriceForUpdateSQL,
					next.RouteID,
				))
				switch {
				case err == nil:
					return ports.ErrAdminStateConflict
				case errors.Is(err, ports.ErrNotFound):
				default:
					return err
				}

				value, err := scanRoutePrice(tx.QueryRow(
					ctx,
					insertAdminRoutePriceSQL,
					persistedNext.RouteID,
					persistedNext.Currency,
					persistedNext.
						InputPricePer1MTokensCents,
					persistedNext.
						CachedInputPricePer1MTokensCents,
					persistedNext.
						OutputPricePer1MTokensCents,
					persistedNext.
						ReasoningOutputPricePer1MTokensCents,
					persistedNext.
						ImageInputPricePer1MTokensCents,
					persistedNext.
						AudioInputPricePer1MTokensCents,
					persistedNext.
						AudioOutputPricePer1MTokensCents,
					persistedNext.
						FileInputPricePer1MTokensCents,
					persistedNext.
						VideoInputPricePer1MTokensCents,
					persistedNext.
						ImageGenerationPricePerUnitCents,
					string(
						persistedNext.
							ImageGenerationUnitKind,
					),
					persistedNext.MarkupCoefficient,
					persistedNext.Enabled,
					persistedNext.CreatedAt,
					persistedNext.UpdatedAt,
				))
				if err != nil {
					return normalizeAdminWriteError(err)
				}
				if !sameAdminRoutePrice(
					value,
					persistedNext,
				) {
					return ports.ErrStoreContractViolation
				}
				result = value
			} else {
				current, err := scanRoutePrice(tx.QueryRow(
					ctx,
					findAdminRoutePriceForUpdateSQL,
					next.RouteID,
				))
				if err != nil {
					return err
				}
				if !sameAdminRoutePrice(current, *expected) {
					return ports.ErrAdminStateConflict
				}

				value, err := scanRoutePrice(tx.QueryRow(
					ctx,
					updateAdminRoutePriceSQL,
					persistedNext.RouteID,
					persistedNext.Currency,
					persistedNext.
						InputPricePer1MTokensCents,
					persistedNext.
						CachedInputPricePer1MTokensCents,
					persistedNext.
						OutputPricePer1MTokensCents,
					persistedNext.
						ReasoningOutputPricePer1MTokensCents,
					persistedNext.
						ImageInputPricePer1MTokensCents,
					persistedNext.
						AudioInputPricePer1MTokensCents,
					persistedNext.
						AudioOutputPricePer1MTokensCents,
					persistedNext.
						FileInputPricePer1MTokensCents,
					persistedNext.
						VideoInputPricePer1MTokensCents,
					persistedNext.
						ImageGenerationPricePerUnitCents,
					string(
						persistedNext.
							ImageGenerationUnitKind,
					),
					persistedNext.MarkupCoefficient,
					persistedNext.Enabled,
					persistedNext.CreatedAt,
					persistedNext.UpdatedAt,
				))
				if err != nil {
					return normalizeAdminWriteError(err)
				}
				if !sameAdminRoutePrice(
					value,
					persistedNext,
				) {
					return ports.ErrStoreContractViolation
				}
				result = value
			}

			persistedBefore := domain.AuditState{}
			if expected != nil {
				persistedBefore =
					adminRoutePriceState(*expected)
			}
			persistedAudit := canonicalRoutePriceAudit(
				audit,
				persistedBefore,
				adminRoutePriceState(result),
				result.UpdatedAt,
			)
			return insertAdminAudit(ctx, tx, persistedAudit)
		},
	)
	if err != nil {
		if errors.Is(err, ports.ErrStoreConflict) {
			return domain.RoutePrice{},
				ports.ErrAdminStateConflict
		}
		return domain.RoutePrice{}, err
	}
	return result, nil
}

func lockRouteForPrice(
	ctx context.Context,
	tx pgx.Tx,
	routeID string,
) error {
	var persistedID string
	if err := tx.QueryRow(
		ctx,
		`
SELECT id
FROM tokenio_routes
WHERE id = $1
FOR KEY SHARE
`,
		routeID,
	).Scan(&persistedID); err != nil {
		if errors.Is(
			normalizeRegistryReadError(err),
			ports.ErrNotFound,
		) {
			return ports.ErrAdminConflict
		}
		return normalizeRegistryReadError(err)
	}
	return nil
}
