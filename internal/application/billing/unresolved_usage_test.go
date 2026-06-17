package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type unresolvedIdentity struct{}

func (unresolvedIdentity) TokenForSubject(context.Context, string) (string, error) {
	return "billing-token", nil
}

type unresolvedBalance struct{}

func (unresolvedBalance) GetBalance(context.Context, string) (ports.BillingBalance, error) {
	return ports.BillingBalance{
		Currency:     "RUB",
		BalanceCents: 1000,
	}, nil
}

type unresolvedLedger struct{}

func (unresolvedLedger) CreateReserved(context.Context, domain.UsageRecord) (ports.UsageReserveResult, error) {
	panic("unexpected CreateReserved")
}

func (unresolvedLedger) FindByLocalRequestID(context.Context, string) (*domain.UsageRecord, error) {
	panic("unexpected FindByLocalRequestID")
}

func (unresolvedLedger) CompareAndSwap(
	context.Context,
	string,
	domain.UsageStatus,
	domain.UsageRecord,
) (ports.UsageTransitionResult, error) {
	panic("unexpected CompareAndSwap")
}

func (unresolvedLedger) LoadExposure(
	context.Context,
	string,
	string,
) (ports.UsageExposureSnapshot, error) {
	return ports.UsageExposureSnapshot{
		Currency:           "RUB",
		PricingFailedCount: 1,
	}, nil
}

func (unresolvedLedger) LoadOpenChargeBatches(
	context.Context,
	string,
	string,
	string,
) ([]ports.BillingChargeBatchSnapshot, error) {
	panic("unexpected LoadOpenChargeBatches")
}

func (unresolvedLedger) LoadChargeCandidates(
	context.Context,
	string,
	string,
) ([]domain.UsageRecord, error) {
	panic("unexpected LoadChargeCandidates")
}

func (unresolvedLedger) PrepareChargeBatch(
	context.Context,
	ports.UsageChargeBatchPlan,
) (ports.BillingChargeBatchSnapshot, error) {
	panic("unexpected PrepareChargeBatch")
}

func (unresolvedLedger) MarkChargeBatchFailed(
	context.Context,
	string,
	domain.BillingChargeStatus,
	string,
	time.Time,
) error {
	panic("unexpected MarkChargeBatchFailed")
}

func (unresolvedLedger) ApplyChargeSuccess(
	context.Context,
	ports.UsageChargeSuccess,
) error {
	panic("unexpected ApplyChargeSuccess")
}

func TestAdmissionNormalizesUnresolvedUsageAtApplicationBoundary(t *testing.T) {
	service, err := NewAdmissionService(
		unresolvedIdentity{},
		unresolvedBalance{},
		unresolvedLedger{},
		AdmissionConfig{},
	)
	if err != nil {
		t.Fatalf("NewAdmissionService: %v", err)
	}

	_, err = service.Admit(
		context.Background(),
		AdmissionInput{
			UserID:               "user-1",
			BillingSubjectUserID: "billing-1",
			RequiredReserveCents: 1,
			Currency:             "RUB",
		},
	)
	if !errors.Is(err, ErrUnresolvedUsage) {
		t.Fatalf("error = %v, want ErrUnresolvedUsage", err)
	}
	if !errors.Is(err, domain.ErrUnresolvedUsage) {
		t.Fatalf("error = %v, want domain.ErrUnresolvedUsage compatibility", err)
	}

	failure, ok := ports.AsApplicationError(err)
	if !ok {
		t.Fatal("billing admission error is not normalized")
	}
	if failure.Code != domain.ErrorCodeUnresolvedUsage ||
		failure.SafeMessage != "Previous usage requires resolution" ||
		failure.Category != ports.FailureCategoryConflict ||
		failure.Retryability != ports.RetryabilityNonRetryable ||
		failure.RequestStage != ports.RequestStagePreForwarding {
		t.Fatalf("failure = %+v", failure)
	}
}

type insufficientBalance struct{}

func (insufficientBalance) GetBalance(
	context.Context,
	string,
) (ports.BillingBalance, error) {
	return ports.BillingBalance{
		Currency:     "RUB",
		BalanceCents: 10,
	}, nil
}

type resolvedLedger struct {
	unresolvedLedger
}

func (resolvedLedger) LoadExposure(
	context.Context,
	string,
	string,
) (ports.UsageExposureSnapshot, error) {
	return ports.UsageExposureSnapshot{Currency: "RUB"}, nil
}

func TestAdmissionNormalizesInsufficientFundsAtApplicationBoundary(
	t *testing.T,
) {
	service, err := NewAdmissionService(
		unresolvedIdentity{},
		insufficientBalance{},
		resolvedLedger{},
		AdmissionConfig{},
	)
	if err != nil {
		t.Fatalf("NewAdmissionService: %v", err)
	}

	_, err = service.Admit(
		context.Background(),
		AdmissionInput{
			UserID:               "user-1",
			BillingSubjectUserID: "billing-1",
			RequiredReserveCents: 20,
			Currency:             "RUB",
		},
	)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("error = %v, want ErrInsufficientFunds", err)
	}
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("error = %v, want domain.ErrInsufficientFunds compatibility", err)
	}

	failure, ok := ports.AsApplicationError(err)
	if !ok {
		t.Fatal("billing insufficient funds error is not normalized")
	}
	if failure.Code != domain.ErrorCodeInsufficientFunds ||
		failure.SafeMessage != "Insufficient balance" ||
		failure.Category != ports.FailureCategoryPaymentRequired ||
		failure.Retryability != ports.RetryabilityNonRetryable ||
		failure.RequestStage != ports.RequestStagePreForwarding {
		t.Fatalf("failure = %+v", failure)
	}
}
