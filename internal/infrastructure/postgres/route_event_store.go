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

const routeEventColumns = `
    id,
    route_id,
    reseller_id,
    provider_type,
    api_family,
    endpoint_kind,
    client_model,
    event_type,
    reason,
    local_request_id,
    metadata,
    created_at
`

const findRouteEventSQL = `
SELECT
` + routeEventColumns + `
FROM tokenio_route_events
WHERE id = $1
`

const insertRouteEventSQL = `
INSERT INTO tokenio_route_events (
    id,
    route_id,
    reseller_id,
    provider_type,
    api_family,
    endpoint_kind,
    client_model,
    event_type,
    reason,
    local_request_id,
    metadata,
    created_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12
)
`

type RouteEventStore struct {
	db *DB
}

var _ ports.RouteEventStore = (*RouteEventStore)(nil)

func NewRouteEventStore(db *DB) (*RouteEventStore, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &RouteEventStore{db: db}, nil
}

func (s *RouteEventStore) AppendRouteEvent(
	ctx context.Context,
	event domain.RouteEvent,
) error {
	if err := validateRouteEventPersistence(event); err != nil {
		return err
	}
	persisted := canonicalRouteEvent(event)

	return InTx(
		ctx,
		s.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			if _, err := tx.Exec(
				ctx,
				`
SELECT pg_advisory_xact_lock(
    hashtextextended('tokenio_route_event:' || $1, 0)
)
`,
				persisted.ID,
			); err != nil {
				return NormalizeError(err)
			}

			existing, err := scanRouteEvent(
				tx.QueryRow(ctx, findRouteEventSQL, persisted.ID),
			)
			switch {
			case err == nil:
				if sameRouteEvent(existing, persisted) {
					return nil
				}
				return ports.ErrStoreConflict
			case errors.Is(err, ports.ErrNotFound):
			default:
				return err
			}

			if err := validateOperationalReferences(
				ctx,
				tx,
				persisted.RouteID,
				persisted.ResellerID,
				persisted.ProviderType,
				persisted.APIFamily,
				persisted.EndpointKind,
				persisted.ClientModel,
			); err != nil {
				return err
			}

			metadata, err := encodeRouteEventMetadata(
				persisted.Metadata,
			)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(
				ctx,
				insertRouteEventSQL,
				persisted.ID,
				nullIfEmpty(persisted.RouteID),
				nullIfEmpty(persisted.ResellerID),
				nullIfEmpty(string(persisted.ProviderType)),
				nullIfEmpty(string(persisted.APIFamily)),
				nullIfEmpty(string(persisted.EndpointKind)),
				nullIfEmpty(persisted.ClientModel),
				string(persisted.EventType),
				nullIfEmpty(persisted.Reason),
				nullIfEmpty(persisted.LocalRequestID),
				metadata,
				persisted.CreatedAt,
			); err != nil {
				return NormalizeError(err)
			}
			return nil
		},
	)
}

func (s *RouteEventStore) FindRouteEventByID(
	ctx context.Context,
	eventID string,
) (*domain.RouteEvent, error) {
	value, err := scanRouteEvent(
		s.db.QueryRow(ctx, findRouteEventSQL, eventID),
	)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (s *RouteEventStore) ListRouteEvents(
	ctx context.Context,
	filter ports.RouteEventListFilter,
) (ports.Page[domain.RouteEvent], error) {
	if err := validateOperationalPage(filter.Page); err != nil {
		return ports.Page[domain.RouteEvent]{}, err
	}
	if err := validateOperationalWindow(
		filter.CreatedFrom,
		filter.CreatedTo,
	); err != nil {
		return ports.Page[domain.RouteEvent]{}, err
	}
	if filter.EventType != "" &&
		!validRouteEventType(filter.EventType) {
		return ports.Page[domain.RouteEvent]{},
			ports.ErrStoreContractViolation
	}

	where, args := buildRouteEventFilter(filter)
	var result ports.Page[domain.RouteEvent]

	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			if err := tx.QueryRow(
				ctx,
				"SELECT COUNT(*) FROM tokenio_route_events"+where,
				args...,
			).Scan(&result.Total); err != nil {
				return normalizeRegistryReadError(err)
			}

			listArgs := append([]any(nil), args...)
			limitPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Limit)
			offsetPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Offset)

			query := `
SELECT
` + routeEventColumns + `
FROM tokenio_route_events` + where + fmt.Sprintf(`
ORDER BY created_at DESC, id ASC
LIMIT $%d OFFSET $%d
`, limitPosition, offsetPosition)

			rows, err := tx.Query(ctx, query, listArgs...)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			result.Items = make([]domain.RouteEvent, 0)
			for rows.Next() {
				value, err := scanRouteEvent(rows)
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
		return ports.Page[domain.RouteEvent]{}, err
	}
	return result, nil
}

func validateOperationalReferences(
	ctx context.Context,
	tx pgx.Tx,
	routeID string,
	resellerID string,
	providerType domain.ProviderType,
	apiFamily domain.APIFamily,
	endpointKind domain.EndpointKind,
	clientModel string,
) error {
	if routeID != "" {
		var persistedResellerID string
		var persistedProvider string
		var persistedFamily string
		var persistedEndpoint string
		var persistedClientModel string

		if err := tx.QueryRow(
			ctx,
			`
SELECT
    reseller_id,
    provider_type,
    api_family,
    endpoint_kind,
    client_model
FROM tokenio_routes
WHERE id = $1
FOR KEY SHARE
`,
			routeID,
		).Scan(
			&persistedResellerID,
			&persistedProvider,
			&persistedFamily,
			&persistedEndpoint,
			&persistedClientModel,
		); err != nil {
			return normalizeRegistryReadError(err)
		}

		if resellerID != "" &&
			resellerID != persistedResellerID ||
			providerType != "" &&
				string(providerType) != persistedProvider ||
			apiFamily != "" &&
				string(apiFamily) != persistedFamily ||
			endpointKind != "" &&
				string(endpointKind) != persistedEndpoint ||
			clientModel != "" &&
				clientModel != persistedClientModel {
			return ports.ErrStoreConflict
		}
		return nil
	}

	if resellerID != "" {
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
			return normalizeRegistryReadError(err)
		}
		if providerType != "" &&
			string(providerType) != persistedProvider {
			return ports.ErrStoreConflict
		}
	}
	return nil
}

func buildRouteEventFilter(
	filter ports.RouteEventListFilter,
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

	if filter.RouteID != "" {
		add("route_id = $%d", filter.RouteID)
	}
	if filter.ResellerID != "" {
		add("reseller_id = $%d", filter.ResellerID)
	}
	if filter.EventType != "" {
		add("event_type = $%d", string(filter.EventType))
	}
	if filter.LocalRequestID != "" {
		add("local_request_id = $%d", filter.LocalRequestID)
	}
	if filter.CreatedFrom != nil {
		add("created_at >= $%d", operationalTime(*filter.CreatedFrom))
	}
	if filter.CreatedTo != nil {
		add("created_at < $%d", operationalTime(*filter.CreatedTo))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
