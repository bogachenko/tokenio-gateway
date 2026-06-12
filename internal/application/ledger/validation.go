package ledger

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

const currencyRUB = "RUB"

var failureReasonPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

func validateLocalRequestID(localRequestID string) error {
	if !strings.HasPrefix(localRequestID, "llmreq_") || len(localRequestID) == len("llmreq_") {
		return fmt.Errorf("%w: invalid local request id", ErrInvalidLedgerInput)
	}
	return nil
}

func isBlank(value string) bool { return strings.TrimSpace(value) == "" }

func validateFailureReason(reason string) error {
	if !failureReasonPattern.MatchString(reason) {
		return fmt.Errorf("%w: invalid failure reason", ErrInvalidLedgerInput)
	}
	return nil
}

func validateNonNegativeAmount(name string, amount int64) error {
	if amount < 0 {
		return fmt.Errorf("%w: negative %s", ErrInvalidLedgerInput, name)
	}
	return nil
}

func validateUsage(usage domain.TokenUsage) error {
	if err := pricing.ValidateUsage(usage); err != nil {
		return fmt.Errorf("%w: usage", ErrInvalidLedgerInput)
	}
	return nil
}

func acceptedBillableCompleteness(value pricing.UsageCompleteness) bool {
	return value == pricing.UsageCompletenessDetailed || value == pricing.UsageCompletenessAggregate || value == pricing.UsageCompletenessEstimated
}

func acceptedPricingFailedCompleteness(value pricing.UsageCompleteness) bool {
	return acceptedBillableCompleteness(value) || value == pricing.UsageCompletenessMissing || value == pricing.UsageCompletenessFailed
}

func ValidateRecord(record domain.UsageRecord) error {
	if err := validateLocalRequestID(record.LocalRequestID); err != nil {
		return fmt.Errorf("%w: local request id", ErrRecordCorrupt)
	}
	if !isKnownStatus(record.Status) {
		return fmt.Errorf("%w: %s", ErrInvalidUsageStatus, record.Status)
	}
	if record.EstimatedClientAmountCents < 0 || record.EstimatedUpstreamCostCents < 0 || record.ClientAmountCents < 0 || record.ChargedAmountCents < 0 || record.RemainingAmountCents < 0 || record.ActualUpstreamCostCents < 0 {
		return fmt.Errorf("%w: negative amount", ErrRecordCorrupt)
	}
	if err := validateUsage(record.EstimatedUsage); err != nil {
		return fmt.Errorf("%w: estimated usage", ErrRecordCorrupt)
	}
	switch record.Status {
	case domain.UsageStatusReserved:
		if record.ReservedAt == nil || record.BillableAt != nil || record.ReleasedAt != nil || record.ChargedAt != nil || record.FailedAt != nil {
			return fmt.Errorf("%w: reserved timestamps", ErrRecordCorrupt)
		}
		if !isZeroUsage(record.Usage) || record.ClientAmountCents != 0 || record.ChargedAmountCents != 0 || record.RemainingAmountCents != 0 || record.ActualUpstreamCostCents != 0 {
			return fmt.Errorf("%w: reserved amounts", ErrRecordCorrupt)
		}
	case domain.UsageStatusReleased:
		if record.ReleasedAt == nil || validateFailureReason(record.FailureReason) != nil {
			return fmt.Errorf("%w: released fields", ErrRecordCorrupt)
		}
		if record.ClientAmountCents != 0 || record.ChargedAmountCents != 0 || record.RemainingAmountCents != 0 || record.ActualUpstreamCostCents != 0 {
			return fmt.Errorf("%w: released amounts", ErrRecordCorrupt)
		}
	case domain.UsageStatusFailed:
		if record.FailedAt == nil || validateFailureReason(record.FailureReason) != nil {
			return fmt.Errorf("%w: failed fields", ErrRecordCorrupt)
		}
	case domain.UsageStatusBillable:
		if record.BillableAt == nil || !acceptedBillableCompleteness(pricing.UsageCompleteness(record.UsageCompleteness)) || validateUsage(record.Usage) != nil {
			return fmt.Errorf("%w: billable fields", ErrRecordCorrupt)
		}
		if record.ChargedAmountCents != 0 || record.RemainingAmountCents != record.ClientAmountCents {
			return fmt.Errorf("%w: billable amounts", ErrRecordCorrupt)
		}
	case domain.UsageStatusPartiallyCharged:
		if record.ChargedAt == nil || strings.TrimSpace(record.BillingChargeRequestID) == "" || record.BillableAt == nil || !acceptedBillableCompleteness(pricing.UsageCompleteness(record.UsageCompleteness)) || validateUsage(record.Usage) != nil {
			return fmt.Errorf("%w: partially charged fields", ErrRecordCorrupt)
		}
		if record.ClientAmountCents <= 0 || record.ChargedAmountCents <= 0 || record.ChargedAmountCents >= record.ClientAmountCents || record.RemainingAmountCents != record.ClientAmountCents-record.ChargedAmountCents {
			return fmt.Errorf("%w: partially charged amounts", ErrRecordCorrupt)
		}
	case domain.UsageStatusCharged:
		if record.ChargedAt == nil || strings.TrimSpace(record.BillingChargeRequestID) == "" || record.BillableAt == nil || !acceptedBillableCompleteness(pricing.UsageCompleteness(record.UsageCompleteness)) || validateUsage(record.Usage) != nil || record.ChargedAmountCents != record.ClientAmountCents || record.RemainingAmountCents != 0 {
			return fmt.Errorf("%w: charged fields", ErrRecordCorrupt)
		}
	case domain.UsageStatusPricingFailed:
		if !acceptedPricingFailedCompleteness(pricing.UsageCompleteness(record.UsageCompleteness)) || validateFailureReason(record.FailureReason) != nil || record.ClientAmountCents != 0 || record.ChargedAmountCents != 0 || record.RemainingAmountCents != 0 {
			return fmt.Errorf("%w: pricing failed fields", ErrRecordCorrupt)
		}
	}
	return nil
}

