package ledger

import (
	"fmt"
	"regexp"
	"strings"

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
		if record.ClientAmountCents <= 0 || record.ChargedAmountCents <= 0 || record.ChargedAmountCents >= record.ClientAmountCents || record.RemainingAmountCents != record.ClientAmountCents-record.ChargedAmountCents {
			return fmt.Errorf("%w: partially charged amounts", ErrRecordCorrupt)
		}
	case domain.UsageStatusCharged:
		if record.ChargedAt == nil || record.ChargedAmountCents != record.ClientAmountCents || record.RemainingAmountCents != 0 {
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
