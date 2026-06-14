package app

import (
	"context"
	"errors"
	"testing"

	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
)

type billingAdmissionServiceFunc func(
	context.Context,
	billingapp.AdmissionInput,
) (billingapp.AdmissionResult, error)

func (function billingAdmissionServiceFunc) Admit(
	ctx context.Context,
	input billingapp.AdmissionInput,
) (billingapp.AdmissionResult, error) {
	return function(ctx, input)
}

func TestLLMRequestBillingAdmitterMapsInputAndResult(
	t *testing.T,
) {
	var got billingapp.AdmissionInput
	adapter, err := NewLLMRequestBillingAdmitter(
		billingAdmissionServiceFunc(
			func(
				_ context.Context,
				input billingapp.AdmissionInput,
			) (billingapp.AdmissionResult, error) {
				got = input
				return billingapp.AdmissionResult{
					Allowed:               true,
					RemoteBalanceCents:    1000,
					PendingAmountCents:    100,
					EffectiveBalanceCents: 900,
					RequiredReserveCents:  20,
					Currency:              "RUB",
				}, nil
			},
		),
	)
	if err != nil {
		t.Fatalf("NewLLMRequestBillingAdmitter: %v", err)
	}

	result, err := adapter.Admit(
		context.Background(),
		llmrequest.BillingAdmissionInput{
			Principal: llmrequest.Principal{
				UserID:               "user-1",
				APIKeyID:             "key-1",
				BillingSubjectUserID: "billing-1",
			},
			RequiredReserveCents: 20,
			Currency:             "RUB",
		},
	)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}

	if got != (billingapp.AdmissionInput{
		UserID:               "user-1",
		BillingSubjectUserID: "billing-1",
		RequiredReserveCents: 20,
		Currency:             "RUB",
	}) {
		t.Fatalf("billing input = %+v", got)
	}
	if result != (llmrequest.BillingAdmissionResult{
		Allowed:               true,
		RemoteBalanceCents:    1000,
		PendingAmountCents:    100,
		EffectiveBalanceCents: 900,
		RequiredReserveCents:  20,
		Currency:              "RUB",
	}) {
		t.Fatalf("admission result = %+v", result)
	}
}

func TestLLMRequestBillingAdmitterPreservesDeniedResult(
	t *testing.T,
) {
	adapter, err := NewLLMRequestBillingAdmitter(
		billingAdmissionServiceFunc(
			func(
				context.Context,
				billingapp.AdmissionInput,
			) (billingapp.AdmissionResult, error) {
				return billingapp.AdmissionResult{
					Allowed:               false,
					RemoteBalanceCents:    100,
					PendingAmountCents:    90,
					EffectiveBalanceCents: 10,
					RequiredReserveCents:  20,
					Currency:              "RUB",
				}, nil
			},
		),
	)
	if err != nil {
		t.Fatalf("NewLLMRequestBillingAdmitter: %v", err)
	}

	result, err := adapter.Admit(
		context.Background(),
		llmrequest.BillingAdmissionInput{
			Principal: llmrequest.Principal{
				UserID:               "user-1",
				BillingSubjectUserID: "billing-1",
			},
			RequiredReserveCents: 20,
			Currency:             "RUB",
		},
	)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if result.Allowed {
		t.Fatalf("result = %+v", result)
	}
	if result.EffectiveBalanceCents != 10 ||
		result.RequiredReserveCents != 20 {
		t.Fatalf("result = %+v", result)
	}
}

func TestLLMRequestBillingAdmitterPreservesBillingError(
	t *testing.T,
) {
	stageError := errors.New("billing failed")
	adapter, err := NewLLMRequestBillingAdmitter(
		billingAdmissionServiceFunc(
			func(
				context.Context,
				billingapp.AdmissionInput,
			) (billingapp.AdmissionResult, error) {
				return billingapp.AdmissionResult{}, stageError
			},
		),
	)
	if err != nil {
		t.Fatalf("NewLLMRequestBillingAdmitter: %v", err)
	}

	_, err = adapter.Admit(
		context.Background(),
		llmrequest.BillingAdmissionInput{},
	)
	if !errors.Is(err, stageError) {
		t.Fatalf("error = %v", err)
	}
}

func TestLLMRequestBillingAdmitterRejectsNilContextBeforeDependency(
	t *testing.T,
) {
	called := false
	adapter, err := NewLLMRequestBillingAdmitter(
		billingAdmissionServiceFunc(
			func(
				context.Context,
				billingapp.AdmissionInput,
			) (billingapp.AdmissionResult, error) {
				called = true
				return billingapp.AdmissionResult{}, nil
			},
		),
	)
	if err != nil {
		t.Fatalf("NewLLMRequestBillingAdmitter: %v", err)
	}

	_, err = adapter.Admit(
		nil,
		llmrequest.BillingAdmissionInput{},
	)
	if !errors.Is(err, llmrequest.ErrInvalidInput) {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("dependency was called")
	}
}

func TestLLMRequestBillingAdmitterPropagatesCanceledContext(
	t *testing.T,
) {
	called := false
	adapter, err := NewLLMRequestBillingAdmitter(
		billingAdmissionServiceFunc(
			func(
				context.Context,
				billingapp.AdmissionInput,
			) (billingapp.AdmissionResult, error) {
				called = true
				return billingapp.AdmissionResult{}, nil
			},
		),
	)
	if err != nil {
		t.Fatalf("NewLLMRequestBillingAdmitter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = adapter.Admit(
		ctx,
		llmrequest.BillingAdmissionInput{},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("dependency was called")
	}
}

func TestNewLLMRequestBillingAdmitterRequiresDependency(
	t *testing.T,
) {
	_, err := NewLLMRequestBillingAdmitter(nil)
	if !errors.Is(err, llmrequest.ErrDependencyRequired) {
		t.Fatalf("error = %v", err)
	}
}
