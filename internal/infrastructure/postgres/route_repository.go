package postgres

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const listModelCatalogRoutesSQL = `
SELECT
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
FROM tokenio_routes
WHERE api_family = $1
ORDER BY
    client_model ASC,
    endpoint_kind ASC,
    priority ASC,
    id ASC
`

const findRoutesSQL = `
SELECT
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
FROM tokenio_routes
WHERE api_family = $1
  AND endpoint_kind = $2
  AND client_model = $3
ORDER BY priority ASC, id ASC
`

type RouteRepository struct {
	db DBTX
}

var _ ports.RouteRepository = (*RouteRepository)(nil)
var _ ports.ModelCatalogRouteRepository = (*RouteRepository)(nil)

func NewRouteRepository(db DBTX) (*RouteRepository, error) {
	if db == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &RouteRepository{db: db}, nil
}

func (r *RouteRepository) ListModelCatalogRoutes(
	ctx context.Context,
	apiFamily domain.APIFamily,
) ([]domain.Route, error) {
	if !validModelCatalogAPIFamily(apiFamily) {
		return nil, ports.ErrStoreContractViolation
	}
	if r == nil || r.db == nil {
		return nil, ErrInvalidDatabaseConfig
	}

	rows, err := r.db.Query(
		ctx,
		listModelCatalogRoutesSQL,
		string(apiFamily),
	)
	if err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	defer rows.Close()

	result := make([]domain.Route, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		value, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		if value.APIFamily != apiFamily {
			return nil, ports.ErrStoreContractViolation
		}
		if _, exists := seen[value.ID]; exists {
			return nil, ports.ErrStoreContractViolation
		}
		seen[value.ID] = struct{}{}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	return result, nil
}

func validModelCatalogAPIFamily(
	value domain.APIFamily,
) bool {
	switch value {
	case domain.APIFamilyOpenAICompatible,
		domain.APIFamilyGeminiNative,
		domain.APIFamilyAnthropicNative,
		domain.APIFamilyOllamaNative:
		return true
	default:
		return false
	}
}

func (r *RouteRepository) FindRoutes(
	ctx context.Context,
	query ports.RouteQuery,
) ([]domain.Route, error) {
	rows, err := r.db.Query(
		ctx,
		findRoutesSQL,
		string(query.APIFamily),
		string(query.EndpointKind),
		query.ClientModel,
	)
	if err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	defer rows.Close()

	result := make([]domain.Route, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		value, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[value.ID]; exists {
			return nil, ports.ErrStoreContractViolation
		}
		seen[value.ID] = struct{}{}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, normalizeRegistryReadError(err)
	}
	return result, nil
}

func scanRoute(row pgx.Row) (domain.Route, error) {
	var value domain.Route
	var providerType string
	var apiFamily string
	var endpointKind string
	var rewritePolicy string
	var capabilitiesJSON []byte
	var cooldownUntil pgtype.Timestamptz
	var cooldownReason pgtype.Text
	var lastErrorCode pgtype.Text
	var lastErrorAt pgtype.Timestamptz
	var disabledAt pgtype.Timestamptz

	if err := row.Scan(
		&value.ID,
		&value.ResellerID,
		&providerType,
		&apiFamily,
		&endpointKind,
		&value.ClientModel,
		&value.ProviderModel,
		&rewritePolicy,
		&value.Enabled,
		&value.Priority,
		&value.RequestsPerMinute,
		&value.TokensPerMinute,
		&value.ConcurrentRequests,
		&value.DefaultMaxOutputTokens,
		&capabilitiesJSON,
		&cooldownUntil,
		&cooldownReason,
		&lastErrorCode,
		&lastErrorAt,
		&value.CreatedAt,
		&value.UpdatedAt,
		&disabledAt,
	); err != nil {
		return domain.Route{}, normalizeRegistryReadError(err)
	}

	capabilities, err := decodeCapabilities(capabilitiesJSON)
	if err != nil {
		return domain.Route{}, err
	}

	value.ProviderType = domain.ProviderType(providerType)
	value.APIFamily = domain.APIFamily(apiFamily)
	value.EndpointKind = domain.EndpointKind(endpointKind)
	value.ModelRewritePolicy = domain.ModelRewritePolicy(rewritePolicy)
	value.Capabilities = capabilities
	value.CooldownUntil = optionalTime(cooldownUntil)
	value.CooldownReason = optionalText(cooldownReason)
	value.LastErrorCode = optionalText(lastErrorCode)
	value.LastErrorAt = optionalTime(lastErrorAt)
	value.CreatedAt = canonicalTime(value.CreatedAt)
	value.UpdatedAt = canonicalTime(value.UpdatedAt)
	value.DisabledAt = optionalTime(disabledAt)
	return value, nil
}
