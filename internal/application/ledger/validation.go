package ledger

import (
	"fmt"
	"regexp"
	"strings"
	"time"

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
	if err := domain.ValidateTokenUsage(usage); err != nil {
		return fmt.Errorf("%w: usage", ErrInvalidLedgerInput)
	}
	return nil
}

func acceptedBillableCompleteness(value domain.UsageCompleteness) bool {
	return value == domain.UsageCompletenessDetailed || value == domain.UsageCompletenessAggregate || value == domain.UsageCompletenessEstimated
}

func acceptedPricingFailedCompleteness(value domain.UsageCompleteness) bool {
	return acceptedBillableCompleteness(value) || value == domain.UsageCompletenessMissing || value == domain.UsageCompletenessFailed
}

func ValidateRecord(record domain.UsageRecord) error {
	return domain.ValidateUsageRecord(record)
}

func isZeroUsage(usage domain.TokenUsage) bool { return usage == (domain.TokenUsage{}) }

func ValidateChargeBatch(batch domain.BillingChargeBatch) error {
	return domain.ValidateBillingChargeBatch(batch)
}

func isUTCTime(value time.Time) bool { return value.Location() == time.UTC }

func ValidateAllocation(
	batch domain.BillingChargeBatch,
	allocation domain.BillingChargeAllocation,
) error {
	return domain.ValidateBillingChargeAllocation(batch, allocation)
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
