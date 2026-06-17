package postgres

import (
	"context"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

type RouteCooldownStore struct {
	db *DB
}

var _ ports.RouteCooldownStore = (*RouteCooldownStore)(nil)

func NewRouteCooldownStore(db *DB) (*RouteCooldownStore, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &RouteCooldownStore{db: db}, nil
}

func (store *RouteCooldownStore) CompareAndSwapRouteCooldownWithEvent(
	ctx context.Context,
	expected domain.Route,
	next domain.Route,
	event domain.RouteEvent,
) (domain.Route, error) {
	if err := validateRouteCooldownTransition(expected, next, event); err != nil {
		return domain.Route{}, err
	}

	persistedNext := canonicalAdminRoute(next)
	persistedEvent := canonicalRouteEvent(event)
	var updated domain.Route

	err := InTx(
		ctx,
		store.db,
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
				return ports.ErrStoreConflict
			}

			value, err := updateRouteCooldownRow(
				ctx,
				tx,
				persistedNext,
			)
			if err != nil {
				return err
			}
			if !sameAdminRoute(value, persistedNext) {
				return ports.ErrStoreContractViolation
			}
			if err := appendCooldownEventTx(
				ctx,
				tx,
				persistedEvent,
			); err != nil {
				return err
			}
			updated = value
			return nil
		},
	)
	if err != nil {
		return domain.Route{}, err
	}
	return updated, nil
}

func (store *RouteCooldownStore) AppendRouteEvent(
	ctx context.Context,
	event domain.RouteEvent,
) error {
	return (&RouteEventStore{db: store.db}).AppendRouteEvent(ctx, event)
}

func (store *RouteCooldownStore) FindRouteEventByID(
	ctx context.Context,
	eventID string,
) (*domain.RouteEvent, error) {
	return (&RouteEventStore{db: store.db}).FindRouteEventByID(ctx, eventID)
}

func (store *RouteCooldownStore) ListRouteEvents(
	ctx context.Context,
	filter ports.RouteEventListFilter,
) (ports.Page[domain.RouteEvent], error) {
	return (&RouteEventStore{db: store.db}).ListRouteEvents(ctx, filter)
}

func (store *RouteCooldownStore) CompareAndSwapRouteCooldownExpiryWithEvent(
	ctx context.Context,
	expected domain.Route,
	next domain.Route,
	event domain.RouteEvent,
) (domain.Route, error) {
	if err := validateRouteCooldownExpiryTransition(expected, next, event); err != nil {
		return domain.Route{}, err
	}

	persistedNext := canonicalAdminRoute(next)
	persistedEvent := canonicalRouteEvent(event)
	var updated domain.Route

	err := InTx(
		ctx,
		store.db,
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
				return ports.ErrStoreConflict
			}

			value, err := updateRouteCooldownRow(ctx, tx, persistedNext)
			if err != nil {
				return err
			}
			if !sameAdminRoute(value, persistedNext) {
				return ports.ErrStoreContractViolation
			}
			if err := appendCooldownEventTx(ctx, tx, persistedEvent); err != nil {
				return err
			}
			updated = value
			return nil
		},
	)
	if err != nil {
		return domain.Route{}, err
	}
	return updated, nil
}

func updateRouteCooldownRow(
	ctx context.Context,
	tx pgx.Tx,
	next domain.Route,
) (domain.Route, error) {
	capabilities, err := encodeAdminRouteCapabilities(next.Capabilities)
	if err != nil {
		return domain.Route{}, err
	}
	value, err := scanRoute(tx.QueryRow(
		ctx,
		updateAdminRouteSQL,
		next.ID,
		next.ResellerID,
		string(next.ProviderType),
		string(next.APIFamily),
		string(next.EndpointKind),
		next.ClientModel,
		next.ProviderModel,
		string(next.ModelRewritePolicy),
		next.Enabled,
		next.Priority,
		next.RequestsPerMinute,
		next.TokensPerMinute,
		next.ConcurrentRequests,
		next.DefaultMaxOutputTokens,
		capabilities,
		adminRouteTimeArg(next.CooldownUntil),
		nullIfEmpty(next.CooldownReason),
		nullIfEmpty(next.LastErrorCode),
		adminRouteTimeArg(next.LastErrorAt),
		next.CreatedAt,
		next.UpdatedAt,
		adminRouteTimeArg(next.DisabledAt),
	))
	if err != nil {
		return domain.Route{}, normalizeAdminWriteError(err)
	}
	return value, nil
}

