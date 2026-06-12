package ledger

import (
	"context"
	"fmt"
	"math"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type Exposure struct {
	Currency string

	PendingAmountCents int64
	HasUnresolvedUsage bool
}

type BalanceInput struct {
	RemoteBalanceCents   int64
	RequiredReserveCents int64

	Exposure Exposure
}

type BalanceResult struct {
	RemoteBalanceCents    int64
	PendingAmountCents    int64
	EffectiveBalanceCents int64
	RequiredReserveCents  int64

	Allowed bool
}

func (s *Service) LoadExposure(ctx context.Context, userID string, currency string) (Exposure, error) {
	if isBlank(userID) || currency != currencyRUB {
		return Exposure{}, fmt.Errorf("%w: exposure input", ErrInvalidLedgerInput)
	}
	snapshot, err := s.ledger.LoadExposure(ctx, userID, currency)
	if err != nil {
		return Exposure{}, fmt.Errorf("%w: load exposure: %w", ErrUsageStoreUnavailable, err)
	}
	return CalculateExposure(snapshot)
}

func PendingAmountForRecord(record domain.UsageRecord) (int64, error) {
	switch record.Status {
	case domain.UsageStatusReserved:
		if record.EstimatedClientAmountCents < 0 {
			return 0, ErrRecordCorrupt
		}
		return record.EstimatedClientAmountCents, nil
	case domain.UsageStatusBillable, domain.UsageStatusPartiallyCharged:
		if record.RemainingAmountCents < 0 {
			return 0, ErrRecordCorrupt
		}
		return record.RemainingAmountCents, nil
	case domain.UsageStatusReleased, domain.UsageStatusCharged, domain.UsageStatusFailed:
		return 0, nil
	case domain.UsageStatusPricingFailed:
		return 0, ErrUnresolvedUsage
	default:
		return 0, fmt.Errorf("%w: %s", ErrRecordCorrupt, record.Status)
	}
}

func CalculateExposure(snapshot ports.UsageExposureSnapshot) (Exposure, error) {
	if snapshot.Currency != currencyRUB {
		return Exposure{}, fmt.Errorf("%w: currency", ErrInvalidLedgerInput)
	}
	if snapshot.ReservedEstimatedAmountCents < 0 || snapshot.BillableRemainingAmountCents < 0 || snapshot.PartiallyChargedRemainingAmountCents < 0 || snapshot.PricingFailedCount < 0 {
		return Exposure{}, fmt.Errorf("%w: negative exposure", ErrInvalidLedgerInput)
	}
	total, err := checkedAdd(snapshot.ReservedEstimatedAmountCents, snapshot.BillableRemainingAmountCents)
	if err != nil {
		return Exposure{}, err
	}
	total, err = checkedAdd(total, snapshot.PartiallyChargedRemainingAmountCents)
	if err != nil {
		return Exposure{}, err
	}
	return Exposure{Currency: snapshot.Currency, PendingAmountCents: total, HasUnresolvedUsage: snapshot.PricingFailedCount > 0}, nil
}

func EvaluateBalance(input BalanceInput) (BalanceResult, error) {
	result := BalanceResult{
		RemoteBalanceCents:   input.RemoteBalanceCents,
		PendingAmountCents:   input.Exposure.PendingAmountCents,
		RequiredReserveCents: input.RequiredReserveCents,
	}
	if input.RemoteBalanceCents < 0 || input.RequiredReserveCents < 0 || input.Exposure.PendingAmountCents < 0 {
		return result, fmt.Errorf("%w: negative balance input", ErrInvalidLedgerInput)
	}
	if input.Exposure.Currency != currencyRUB {
		return result, fmt.Errorf("%w: currency", ErrInvalidLedgerInput)
	}
	if input.Exposure.HasUnresolvedUsage {
		return result, ErrUnresolvedUsage
	}
	effective, err := checkedSub(input.RemoteBalanceCents, input.Exposure.PendingAmountCents)
	if err != nil {
		return result, err
	}
	result.EffectiveBalanceCents = effective
	result.Allowed = effective >= input.RequiredReserveCents
	if !result.Allowed {
		return result, ErrInsufficientFunds
	}
	return result, nil
}

func checkedAdd(left int64, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, ErrAmountOverflow
	}
	return left + right, nil
}

func checkedSub(left int64, right int64) (int64, error) {
	if right > 0 && left < math.MinInt64+right {
		return 0, ErrAmountOverflow
	}
	return left - right, nil
}
