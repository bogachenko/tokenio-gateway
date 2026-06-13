package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const adminRouteColumns = `
    id,
    reseller_id,
    provider_type,
    api_family,
    endpoint_kind,
    client_model,
    provider_model,
    model_rewrite_policy,
    enabled,
    priority,
    requests_per_minute,
    tokens_per_minute,
    concurrent_requests,
    default_max_output_tokens,
    capabilities,
    cooldown_until,
    cooldown_reason,
    last_error_code,
    last_error_at,
    created_at,
    updated_at,
    disabled_at
`

const findAdminRouteByIDSQL = `
SELECT
` + adminRouteColumns + `
FROM tokenio_routes
WHERE id = $1
`

const findAdminRouteByIDForUpdateSQL = `
SELECT
` + adminRouteColumns + `
FROM tokenio_routes
WHERE id = $1
FOR UPDATE
`

const insertAdminRouteSQL = `
INSERT INTO tokenio_routes (
    id,
    reseller_id,
    provider_type,
    api_family,
    endpoint_kind,
    client_model,
    provider_model,
    model_rewrite_policy,
    enabled,
    priority,
    requests_per_minute,
    tokens_per_minute,
    concurrent_requests,
    default_max_output_tokens,
    capabilities,
    cooldown_until,
    cooldown_reason,
    last_error_code,
    last_error_at,
    created_at,
    updated_at,
    disabled_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
    $12, $13, $14, $15::jsonb, $16, $17, $18, $19,
    $20, $21, $22
)
RETURNING
` + adminRouteColumns

const updateAdminRouteSQL = `
UPDATE tokenio_routes
SET
    reseller_id = $2,
    provider_type = $3,
    api_family = $4,
    endpoint_kind = $5,
    client_model = $6,
    provider_model = $7,
    model_rewrite_policy = $8,
    enabled = $9,
    priority = $10,
    requests_per_minute = $11,
    tokens_per_minute = $12,
    concurrent_requests = $13,
    default_max_output_tokens = $14,
    capabilities = $15::jsonb,
    cooldown_until = $16,
    cooldown_reason = $17,
    last_error_code = $18,
    last_error_at = $19,
    created_at = $20,
    updated_at = $21,
    disabled_at = $22
WHERE id = $1
RETURNING
` + adminRouteColumns

type AdminRouteRepository struct {
	db *DB
}

var _ ports.AdminRouteRepository = (*AdminRouteRepository)(nil)

func NewAdminRouteRepository(
	db *DB,
) (*AdminRouteRepository, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &AdminRouteRepository{db: db}, nil
}

func (r *AdminRouteRepository) FindRoutes(
	ctx context.Context,
	query ports.RouteQuery,
) ([]domain.Route, error) {
	return (&RouteRepository{db: r.db}).FindRoutes(ctx, query)
}

func (r *AdminRouteRepository) FindRouteByID(
	ctx context.Context,
	routeID string,
) (*domain.Route, error) {
	value, err := scanRoute(
		r.db.QueryRow(ctx, findAdminRouteByIDSQL, routeID),
	)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (r *AdminRouteRepository) ListRoutes(
	ctx context.Context,
	filter ports.RouteListFilter,
) (ports.Page[domain.Route], error) {
	if err := validateAdminPage(filter.Page); err != nil {
		return ports.Page[domain.Route]{}, err
	}

	where, args := buildAdminRouteFilter(filter)
	var result ports.Page[domain.Route]

	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			countSQL := "SELECT COUNT(*) FROM tokenio_routes" + where
			if err := tx.QueryRow(ctx, countSQL, args...).Scan(&result.Total); err != nil {
				return normalizeRegistryReadError(err)
			}

			listArgs := append([]any(nil), args...)
			limitPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Limit)
			offsetPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Offset)

			listSQL := `
SELECT
` + adminRouteColumns + `
FROM tokenio_routes` + where + fmt.Sprintf(`
ORDER BY created_at DESC, id ASC
LIMIT $%d OFFSET $%d
`, limitPosition, offsetPosition)

			rows, err := tx.Query(ctx, listSQL, listArgs...)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			result.Items = make([]domain.Route, 0)
			for rows.Next() {
				value, err := scanRoute(rows)
				if err != nil {
					return err
				}
				result.Items = append(result.Items, value)
			}
			if err := rows.Err(); err != nil {
				return normalizeRegistryReadError(err)
			}
			return nil
		},
	)
	if err != nil {
		return ports.Page[domain.Route]{}, err
	}
	return result, nil
}

