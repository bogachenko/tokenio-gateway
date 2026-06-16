package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type ReserveInput struct {
	LocalRequestID string
	IdempotencyKey *string

	UserID   string
	APIKeyID string

	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind

	ClientModel  string
	BillingModel string

	SelectedRouteID    string
	SelectedResellerID string

	ProviderType  domain.ProviderType
	ProviderModel string

	EstimatedUsage domain.TokenUsage

	EstimatedClientAmountCents int64
	EstimatedUpstreamCostCents int64

	Currency string
}

type ReserveDisposition string

const (
	ReserveDispositionCreated         ReserveDisposition = "created"
	ReserveDispositionAlreadyReserved ReserveDisposition = "already_reserved"
)

type ReserveResult struct {
	Disposition ReserveDisposition
	Record      domain.UsageRecord
}

func (s *Service) Reserve(ctx context.Context, input ReserveInput) (ReserveResult, error) {
	if err := validateReserveInput(input); err != nil {
		return ReserveResult{}, err
	}
	now, err := s.operationTime()
	if err != nil {
		return ReserveResult{}, err
	}
	idempotencyKey := ""
	if input.IdempotencyKey != nil {
		idempotencyKey = *input.IdempotencyKey
	}
	record := domain.UsageRecord{
		LocalRequestID:             input.LocalRequestID,
		IdempotencyKey:             idempotencyKey,
		UserID:                     input.UserID,
		APIKeyID:                   input.APIKeyID,
		APIFamily:                  input.APIFamily,
		EndpointKind:               input.EndpointKind,
		ClientModel:                input.ClientModel,
		BillingModel:               input.BillingModel,
		SelectedRouteID:            input.SelectedRouteID,
		SelectedResellerID:         input.SelectedResellerID,
		ProviderType:               input.ProviderType,
		ProviderModel:              input.ProviderModel,
		EstimatedUsage:             input.EstimatedUsage,
		EstimatedClientAmountCents: input.EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: input.EstimatedUpstreamCostCents,
		Currency:                   input.Currency,
		UsageCompleteness:          string(domain.UsageCompletenessMissing),
		Status:                     domain.UsageStatusReserved,
		CreatedAt:                  now,
		ReservedAt:                 timePtr(now),
		UpdatedAt:                  now,
	}
	if err := ValidateRecord(record); err != nil {
		return ReserveResult{}, err
	}
	result, err := s.ledger.CreateReserved(ctx, record)
	if err != nil {
		return ReserveResult{}, fmt.Errorf("%w: create reserved: %w", ErrUsageStoreUnavailable, err)
	}
	return classifyReserveResult(record, result)
}

func validateReserveInput(input ReserveInput) error {
	if err := validateLocalRequestID(input.LocalRequestID); err != nil {
		return err
	}
	if input.IdempotencyKey != nil && isBlank(*input.IdempotencyKey) {
		return fmt.Errorf("%w: invalid idempotency key", ErrInvalidLedgerInput)
	}
	if isBlank(input.UserID) || isBlank(input.APIKeyID) || input.APIFamily == "" || input.EndpointKind == "" || isBlank(input.ClientModel) || isBlank(input.BillingModel) || isBlank(input.SelectedRouteID) || isBlank(input.SelectedResellerID) || input.ProviderType == "" || isBlank(input.ProviderModel) {
		return fmt.Errorf("%w: missing reservation field", ErrInvalidLedgerInput)
	}
	billingModel, err := domain.BillingModel(input.ProviderType, input.ClientModel)
	if err != nil || input.BillingModel != billingModel {
		return fmt.Errorf("%w: billing model", ErrInvalidLedgerInput)
	}
	if err := validateUsage(input.EstimatedUsage); err != nil {
		return err
	}
	if err := validateNonNegativeAmount("estimated client amount", input.EstimatedClientAmountCents); err != nil {
		return err
	}
	if err := validateNonNegativeAmount("estimated upstream cost", input.EstimatedUpstreamCostCents); err != nil {
		return err
	}
	if input.Currency != currencyRUB {
		return fmt.Errorf("%w: currency", ErrInvalidLedgerInput)
	}
	return nil
}

