package postgres

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	reservation "github.com/bogachenko/tokenio-gateway/internal/ports/llmrequestreservation"
	"github.com/jackc/pgx/v5"
)

const findUsageForRouteTransferSQL = `
SELECT
` + usageRecordColumns + `
FROM tokenio_usage_records
WHERE local_request_id = $1
FOR UPDATE
`

const findResellersForRouteTransferSQL = `
SELECT
    id,
    name,
    provider_type,
    base_url,
    api_key_env,
    enabled,
    balance_cents,
    reserved_cents,
    minimum_balance_cents,
    created_at,
    updated_at,
    disabled_at
FROM tokenio_resellers
WHERE id = ANY($1::text[])
ORDER BY id ASC
FOR UPDATE
`

const updateResellerReserveForRouteTransferSQL = `
UPDATE tokenio_resellers
SET
    reserved_cents = $2,
    updated_at = $3
WHERE id = $1
RETURNING
    id,
    name,
    provider_type,
    base_url,
    api_key_env,
    enabled,
    balance_cents,
    reserved_cents,
    minimum_balance_cents,
    created_at,
    updated_at,
    disabled_at
`

type LLMRequestRouteReservationTransfer struct {
	db    *DB
	clock ports.Clock
}

var _ reservation.RouteReservationTransfer = (*LLMRequestRouteReservationTransfer)(nil)

func NewLLMRequestRouteReservationTransfer(
	db *DB,
	clock ports.Clock,
) (*LLMRequestRouteReservationTransfer, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	if clock == nil {
		return nil, fmt.Errorf(
			"%w: nil route reservation transfer clock",
			reservation.ErrDependencyRequired,
		)
	}
	return &LLMRequestRouteReservationTransfer{db: db, clock: clock}, nil
}

func (s *LLMRequestRouteReservationTransfer) Transfer(
	ctx context.Context,
	input reservation.RouteReservationTransferInput,
) (reservation.RouteReservationTransferResult, error) {
	if s == nil || s.db == nil || s.db.pool == nil || s.clock == nil {
		return reservation.RouteReservationTransferResult{}, ErrInvalidDatabaseConfig
	}
	if ctx == nil {
		return reservation.RouteReservationTransferResult{},
			ports.ErrStoreContractViolation
	}
	if err := validateRouteReservationTransferInput(input); err != nil {
		return reservation.RouteReservationTransferResult{}, err
	}

	now := s.clock.Now()
	if now.IsZero() || now.Location() != time.UTC {
		return reservation.RouteReservationTransferResult{},
			ports.ErrStoreContractViolation
	}
	now = now.Truncate(time.Microsecond)

	var result reservation.RouteReservationTransferResult
	err := InTx(ctx, s.db, pgx.TxOptions{}, func(tx pgx.Tx) error {
		current, err := scanUsageRecord(
			tx.QueryRow(
				ctx,
				findUsageForRouteTransferSQL,
				input.ExpectedUsage.LocalRequestID,
			),
		)
		if err != nil {
			return err
		}

		targetUsage := routeTransferTargetUsage(
			input.ExpectedUsage,
			input.Target,
			now,
		)
		if routeTransferAlreadyApplied(current, targetUsage) {
			resellers, err := lockRouteTransferResellers(
				ctx,
				tx,
				input.ExpectedUsage.SelectedResellerID,
				input.Target.Reseller.ID,
			)
			if err != nil {
				return err
			}
			result = reservation.RouteReservationTransferResult{
				Usage:            current,
				ReleasedReseller: resellers[input.ExpectedUsage.SelectedResellerID],
				ReservedReseller: resellers[input.Target.Reseller.ID],
			}
			return nil
		}
		if !reflect.DeepEqual(current, input.ExpectedUsage) {
			return ports.ErrStoreConflict
		}

		resellers, err := lockRouteTransferResellers(
			ctx,
			tx,
			current.SelectedResellerID,
			input.Target.Reseller.ID,
		)
		if err != nil {
			return err
		}
		previous := resellers[current.SelectedResellerID]
		target := resellers[input.Target.Reseller.ID]
		if err := validateRouteTransferResellers(
			previous,
			target,
			current,
			input.Target,
		); err != nil {
			return err
		}

		released, reserved, err := applyRouteTransferBalances(
			ctx,
			tx,
			previous,
			target,
			current.EstimatedUpstreamCostCents,
			input.Target.EstimatedUpstreamCostCents,
			now,
		)
		if err != nil {
			return err
		}

		args := usageRecordNamedArgs(targetUsage)
		args["lookup_local_request_id"] = current.LocalRequestID
		args["expected_status"] = string(domain.UsageStatusReserved)
		tag, err := tx.Exec(ctx, updateUsageRecordCASQL, args)
		if err != nil {
			return NormalizeError(err)
		}
		if tag.RowsAffected() != 1 {
			return ports.ErrStoreConflict
		}

		persisted, err := scanUsageRecord(
			tx.QueryRow(
				ctx,
				findUsageForRouteTransferSQL,
				current.LocalRequestID,
			),
		)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(persisted, targetUsage) {
			return ports.ErrStoreContractViolation
		}

		result = reservation.RouteReservationTransferResult{
			Usage:            persisted,
			ReleasedReseller: released,
			ReservedReseller: reserved,
		}
		return nil
	})
	if err != nil {
		return reservation.RouteReservationTransferResult{}, err
	}
	return result, nil
}

