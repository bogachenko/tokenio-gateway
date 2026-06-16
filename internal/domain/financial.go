package domain

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

const CurrencyRUB = "RUB"

var (
	ErrInvalidFinancialInput    = errors.New("invalid financial input")
	ErrInvalidUsageCompleteness = errors.New("invalid usage completeness")
	ErrInvalidTokenUsage        = errors.New("invalid token usage")
	ErrInvalidUsageStatus       = errors.New("invalid usage status")
	ErrUsageRecordCorrupt       = errors.New("corrupt usage record")
	ErrUnresolvedUsage          = errors.New("unresolved usage")
	ErrInsufficientFunds        = errors.New("insufficient funds")
	ErrFinancialAmountOverflow  = errors.New("financial amount overflow")
)

type UsageCompleteness string

const (
	UsageCompletenessDetailed  UsageCompleteness = "detailed"
	UsageCompletenessAggregate UsageCompleteness = "aggregate"
	UsageCompletenessEstimated UsageCompleteness = "estimated"
	UsageCompletenessMissing   UsageCompleteness = "missing"
	UsageCompletenessFailed    UsageCompleteness = "failed"
)

type UsageExposure struct {
	Currency           string
	PendingAmountCents int64
	HasUnresolvedUsage bool
}

type BalanceInput struct {
	RemoteBalanceCents   int64
	RequiredReserveCents int64
	Exposure             UsageExposure
}

type BalanceResult struct {
	RemoteBalanceCents    int64
	PendingAmountCents    int64
	EffectiveBalanceCents int64
	RequiredReserveCents  int64
	Allowed               bool
}

var financialFailureReasonPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

func BillingModel(providerType ProviderType, clientModel string) (string, error) {
	if providerType == "" {
		return "", fmt.Errorf("%w: provider type is empty", ErrInvalidFinancialInput)
	}
	if strings.TrimSpace(clientModel) == "" {
		return "", fmt.Errorf("%w: client model is blank", ErrInvalidFinancialInput)
	}
	return string(providerType) + ":" + clientModel, nil
}

func ParseUsageCompleteness(value string) (UsageCompleteness, error) {
	switch UsageCompleteness(value) {
	case UsageCompletenessDetailed, UsageCompletenessAggregate, UsageCompletenessEstimated, UsageCompletenessMissing, UsageCompletenessFailed:
		return UsageCompleteness(value), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidUsageCompleteness, value)
	}
}

func ValidateTokenUsage(usage TokenUsage) error {
	values := []int64{
		usage.InputTokens,
		usage.CachedInputTokens,
		usage.OutputTokens,
		usage.ReasoningTokens,
		usage.ImageInputTokens,
		usage.AudioInputTokens,
		usage.AudioOutputTokens,
		usage.FileInputTokens,
		usage.VideoInputTokens,
		usage.ImageGenerationUnits,
	}
	for _, value := range values {
		if value < 0 {
			return ErrInvalidTokenUsage
		}
	}
	return nil
}