func (r *AdminRouteRepository) CreateRouteWithAudit(
	ctx context.Context,
	requested domain.Route,
	audit domain.AuditContext,
) (domain.Route, error) {
	if err := validateAdminRouteRecord(requested); err != nil {
		return domain.Route{}, err
	}
	if requested.DisabledAt != nil ||
		!postgresAdminTime(requested.CreatedAt).Equal(
			postgresAdminTime(requested.UpdatedAt),
		) {
		return domain.Route{}, ports.ErrStoreContractViolation
	}
	if err := validateAuditForEntity(
		audit,
		domain.AuditActionRouteCreate,
		"route",
		requested.ID,
		domain.AuditState{},
		adminRouteApplicationState(requested),
		requested.CreatedAt,
	); err != nil {
		return domain.Route{}, err
	}

	persistedInput := canonicalAdminRoute(requested)
	var created domain.Route
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			if err := validateRouteResellerProvider(
				ctx,
				tx,
				persistedInput.ResellerID,
				persistedInput.ProviderType,
			); err != nil {
				return err
			}

			capabilities, err := encodeAdminRouteCapabilities(
				persistedInput.Capabilities,
			)
			if err != nil {
				return err
			}
			value, err := scanRoute(tx.QueryRow(
				ctx,
				insertAdminRouteSQL,
				persistedInput.ID,
				persistedInput.ResellerID,
				string(persistedInput.ProviderType),
				string(persistedInput.APIFamily),
				string(persistedInput.EndpointKind),
				persistedInput.ClientModel,
				persistedInput.ProviderModel,
				string(persistedInput.ModelRewritePolicy),
				persistedInput.Enabled,
				persistedInput.Priority,
				persistedInput.RequestsPerMinute,
				persistedInput.TokensPerMinute,
				persistedInput.ConcurrentRequests,
				persistedInput.DefaultMaxOutputTokens,
				capabilities,
				adminRouteTimeArg(
					persistedInput.CooldownUntil,
				),
				nullIfEmpty(persistedInput.CooldownReason),
				nullIfEmpty(persistedInput.LastErrorCode),
				adminRouteTimeArg(
					persistedInput.LastErrorAt,
				),
				persistedInput.CreatedAt,
				persistedInput.UpdatedAt,
				adminRouteTimeArg(persistedInput.DisabledAt),
			))
			if err != nil {
				return normalizeAdminWriteError(err)
			}
			if !sameAdminRoute(value, persistedInput) {
				return ports.ErrStoreContractViolation
			}

			persistedAudit := canonicalRouteAudit(
				audit,
				domain.AuditState{},
				adminRouteState(value),
				value.CreatedAt,
			)
			if err := insertAdminAudit(ctx, tx, persistedAudit); err != nil {
				return err
			}
			created = value
			return nil
		},
	)
	if err != nil {
		if errors.Is(err, ports.ErrStoreConflict) {
			return domain.Route{}, ports.ErrAdminConflict
		}
		return domain.Route{}, err
	}
	return created, nil
}

