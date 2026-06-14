package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const updateResellerBalanceReleaseSQL = `
UPDATE tokenio_resellers
SET
    reserved_cents = $2,
    updated_at = GREATEST(updated_at, $3)
WHERE id = $1
RETURNING
` + resellerBalanceColumns

var _ ports.UsageResellerReleaseStore = (*UsageLedger)(nil)

func (r *UsageLedger) ReleaseReservedUsageAndResellerReserve(
	ctx context.Context,
	localRequestID string,
	failureReason string,
	releasedAt time.Time,
) (ports.UsageResellerReleaseResult, error) {
	if err := validateUsageResellerReleaseInput(
		localRequestID,
		failureReason,
		releasedAt,
	); err != nil {
		return ports.UsageResellerReleaseResult{}, err
	}

	canonicalReleasedAt := releasedAt.UTC().Truncate(time.Microsecond)
	var result ports.UsageResellerReleaseResult

	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{},
		func(tx pgx.Tx) error {
			current, err := scanUsageRecord(tx.QueryRow(
				ctx,
				findUsageByLocalRequestIDForUpdateSQL,
				localRequestID,
			))
			if err != nil {
				return err
			}

			if current.Status != domain.UsageStatusReserved {
				result = ports.UsageResellerReleaseResult{
					Applied: false,
					Usage:   current,
				}
				return nil
			}
			if strings.TrimSpace(current.SelectedResellerID) == "" ||
				current.EstimatedUpstreamCostCents < 0 ||
				canonicalReleasedAt.Before(current.UpdatedAt) {
				return ports.ErrStoreContractViolation
			}

			reseller, err := scanReseller(tx.QueryRow(
				ctx,
				findResellerBalanceForUpdateSQL,
				current.SelectedResellerID,
			))
			if err != nil {
				return err
			}
			if reseller.ReservedCents <
				current.EstimatedUpstreamCostCents {
				return ports.ErrStoreContractViolation
			}

			if current.EstimatedUpstreamCostCents > 0 {
				expectedReseller := reseller
				expectedReseller.ReservedCents -=
					current.EstimatedUpstreamCostCents
				if canonicalReleasedAt.After(
					expectedReseller.UpdatedAt,
				) {
					expectedReseller.UpdatedAt = canonicalReleasedAt
				}

				persistedReseller, err := scanReseller(tx.QueryRow(
					ctx,
					updateResellerBalanceReleaseSQL,
					current.SelectedResellerID,
					expectedReseller.ReservedCents,
					canonicalReleasedAt,
				))
				if err != nil {
					return err
				}
				if !sameResellerBalanceSnapshot(
					persistedReseller,
					expectedReseller,
				) {
					return ports.ErrStoreContractViolation
				}
			}

			desired := current
			desired.Status = domain.UsageStatusReleased
			desired.FailureReason = failureReason
			desired.ReleasedAt = &canonicalReleasedAt
			desired.UpdatedAt = canonicalReleasedAt

			args := usageRecordNamedArgs(desired)
			args["lookup_local_request_id"] = localRequestID
			args["expected_status"] = string(
				domain.UsageStatusReserved,
			)

			tag, err := tx.Exec(ctx, updateUsageRecordCASQL, args)
			if err != nil {
				return NormalizeError(err)
			}
			if tag.RowsAffected() != 1 {
				return ports.ErrStoreConflict
			}

			result = ports.UsageResellerReleaseResult{
				Applied: true,
				Usage:   desired,
			}
			return nil
		},
	)
	if err != nil {
		return ports.UsageResellerReleaseResult{}, err
	}
	return result, nil
}

func validateUsageResellerReleaseInput(
	localRequestID string,
	failureReason string,
	releasedAt time.Time,
) error {
	if strings.TrimSpace(localRequestID) == "" ||
		strings.TrimSpace(failureReason) == "" ||
		releasedAt.IsZero() ||
		releasedAt.Location() != time.UTC {
		return ports.ErrStoreContractViolation
	}
	return nil
}