func ValidateUsageRecord(record UsageRecord) error {
	if !strings.HasPrefix(record.LocalRequestID, "llmreq_") || len(record.LocalRequestID) == len("llmreq_") {
		return fmt.Errorf("%w: local request id", ErrUsageRecordCorrupt)
	}
	if !knownUsageStatus(record.Status) {
		return fmt.Errorf("%w: %s", ErrInvalidUsageStatus, record.Status)
	}
	if record.EstimatedClientAmountCents < 0 || record.EstimatedUpstreamCostCents < 0 || record.ClientAmountCents < 0 || record.ChargedAmountCents < 0 || record.RemainingAmountCents < 0 || record.ActualUpstreamCostCents < 0 {
		return fmt.Errorf("%w: negative amount", ErrUsageRecordCorrupt)
	}
	if err := ValidateTokenUsage(record.EstimatedUsage); err != nil {
		return fmt.Errorf("%w: estimated usage", ErrUsageRecordCorrupt)
	}
	completeness := UsageCompleteness(record.UsageCompleteness)
	switch record.Status {
	case UsageStatusReserved:
		if record.ReservedAt == nil || record.BillableAt != nil || record.ReleasedAt != nil || record.ChargedAt != nil || record.FailedAt != nil {
			return fmt.Errorf("%w: reserved timestamps", ErrUsageRecordCorrupt)
		}
		if record.Usage != (TokenUsage{}) || record.ClientAmountCents != 0 || record.ChargedAmountCents != 0 || record.RemainingAmountCents != 0 || record.ActualUpstreamCostCents != 0 {
			return fmt.Errorf("%w: reserved amounts", ErrUsageRecordCorrupt)
		}
	case UsageStatusReleased:
		if record.ReleasedAt == nil || !validFailureReason(record.FailureReason) {
			return fmt.Errorf("%w: released fields", ErrUsageRecordCorrupt)
		}
		if record.ClientAmountCents != 0 || record.ChargedAmountCents != 0 || record.RemainingAmountCents != 0 || record.ActualUpstreamCostCents != 0 {
			return fmt.Errorf("%w: released amounts", ErrUsageRecordCorrupt)
		}
	case UsageStatusFailed:
		if record.FailedAt == nil || !validFailureReason(record.FailureReason) {
			return fmt.Errorf("%w: failed fields", ErrUsageRecordCorrupt)
		}
	case UsageStatusBillable:
		if record.BillableAt == nil || !acceptedBillable(completeness) || ValidateTokenUsage(record.Usage) != nil {
			return fmt.Errorf("%w: billable fields", ErrUsageRecordCorrupt)
		}
		if record.ChargedAmountCents != 0 || record.RemainingAmountCents != record.ClientAmountCents {
			return fmt.Errorf("%w: billable amounts", ErrUsageRecordCorrupt)
		}
	case UsageStatusPartiallyCharged:
		if record.ChargedAt == nil || strings.TrimSpace(record.BillingChargeRequestID) == "" || record.BillableAt == nil || !acceptedBillable(completeness) || ValidateTokenUsage(record.Usage) != nil {
			return fmt.Errorf("%w: partially charged fields", ErrUsageRecordCorrupt)
		}
		if record.ClientAmountCents <= 0 || record.ChargedAmountCents <= 0 || record.ChargedAmountCents >= record.ClientAmountCents || record.RemainingAmountCents != record.ClientAmountCents-record.ChargedAmountCents {
			return fmt.Errorf("%w: partially charged amounts", ErrUsageRecordCorrupt)
		}
	case UsageStatusCharged:
		if record.ChargedAt == nil || strings.TrimSpace(record.BillingChargeRequestID) == "" || record.BillableAt == nil || !acceptedBillable(completeness) || ValidateTokenUsage(record.Usage) != nil || record.ChargedAmountCents != record.ClientAmountCents || record.RemainingAmountCents != 0 {
			return fmt.Errorf("%w: charged fields", ErrUsageRecordCorrupt)
		}
	case UsageStatusPricingFailed:
		if !acceptedPricingFailed(completeness) || !validFailureReason(record.FailureReason) || record.ClientAmountCents != 0 || record.ChargedAmountCents != 0 || record.RemainingAmountCents != 0 {
			return fmt.Errorf("%w: pricing failed fields", ErrUsageRecordCorrupt)
		}
	}
	expected, err := BillingModel(record.ProviderType, record.ClientModel)
	if err != nil || record.BillingModel != expected {
		return fmt.Errorf("%w: billing model", ErrUsageRecordCorrupt)
	}
	return nil
}

func ValidateBillingChargeBatch(batch BillingChargeBatch) error {
	if !validChargeID(batch.ID) {
		return fmt.Errorf("%w: charge batch id", ErrUsageRecordCorrupt)
	}
	if blank(batch.UserID) || blank(batch.BillingSubjectUserID) || blank(string(batch.ProviderType)) || blank(batch.ClientModel) || blank(batch.BillingModel) {
		return fmt.Errorf("%w: charge batch identity", ErrUsageRecordCorrupt)
	}
	expected, err := BillingModel(batch.ProviderType, batch.ClientModel)
	if err != nil || batch.BillingModel != expected {
		return fmt.Errorf("%w: charge batch billing model", ErrUsageRecordCorrupt)
	}
	if batch.Currency != CurrencyRUB || batch.AmountCents <= 0 || batch.InputTokens < 0 || batch.OutputTokens < 0 {
		return fmt.Errorf("%w: charge batch amounts", ErrUsageRecordCorrupt)
	}
	if batch.BillingResponseBalanceCents != nil && *batch.BillingResponseBalanceCents < 0 {
		return fmt.Errorf("%w: charge batch response balance", ErrUsageRecordCorrupt)
	}
	if batch.CreatedAt.IsZero() || batch.UpdatedAt.IsZero() || batch.CreatedAt.Location() != time.UTC || batch.UpdatedAt.Location() != time.UTC {
		return fmt.Errorf("%w: charge batch timestamps", ErrUsageRecordCorrupt)
	}
	switch batch.Status {
	case BillingChargeStatusPending:
		if batch.ChargedAt != nil || batch.FailedAt != nil || batch.BillingErrorCode != "" {
			return fmt.Errorf("%w: pending charge batch fields", ErrUsageRecordCorrupt)
		}
	case BillingChargeStatusFailed:
		if batch.FailedAt == nil || batch.FailedAt.Location() != time.UTC || batch.ChargedAt != nil || !validBillingErrorCode(batch.BillingErrorCode) {
			return fmt.Errorf("%w: failed charge batch fields", ErrUsageRecordCorrupt)
		}
	case BillingChargeStatusSucceeded:
		if batch.ChargedAt == nil || batch.ChargedAt.Location() != time.UTC || batch.FailedAt != nil || batch.BillingErrorCode != "" {
			return fmt.Errorf("%w: succeeded charge batch fields", ErrUsageRecordCorrupt)
		}
	default:
		return fmt.Errorf("%w: charge batch status", ErrUsageRecordCorrupt)
	}
	return nil
}

