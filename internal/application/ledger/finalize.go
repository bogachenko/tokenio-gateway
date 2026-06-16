package ledger

import (
	"context"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type ReleaseInput struct {
	LocalRequestID string
	FailureReason  string
}

type FailInput struct {
	LocalRequestID string
	FailureReason  string
}

type CommitBillableInput struct {
	LocalRequestID string

	Usage             domain.TokenUsage
	UsageCompleteness domain.UsageCompleteness

	ClientAmountCents       int64
	ActualUpstreamCostCents int64

	ProviderRequestID     string
	ProviderResponseModel string
}

type MarkPricingFailedInput struct {
	LocalRequestID string

	Usage             domain.TokenUsage
	UsageCompleteness domain.UsageCompleteness

	ProviderRequestID     string
	ProviderResponseModel string

	FailureReason string
}

func (s *Service) Release(ctx context.Context, input ReleaseInput) (domain.UsageRecord, error) {
	if err := validateLocalRequestID(input.LocalRequestID); err != nil {
		return domain.UsageRecord{}, err
	}
	if err := validateFailureReason(input.FailureReason); err != nil {
		return domain.UsageRecord{}, err
	}
	now, err := s.operationTime()
	if err != nil {
		return domain.UsageRecord{}, err
	}
	current, err := findRecord(ctx, s.ledger, input.LocalRequestID)
	if err != nil {
		return domain.UsageRecord{}, err
	}
	desired := copyRecord(current)
	desired.Status = domain.UsageStatusReleased
	desired.FailureReason = input.FailureReason
	desired.ReleasedAt = timePtr(now)
	desired.UpdatedAt = now
	return s.persistReservedTransition(ctx, current, desired, sameRelease)
}

func (s *Service) Fail(ctx context.Context, input FailInput) (domain.UsageRecord, error) {
	if err := validateLocalRequestID(input.LocalRequestID); err != nil {
		return domain.UsageRecord{}, err
	}
	if err := validateFailureReason(input.FailureReason); err != nil {
		return domain.UsageRecord{}, err
	}
	now, err := s.operationTime()
	if err != nil {
		return domain.UsageRecord{}, err
	}
	current, err := findRecord(ctx, s.ledger, input.LocalRequestID)
	if err != nil {
		return domain.UsageRecord{}, err
	}
	desired := copyRecord(current)
	desired.Status = domain.UsageStatusFailed
	desired.FailureReason = input.FailureReason
	desired.FailedAt = timePtr(now)
	desired.UpdatedAt = now
	return s.persistReservedTransition(ctx, current, desired, sameFail)
}

func (s *Service) CommitBillable(ctx context.Context, input CommitBillableInput) (domain.UsageRecord, error) {
	if err := validateCommitBillableInput(input); err != nil {
		return domain.UsageRecord{}, err
	}
	now, err := s.operationTime()
	if err != nil {
		return domain.UsageRecord{}, err
	}
	current, err := findRecord(ctx, s.ledger, input.LocalRequestID)
	if err != nil {
		return domain.UsageRecord{}, err
	}
	desired := copyRecord(current)
	desired.Status = domain.UsageStatusBillable
	desired.Usage = input.Usage
	desired.UsageCompleteness = string(input.UsageCompleteness)
	desired.ClientAmountCents = input.ClientAmountCents
	desired.ChargedAmountCents = 0
	desired.RemainingAmountCents = input.ClientAmountCents
	desired.ActualUpstreamCostCents = input.ActualUpstreamCostCents
	desired.ProviderRequestID = input.ProviderRequestID
	desired.ProviderResponseModel = input.ProviderResponseModel
	desired.BillableAt = timePtr(now)
	desired.UpdatedAt = now
	return s.persistReservedTransition(ctx, current, desired, sameBillable)
}

func (s *Service) MarkPricingFailed(ctx context.Context, input MarkPricingFailedInput) (domain.UsageRecord, error) {
	if err := validateMarkPricingFailedInput(input); err != nil {
		return domain.UsageRecord{}, err
	}
	now, err := s.operationTime()
	if err != nil {
		return domain.UsageRecord{}, err
	}
	current, err := findRecord(ctx, s.ledger, input.LocalRequestID)
	if err != nil {
		return domain.UsageRecord{}, err
	}
	desired := copyRecord(current)
	desired.Status = domain.UsageStatusPricingFailed
	desired.Usage = input.Usage
	desired.UsageCompleteness = string(input.UsageCompleteness)
	desired.ProviderRequestID = input.ProviderRequestID
	desired.ProviderResponseModel = input.ProviderResponseModel
	desired.FailureReason = input.FailureReason
	desired.ClientAmountCents = 0
	desired.ChargedAmountCents = 0
	desired.RemainingAmountCents = 0
	desired.FailedAt = timePtr(now)
	desired.UpdatedAt = now
	return s.persistReservedTransition(ctx, current, desired, samePricingFailed)
}

func validateCommitBillableInput(input CommitBillableInput) error {
	if err := validateLocalRequestID(input.LocalRequestID); err != nil {
		return err
	}
	if err := validateUsage(input.Usage); err != nil {
		return err
	}
	if err := validateNonNegativeAmount("client amount", input.ClientAmountCents); err != nil {
		return err
	}
	if err := validateNonNegativeAmount("actual upstream cost", input.ActualUpstreamCostCents); err != nil {
		return err
	}
	if !acceptedBillableCompleteness(input.UsageCompleteness) {
		return fmt.Errorf("%w: usage completeness", ErrInvalidLedgerInput)
	}
	return nil
}

func validateMarkPricingFailedInput(input MarkPricingFailedInput) error {
	if err := validateLocalRequestID(input.LocalRequestID); err != nil {
		return err
	}
	if err := validateUsage(input.Usage); err != nil {
		return err
	}
	if !acceptedPricingFailedCompleteness(input.UsageCompleteness) {
		return fmt.Errorf("%w: usage completeness", ErrInvalidLedgerInput)
	}
	if err := validateFailureReason(input.FailureReason); err != nil {
		return err
	}
	return nil
}

func (s *Service) persistReservedTransition(ctx context.Context, current domain.UsageRecord, desired domain.UsageRecord, same func(domain.UsageRecord, domain.UsageRecord) bool) (domain.UsageRecord, error) {
	if err := ValidateRecord(current); err != nil {
		return domain.UsageRecord{}, err
	}
	if current.Status == desired.Status {
		if same(current, desired) {
			return current, nil
		}
		return domain.UsageRecord{}, ErrLedgerStateConflict
	}
	if err := ValidateTransition(current.Status, desired.Status); err != nil {
		return domain.UsageRecord{}, err
	}
	if current.Status != domain.UsageStatusReserved {
		return domain.UsageRecord{}, fmt.Errorf("%w: runtime transition from %s", ErrInvalidStateTransition, current.Status)
	}
	if err := ValidateRecord(desired); err != nil {
		return domain.UsageRecord{}, err
	}
	result, err := s.ledger.CompareAndSwap(ctx, current.LocalRequestID, domain.UsageStatusReserved, desired)
	if err != nil {
		return domain.UsageRecord{}, fmt.Errorf("%w: compare and swap: %w", ErrUsageStoreUnavailable, err)
	}
	if result.Applied {
		return desired, nil
	}
	if result.Current == nil {
		return domain.UsageRecord{}, fmt.Errorf("%w: missing current", ErrUsageStoreContractViolation)
	}
	actual := copyRecord(*result.Current)
	if err := ValidateRecord(actual); err != nil {
		return domain.UsageRecord{}, err
	}
	if actual.Status == desired.Status && same(actual, desired) {
		return actual, nil
	}
	return domain.UsageRecord{}, ErrLedgerStateConflict
}

func sameRelease(current domain.UsageRecord, desired domain.UsageRecord) bool {
	return current.Status == domain.UsageStatusReleased && current.FailureReason == desired.FailureReason
}

func sameFail(current domain.UsageRecord, desired domain.UsageRecord) bool {
	return current.Status == domain.UsageStatusFailed && current.FailureReason == desired.FailureReason
}

func sameBillable(current domain.UsageRecord, desired domain.UsageRecord) bool {
	return current.Status == domain.UsageStatusBillable &&
		current.Usage == desired.Usage &&
		current.UsageCompleteness == desired.UsageCompleteness &&
		current.ClientAmountCents == desired.ClientAmountCents &&
		current.ChargedAmountCents == desired.ChargedAmountCents &&
		current.RemainingAmountCents == desired.RemainingAmountCents &&
		current.ActualUpstreamCostCents == desired.ActualUpstreamCostCents &&
		current.ProviderRequestID == desired.ProviderRequestID &&
		current.ProviderResponseModel == desired.ProviderResponseModel
}

func samePricingFailed(current domain.UsageRecord, desired domain.UsageRecord) bool {
	return current.Status == domain.UsageStatusPricingFailed &&
		current.Usage == desired.Usage &&
		current.UsageCompleteness == desired.UsageCompleteness &&
		current.ProviderRequestID == desired.ProviderRequestID &&
		current.ProviderResponseModel == desired.ProviderResponseModel &&
		current.FailureReason == desired.FailureReason &&
		current.ClientAmountCents == 0 && current.ChargedAmountCents == 0 && current.RemainingAmountCents == 0
}