func classifyReserveResult(requested domain.UsageRecord, result ports.UsageReserveResult) (ReserveResult, error) {
	switch result.Outcome {
	case ports.UsageReserveOutcomeCreated:
		if result.Existing != nil {
			return ReserveResult{}, fmt.Errorf("%w: created with existing", ErrUsageStoreContractViolation)
		}
		return ReserveResult{Disposition: ReserveDispositionCreated, Record: requested}, nil
	case ports.UsageReserveOutcomeLocalRequestExists:
		if result.Existing == nil || result.Existing.LocalRequestID != requested.LocalRequestID {
			return ReserveResult{}, fmt.Errorf("%w: local request outcome", ErrUsageStoreContractViolation)
		}
		existing := copyRecord(*result.Existing)
		if err := ValidateRecord(existing); err != nil {
			return ReserveResult{}, err
		}
		if existing.Status != domain.UsageStatusReserved || !SameReservation(existing, requested) {
			return ReserveResult{}, fmt.Errorf("%w: local request", ErrLocalRequestConflict)
		}
		return ReserveResult{Disposition: ReserveDispositionAlreadyReserved, Record: existing}, nil
	case ports.UsageReserveOutcomeIdempotencyExists:
		if result.Existing == nil || result.Existing.UserID != requested.UserID || result.Existing.EndpointKind != requested.EndpointKind || result.Existing.IdempotencyKey != requested.IdempotencyKey {
			return ReserveResult{}, fmt.Errorf("%w: idempotency outcome", ErrUsageStoreContractViolation)
		}
		if !isKnownStatus(result.Existing.Status) {
			return ReserveResult{}, fmt.Errorf("%w: idempotency status %s", ErrRecordCorrupt, result.Existing.Status)
		}
		return ReserveResult{}, classifyExistingIdempotencyStatus(result.Existing.Status)
	case ports.UsageReserveOutcomeUnresolvedUsage:
		return ReserveResult{}, ErrUnresolvedUsage
	default:
		return ReserveResult{}, fmt.Errorf("%w: unknown reserve outcome", ErrUsageStoreContractViolation)
	}
}

func classifyExistingIdempotencyStatus(status domain.UsageStatus) error {
	switch status {
	case domain.UsageStatusReserved:
		return ErrRequestInProgress
	case domain.UsageStatusBillable, domain.UsageStatusPartiallyCharged, domain.UsageStatusCharged:
		return ErrIdempotencyReplayNotAvailable
	case domain.UsageStatusReleased, domain.UsageStatusFailed:
		return ErrIdempotencyKeyReused
	case domain.UsageStatusPricingFailed:
		return ErrUnresolvedUsage
	default:
		if !isKnownStatus(status) {
			return fmt.Errorf("%w: idempotency status %s", ErrRecordCorrupt, status)
		}
		return ErrRecordCorrupt
	}
}

func SameReservation(left domain.UsageRecord, right domain.UsageRecord) bool {
	return left.LocalRequestID == right.LocalRequestID &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.UserID == right.UserID &&
		left.APIKeyID == right.APIKeyID &&
		left.APIFamily == right.APIFamily &&
		left.EndpointKind == right.EndpointKind &&
		left.ClientModel == right.ClientModel &&
		left.BillingModel == right.BillingModel &&
		left.SelectedRouteID == right.SelectedRouteID &&
		left.SelectedResellerID == right.SelectedResellerID &&
		left.ProviderType == right.ProviderType &&
		left.ProviderModel == right.ProviderModel &&
		left.EstimatedUsage == right.EstimatedUsage &&
		left.EstimatedClientAmountCents == right.EstimatedClientAmountCents &&
		left.EstimatedUpstreamCostCents == right.EstimatedUpstreamCostCents &&
		left.Currency == right.Currency
}

func findRecord(ctx context.Context, usageLedger ports.UsageLedger, localRequestID string) (domain.UsageRecord, error) {
	record, err := usageLedger.FindByLocalRequestID(ctx, localRequestID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return domain.UsageRecord{}, ErrUsageNotFound
		}
		return domain.UsageRecord{}, fmt.Errorf("%w: find usage: %w", ErrUsageStoreUnavailable, err)
	}
	if record == nil {
		return domain.UsageRecord{}, fmt.Errorf("%w: nil find result", ErrUsageStoreContractViolation)
	}
	copyValue := copyRecord(*record)
	if err := ValidateRecord(copyValue); err != nil {
		return domain.UsageRecord{}, err
	}
	return copyValue, nil
}

func copyRecord(record domain.UsageRecord) domain.UsageRecord {
	copyValue := record
	copyValue.ReservedAt = cloneTime(record.ReservedAt)
	copyValue.ReleasedAt = cloneTime(record.ReleasedAt)
	copyValue.BillableAt = cloneTime(record.BillableAt)
	copyValue.ChargedAt = cloneTime(record.ChargedAt)
	copyValue.FailedAt = cloneTime(record.FailedAt)
	return copyValue
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