func ValidateBillingChargeAllocation(batch BillingChargeBatch, allocation BillingChargeAllocation) error {
	if !validAllocationID(allocation.ID) || allocation.BatchID != batch.ID || blank(allocation.LocalRequestID) {
		return fmt.Errorf("%w: charge allocation identity", ErrUsageRecordCorrupt)
	}
	if allocation.ChargedAmountCents <= 0 || allocation.RemainingAmountCents < 0 {
		return fmt.Errorf("%w: charge allocation amounts", ErrUsageRecordCorrupt)
	}
	if allocation.CreatedAt.IsZero() {
		return fmt.Errorf("%w: charge allocation timestamp", ErrUsageRecordCorrupt)
	}
	return nil
}

func CalculateUsageExposure(currency string, reserved int64, billable int64, partial int64, pricingFailed int64) (UsageExposure, error) {
	if currency != CurrencyRUB {
		return UsageExposure{}, fmt.Errorf("%w: currency", ErrInvalidFinancialInput)
	}
	if reserved < 0 || billable < 0 || partial < 0 || pricingFailed < 0 {
		return UsageExposure{}, fmt.Errorf("%w: negative exposure", ErrInvalidFinancialInput)
	}
	total, err := checkedAdd(reserved, billable)
	if err != nil {
		return UsageExposure{}, err
	}
	total, err = checkedAdd(total, partial)
	if err != nil {
		return UsageExposure{}, err
	}
	return UsageExposure{Currency: currency, PendingAmountCents: total, HasUnresolvedUsage: pricingFailed > 0}, nil
}

func EvaluateBalance(input BalanceInput) (BalanceResult, error) {
	result := BalanceResult{
		RemoteBalanceCents:   input.RemoteBalanceCents,
		PendingAmountCents:   input.Exposure.PendingAmountCents,
		RequiredReserveCents: input.RequiredReserveCents,
	}
	if input.RemoteBalanceCents < 0 || input.RequiredReserveCents < 0 || input.Exposure.PendingAmountCents < 0 {
		return result, fmt.Errorf("%w: negative balance input", ErrInvalidFinancialInput)
	}
	if input.Exposure.Currency != CurrencyRUB {
		return result, fmt.Errorf("%w: currency", ErrInvalidFinancialInput)
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

func knownUsageStatus(status UsageStatus) bool {
	switch status {
	case UsageStatusReserved, UsageStatusReleased, UsageStatusBillable, UsageStatusPartiallyCharged, UsageStatusCharged, UsageStatusFailed, UsageStatusPricingFailed:
		return true
	default:
		return false
	}
}

func acceptedBillable(value UsageCompleteness) bool {
	return value == UsageCompletenessDetailed || value == UsageCompletenessAggregate || value == UsageCompletenessEstimated
}

func acceptedPricingFailed(value UsageCompleteness) bool {
	return acceptedBillable(value) || value == UsageCompletenessMissing || value == UsageCompletenessFailed
}

func validFailureReason(reason string) bool {
	return financialFailureReasonPattern.MatchString(reason)
}

func validBillingErrorCode(code string) bool {
	return !blank(code) && len(code) <= 64 && financialFailureReasonPattern.MatchString(code)
}

func validChargeID(id string) bool {
	return len(id) == len("billchg_")+64 && strings.HasPrefix(id, "billchg_") && lowerHex(id[len("billchg_"):])
}

func validAllocationID(id string) bool {
	return len(id) == len("billalloc_")+64 && strings.HasPrefix(id, "billalloc_") && lowerHex(id[len("billalloc_"):])
}

func lowerHex(value string) bool {
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func blank(value string) bool {
	return strings.TrimSpace(value) == ""
}

func checkedAdd(left int64, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, ErrFinancialAmountOverflow
	}
	return left + right, nil
}

func checkedSub(left int64, right int64) (int64, error) {
	if right > 0 && left < math.MinInt64+right {
		return 0, ErrFinancialAmountOverflow
	}
	return left - right, nil
}