func (r *AdminRouteRepository) CompareAndSwapRouteWithAudit(
	ctx context.Context,
	expected domain.Route,
	next domain.Route,
	audit domain.AuditContext,
) (domain.Route, error) {
	if err := validateAdminRouteRecord(expected); err != nil {
		return domain.Route{}, err
	}
	if err := validateAdminRouteRecord(next); err != nil {
		return domain.Route{}, err
	}
	if err := validateAdminRouteMutation(
		expected,
		next,
		audit.Action,
	); err != nil {
		return domain.Route{}, err
	}
	if err := validateAuditForEntity(
		audit,
		audit.Action,
		"route",
		next.ID,
		adminRouteApplicationState(expected),
		adminRouteApplicationState(next),
		next.UpdatedAt,
	); err != nil {
		return domain.Route{}, err
	}

	persistedNext := canonicalAdminRoute(next)
	var updated domain.Route
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := scanRoute(tx.QueryRow(
				ctx,
				findAdminRouteByIDForUpdateSQL,
				expected.ID,
			))
			if err != nil {
				return err
			}
			if !sameAdminRoute(current, expected) {
				return ports.ErrAdminStateConflict
			}
			if err := validateRouteResellerProvider(
				ctx,
				tx,
				persistedNext.ResellerID,
				persistedNext.ProviderType,
			); err != nil {
				return err
			}

			capabilities, err := encodeAdminRouteCapabilities(
				persistedNext.Capabilities,
			)
			if err != nil {
				return err
			}
			value, err := scanRoute(tx.QueryRow(
				ctx,
				updateAdminRouteSQL,
				persistedNext.ID,
				persistedNext.ResellerID,
				string(persistedNext.ProviderType),
				string(persistedNext.APIFamily),
				string(persistedNext.EndpointKind),
				persistedNext.ClientModel,
				persistedNext.ProviderModel,
				string(persistedNext.ModelRewritePolicy),
				persistedNext.Enabled,
				persistedNext.Priority,
				persistedNext.RequestsPerMinute,
				persistedNext.TokensPerMinute,
				persistedNext.ConcurrentRequests,
				persistedNext.DefaultMaxOutputTokens,
				capabilities,
				adminRouteTimeArg(
					persistedNext.CooldownUntil,
				),
				nullIfEmpty(persistedNext.CooldownReason),
				nullIfEmpty(persistedNext.LastErrorCode),
				adminRouteTimeArg(persistedNext.LastErrorAt),
				persistedNext.CreatedAt,
				persistedNext.UpdatedAt,
				adminRouteTimeArg(persistedNext.DisabledAt),
			))
			if err != nil {
				return normalizeAdminWriteError(err)
			}
			if !sameAdminRoute(value, persistedNext) {
				return ports.ErrStoreContractViolation
			}

			persistedAudit := canonicalRouteAudit(
				audit,
				adminRouteState(current),
				adminRouteState(value),
				value.UpdatedAt,
			)
			if err := insertAdminAudit(ctx, tx, persistedAudit); err != nil {
				return err
			}
			updated = value
			return nil
		},
	)
	if err != nil {
		if errors.Is(err, ports.ErrStoreConflict) {
			return domain.Route{}, ports.ErrAdminStateConflict
		}
		return domain.Route{}, err
	}
	return updated, nil
}

func validateRouteResellerProvider(
	ctx context.Context,
	tx pgx.Tx,
	resellerID string,
	providerType domain.ProviderType,
) error {
	var persistedProvider string
	if err := tx.QueryRow(
		ctx,
		`
SELECT provider_type
FROM tokenio_resellers
WHERE id = $1
FOR KEY SHARE
`,
		resellerID,
	).Scan(&persistedProvider); err != nil {
		if errors.Is(
			normalizeRegistryReadError(err),
			ports.ErrNotFound,
		) {
			return ports.ErrAdminConflict
		}
		return normalizeRegistryReadError(err)
	}
	if domain.ProviderType(persistedProvider) != providerType {
		return ports.ErrAdminConflict
	}
	return nil
}

func buildAdminRouteFilter(
	filter ports.RouteListFilter,
) (string, []any) {
	var clauses []string
	var args []any

	add := func(expression string, value any) {
		args = append(args, value)
		clauses = append(
			clauses,
			fmt.Sprintf(expression, len(args)),
		)
	}

	if filter.ResellerID != "" {
		add("reseller_id = $%d", filter.ResellerID)
	}
	if filter.ProviderType != "" {
		add("provider_type = $%d", string(filter.ProviderType))
	}
	if filter.APIFamily != "" {
		add("api_family = $%d", string(filter.APIFamily))
	}
	if filter.EndpointKind != "" {
		add("endpoint_kind = $%d", string(filter.EndpointKind))
	}
	if filter.ClientModel != "" {
		add("client_model = $%d", filter.ClientModel)
	}
	if filter.Enabled != nil {
		add("enabled = $%d", *filter.Enabled)
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
