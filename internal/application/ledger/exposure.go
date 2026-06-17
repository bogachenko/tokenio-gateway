package ledger

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type Exposure = domain.UsageExposure
type BalanceInput = domain.BalanceInput
type BalanceResult = domain.BalanceResult

func (s *Service) LoadExposure(ctx context.Context, userID string, currency string) (Exposure, error) {
	if isBlank(userID) || currency != currencyRUB {
		return Exposure{}, fmt.Errorf("%w: exposure input", ErrInvalidLedgerInput)
	}
	snapshot, err := s.ledger.LoadExposure(ctx, userID, currency)
	if err != nil {
		return Exposure{}, usageStoreUnavailable(
			ports.RequestStagePreForwarding,
			fmt.Errorf("load exposure: %w", err),
		)
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

func CalculateExposure(
	snapshot ports.UsageExposureSnapshot,
) (Exposure, error) {
	result, err := domain.CalculateUsageExposure(
		snapshot.Currency,
		snapshot.ReservedEstimatedAmountCents,
		snapshot.BillableRemainingAmountCents,
		snapshot.PartiallyChargedRemainingAmountCents,
		snapshot.PricingFailedCount,
	)
	if err == nil {
		return result, nil
	}
	switch {
	case errors.Is(err, domain.ErrInvalidFinancialInput):
		return Exposure{}, fmt.Errorf("%w: %v", ErrInvalidLedgerInput, err)
	case errors.Is(err, domain.ErrFinancialAmountOverflow):
		return Exposure{}, ErrAmountOverflow
	default:
		return Exposure{}, err
	}
}

func EvaluateBalance(
	input BalanceInput,
) (BalanceResult, error) {
	result, err := domain.EvaluateBalance(input)
	if err == nil {
		return result, nil
	}
	switch {
	case errors.Is(err, domain.ErrInvalidFinancialInput):
		return result, fmt.Errorf("%w: %v", ErrInvalidLedgerInput, err)
	case errors.Is(err, domain.ErrFinancialAmountOverflow):
		return result, ErrAmountOverflow
	default:
		return result, err
	}
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