func appendCooldownEventTx(
	ctx context.Context,
	tx pgx.Tx,
	event domain.RouteEvent,
) error {
	metadata, err := encodeRouteEventMetadata(event.Metadata)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		ctx,
		insertRouteEventSQL,
		event.ID,
		nullIfEmpty(event.RouteID),
		nullIfEmpty(event.ResellerID),
		nullIfEmpty(string(event.ProviderType)),
		nullIfEmpty(string(event.APIFamily)),
		nullIfEmpty(string(event.EndpointKind)),
		nullIfEmpty(event.ClientModel),
		string(event.EventType),
		nullIfEmpty(event.Reason),
		nullIfEmpty(event.LocalRequestID),
		metadata,
		event.CreatedAt,
	); err != nil {
		return NormalizeError(err)
	}
	return nil
}

func validateRouteCooldownTransition(
	expected domain.Route,
	next domain.Route,
	event domain.RouteEvent,
) error {
	if err := validateAdminRouteRecord(expected); err != nil {
		return err
	}
	if err := validateAdminRouteRecord(next); err != nil {
		return err
	}
	if err := validateRouteEventPersistence(event); err != nil {
		return err
	}
	if expected.ID != next.ID ||
		next.CooldownUntil == nil ||
		strings.TrimSpace(next.CooldownReason) == "" ||
		strings.TrimSpace(next.LastErrorCode) == "" ||
		next.LastErrorAt == nil ||
		!next.UpdatedAt.After(expected.UpdatedAt) {
		return ports.ErrStoreContractViolation
	}

	staticNext := next
	staticNext.CooldownUntil = expected.CooldownUntil
	staticNext.CooldownReason = expected.CooldownReason
	staticNext.LastErrorCode = expected.LastErrorCode
	staticNext.LastErrorAt = expected.LastErrorAt
	staticNext.UpdatedAt = expected.UpdatedAt
	if !sameAdminRoute(expected, staticNext) {
		return ports.ErrStoreContractViolation
	}

	if event.EventType != domain.RouteEventTypeCooldownSet ||
		event.RouteID != next.ID ||
		event.ResellerID != next.ResellerID ||
		event.ProviderType != next.ProviderType ||
		event.APIFamily != next.APIFamily ||
		event.EndpointKind != next.EndpointKind ||
		event.ClientModel != next.ClientModel ||
		event.Reason != next.CooldownReason ||
		strings.TrimSpace(event.LocalRequestID) == "" ||
		!event.CreatedAt.Equal(next.UpdatedAt) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validateRouteCooldownExpiryTransition(
	expected domain.Route,
	next domain.Route,
	event domain.RouteEvent,
) error {
	if err := validateAdminRouteRecord(expected); err != nil {
		return err
	}
	if err := validateAdminRouteRecord(next); err != nil {
		return err
	}
	if err := validateRouteEventPersistence(event); err != nil {
		return err
	}
	if expected.CooldownUntil == nil ||
		next.CooldownUntil != nil ||
		next.CooldownReason != "" ||
		!next.UpdatedAt.After(expected.UpdatedAt) {
		return ports.ErrStoreContractViolation
	}

	staticNext := next
	staticNext.CooldownUntil = expected.CooldownUntil
	staticNext.CooldownReason = expected.CooldownReason
	staticNext.UpdatedAt = expected.UpdatedAt
	if !sameAdminRoute(expected, staticNext) {
		return ports.ErrStoreContractViolation
	}

	if event.EventType != domain.RouteEventTypeCooldownExpired ||
		event.RouteID != next.ID ||
		event.ResellerID != next.ResellerID ||
		event.ProviderType != next.ProviderType ||
		event.APIFamily != next.APIFamily ||
		event.EndpointKind != next.EndpointKind ||
		event.ClientModel != next.ClientModel ||
		event.Reason != "cooldown_elapsed" ||
		strings.TrimSpace(event.LocalRequestID) == "" ||
		!event.CreatedAt.Equal(next.UpdatedAt) {
		return ports.ErrStoreContractViolation
	}
	return nil
}