func isZeroUsage(usage domain.TokenUsage) bool { return usage == (domain.TokenUsage{}) }

func ValidateChargeBatch(batch domain.BillingChargeBatch) error {
	if !validBillingChargeID(batch.ID) {
		return fmt.Errorf("%w: charge batch id", ErrRecordCorrupt)
	}
	if isBlank(batch.UserID) || isBlank(batch.BillingSubjectUserID) || isBlank(string(batch.ProviderType)) || isBlank(batch.ClientModel) || isBlank(batch.BillingModel) {
		return fmt.Errorf("%w: charge batch identity", ErrRecordCorrupt)
	}
	billingModel, err := pricing.BillingModel(batch.ProviderType, batch.ClientModel)
	if err != nil || batch.BillingModel != billingModel {
		return fmt.Errorf("%w: charge batch billing model", ErrRecordCorrupt)
	}
	if batch.Currency != currencyRUB || batch.AmountCents <= 0 || batch.InputTokens < 0 || batch.OutputTokens < 0 {
		return fmt.Errorf("%w: charge batch amounts", ErrRecordCorrupt)
	}
	if batch.BillingResponseBalanceCents != nil && *batch.BillingResponseBalanceCents < 0 {
		return fmt.Errorf("%w: charge batch response balance", ErrRecordCorrupt)
	}
	if batch.CreatedAt.IsZero() || batch.UpdatedAt.IsZero() || !isUTCTime(batch.CreatedAt) || !isUTCTime(batch.UpdatedAt) {
		return fmt.Errorf("%w: charge batch timestamps", ErrRecordCorrupt)
	}
	switch batch.Status {
	case domain.BillingChargeStatusPending:
		if batch.ChargedAt != nil || batch.FailedAt != nil || batch.BillingErrorCode != "" {
			return fmt.Errorf("%w: pending charge batch fields", ErrRecordCorrupt)
		}
	case domain.BillingChargeStatusFailed:
		if batch.FailedAt == nil || !isUTCTime(*batch.FailedAt) || batch.ChargedAt != nil || validateBillingErrorCode(batch.BillingErrorCode) != nil {
			return fmt.Errorf("%w: failed charge batch fields", ErrRecordCorrupt)
		}
	case domain.BillingChargeStatusSucceeded:
		if batch.ChargedAt == nil || !isUTCTime(*batch.ChargedAt) || batch.FailedAt != nil || batch.BillingErrorCode != "" {
			return fmt.Errorf("%w: succeeded charge batch fields", ErrRecordCorrupt)
		}
	default:
		return fmt.Errorf("%w: charge batch status", ErrRecordCorrupt)
	}
	return nil
}

func isUTCTime(value time.Time) bool { return value.Location() == time.UTC }

func ValidateAllocation(batch domain.BillingChargeBatch, allocation domain.BillingChargeAllocation) error {
	if !validBillingAllocationID(allocation.ID) || allocation.BatchID != batch.ID || isBlank(allocation.LocalRequestID) {
		return fmt.Errorf("%w: charge allocation identity", ErrRecordCorrupt)
	}
	if allocation.ChargedAmountCents <= 0 || allocation.RemainingAmountCents < 0 {
		return fmt.Errorf("%w: charge allocation amounts", ErrRecordCorrupt)
	}
	if allocation.CreatedAt.IsZero() {
		return fmt.Errorf("%w: charge allocation timestamp", ErrRecordCorrupt)
	}
	return nil
}

func validateBillingErrorCode(code string) error {
	if isBlank(code) || len(code) > 64 || !failureReasonPattern.MatchString(code) {
		return fmt.Errorf("%w: invalid billing error code", ErrRecordCorrupt)
	}
	return nil
}

func validBillingChargeID(id string) bool {
	return len(id) == len("billchg_")+64 && strings.HasPrefix(id, "billchg_") && isLowerHex(id[len("billchg_"):])
}

func validBillingAllocationID(id string) bool {
	return len(id) == len("billalloc_")+64 && strings.HasPrefix(id, "billalloc_") && isLowerHex(id[len("billalloc_"):])
}

func isLowerHex(value string) bool {
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}