func validateRouteReservationTransferInput(
	input reservation.RouteReservationTransferInput,
) error {
	expected := input.ExpectedUsage
	target := input.Target
	if strings.TrimSpace(expected.LocalRequestID) == "" ||
		expected.Status != domain.UsageStatusReserved ||
		strings.TrimSpace(expected.SelectedRouteID) == "" ||
		strings.TrimSpace(expected.SelectedResellerID) == "" ||
		expected.EstimatedUpstreamCostCents < 0 ||
		expected.Currency != "RUB" ||
		strings.TrimSpace(target.Route.ID) == "" ||
		strings.TrimSpace(target.Reseller.ID) == "" ||
		target.Route.ResellerID != target.Reseller.ID ||
		target.Route.ProviderType == "" ||
		target.Route.ProviderType != target.Reseller.ProviderType ||
		target.Route.APIFamily != expected.APIFamily ||
		target.Route.EndpointKind != expected.EndpointKind ||
		target.Route.ClientModel != expected.ClientModel ||
		!target.Route.Enabled ||
		!target.Reseller.Enabled ||
		strings.TrimSpace(target.BillingModel) == "" ||
		target.EstimatedClientAmountCents < 0 ||
		target.EstimatedUpstreamCostCents < 0 ||
		target.Currency != expected.Currency ||
		!nonNegativeLLMRequestAtomicReservationUsage(target.EstimatedUsage) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func routeTransferTargetUsage(
	expected domain.UsageRecord,
	target reservation.RouteFallbackPlan,
	now time.Time,
) domain.UsageRecord {
	result := expected
	result.BillingModel = target.BillingModel
	result.SelectedRouteID = target.Route.ID
	result.SelectedResellerID = target.Reseller.ID
	result.ProviderType = target.Route.ProviderType
	result.ProviderModel = target.Route.ProviderModel
	result.EstimatedUsage = target.EstimatedUsage
	result.EstimatedClientAmountCents = target.EstimatedClientAmountCents
	result.EstimatedUpstreamCostCents = target.EstimatedUpstreamCostCents
	result.Currency = target.Currency
	result.UpdatedAt = now
	return result
}

func routeTransferAlreadyApplied(
	current domain.UsageRecord,
	target domain.UsageRecord,
) bool {
	if current.Status != domain.UsageStatusReserved {
		return false
	}
	target.UpdatedAt = current.UpdatedAt
	return reflect.DeepEqual(current, target)
}

func lockRouteTransferResellers(
	ctx context.Context,
	tx pgx.Tx,
	previousID string,
	targetID string,
) (map[string]domain.Reseller, error) {
	ids := []string{previousID}
	if targetID != previousID {
		ids = append(ids, targetID)
	}
	sort.Strings(ids)

	rows, err := tx.Query(ctx, findResellersForRouteTransferSQL, ids)
	if err != nil {
		return nil, NormalizeError(err)
	}
	defer rows.Close()

	result := make(map[string]domain.Reseller, len(ids))
	for rows.Next() {
		value, err := scanReseller(rows)
		if err != nil {
			return nil, err
		}
		if _, exists := result[value.ID]; exists {
			return nil, ports.ErrStoreContractViolation
		}
		result[value.ID] = value
	}
	if err := rows.Err(); err != nil {
		return nil, NormalizeError(err)
	}
	if len(result) != len(ids) {
		return nil, ports.ErrStoreConflict
	}
	return result, nil
}

func validateRouteTransferResellers(
	previous domain.Reseller,
	target domain.Reseller,
	current domain.UsageRecord,
	plan reservation.RouteFallbackPlan,
) error {
	if previous.ID != current.SelectedResellerID ||
		target.ID != plan.Reseller.ID ||
		target.ID != plan.Route.ResellerID ||
		target.ProviderType != plan.Route.ProviderType ||
		previous.ReservedCents < current.EstimatedUpstreamCostCents {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func applyRouteTransferBalances(
	ctx context.Context,
	tx pgx.Tx,
	previous domain.Reseller,
	target domain.Reseller,
	releaseCents int64,
	reserveCents int64,
	now time.Time,
) (domain.Reseller, domain.Reseller, error) {
	if previous.ID == target.ID {
		nextReserved := previous.ReservedCents - releaseCents
		candidate := previous
		candidate.ReservedCents = nextReserved
		if !domain.CanReserveResellerBalance(candidate, reserveCents) {
			return domain.Reseller{}, domain.Reseller{},
				reservation.ErrResellerReserveUnavailable
		}
		nextReserved += reserveCents
		updated, err := updateRouteTransferReseller(
			ctx,
			tx,
			previous.ID,
			nextReserved,
			now,
		)
		return updated, updated, err
	}

	previousReserved := previous.ReservedCents - releaseCents
	if previousReserved < 0 {
		return domain.Reseller{}, domain.Reseller{},
			ports.ErrStoreContractViolation
	}
	if !domain.CanReserveResellerBalance(target, reserveCents) {
		return domain.Reseller{}, domain.Reseller{},
			reservation.ErrResellerReserveUnavailable
	}
	targetReserved := target.ReservedCents + reserveCents

	released, err := updateRouteTransferReseller(
		ctx,
		tx,
		previous.ID,
		previousReserved,
		now,
	)
	if err != nil {
		return domain.Reseller{}, domain.Reseller{}, err
	}
	reserved, err := updateRouteTransferReseller(
		ctx,
		tx,
		target.ID,
		targetReserved,
		now,
	)
	if err != nil {
		return domain.Reseller{}, domain.Reseller{}, err
	}
	return released, reserved, nil
}

func updateRouteTransferReseller(
	ctx context.Context,
	tx pgx.Tx,
	resellerID string,
	reservedCents int64,
	now time.Time,
) (domain.Reseller, error) {
	if reservedCents < 0 {
		return domain.Reseller{}, ports.ErrStoreContractViolation
	}
	value, err := scanReseller(
		tx.QueryRow(
			ctx,
			updateResellerReserveForRouteTransferSQL,
			resellerID,
			reservedCents,
			now,
		),
	)
	if err != nil {
		return domain.Reseller{}, err
	}
	if value.ID != resellerID ||
		value.ReservedCents != reservedCents {
		return domain.Reseller{}, ports.ErrStoreContractViolation
	}
	return value, nil
}
