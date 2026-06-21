package llmrequestadapters

import (
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/auth"
	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	llmrequest "github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	pricingapp "github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type publicAuthenticationUseCaseFake struct {
	got    authenticateapp.Input
	result authenticateapp.Result
	err    error
}

func (f *publicAuthenticationUseCaseFake) AuthenticatePublicRequest(
	_ context.Context,
	input authenticateapp.Input,
) (authenticateapp.Result, error) {
	f.got = input
	return f.result, f.err
}

func TestPublicAuthenticationAdapterMapsPrincipal(t *testing.T) {
	fake := &publicAuthenticationUseCaseFake{
		result: authenticateapp.Result{
			Principal: auth.APIKeyPrincipal{
				UserID:               "user-1",
				APIKeyID:             "key-1",
				BillingSubjectUserID: "billing-1",
			},
		},
	}
	adapter := llmRequestPublicAuthenticationAdapter{usecase: fake}

	principal, err := adapter.AuthenticatePublicRequest(
		context.Background(),
		"sk-test",
	)
	if err != nil {
		t.Fatalf("AuthenticatePublicRequest: %v", err)
	}
	if fake.got.RawAPIKey != "sk-test" ||
		principal.UserID != "user-1" ||
		principal.APIKeyID != "key-1" ||
		principal.BillingSubjectUserID != "billing-1" {
		t.Fatalf("principal=%+v input=%+v", principal, fake.got)
	}
}

type billingAdmissionUseCaseFake struct {
	got    billingapp.AdmissionInput
	result billingapp.AdmissionResult
	err    error
}

func (f *billingAdmissionUseCaseFake) Admit(
	_ context.Context,
	input billingapp.AdmissionInput,
) (billingapp.AdmissionResult, error) {
	f.got = input
	return f.result, f.err
}

func TestBillingAdmissionAdapterMapsAdmission(t *testing.T) {
	fake := &billingAdmissionUseCaseFake{
		result: billingapp.AdmissionResult{
			Allowed:               true,
			RemoteBalanceCents:    1000,
			PendingAmountCents:    200,
			EffectiveBalanceCents: 800,
			RequiredReserveCents:  300,
			Currency:              "RUB",
		},
	}
	adapter := llmRequestBillingAdmissionAdapter{usecase: fake}

	result, err := adapter.Admit(
		context.Background(),
		llmrequest.BillingAdmissionInput{
			Principal: llmrequest.Principal{
				UserID:               "user-1",
				BillingSubjectUserID: "billing-1",
			},
			RequiredReserveCents: 300,
			Currency:             "RUB",
		},
	)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if fake.got.UserID != "user-1" ||
		fake.got.BillingSubjectUserID != "billing-1" ||
		fake.got.RequiredReserveCents != 300 ||
		fake.got.Currency != "RUB" ||
		!result.Allowed ||
		result.EffectiveBalanceCents != 800 {
		t.Fatalf("result=%+v input=%+v", result, fake.got)
	}
}

type autoChargeUseCaseFake struct {
	got    billingapp.AutoChargeInput
	result billingapp.AutoChargeResult
	err    error
}

func (f *autoChargeUseCaseFake) Run(
	_ context.Context,
	input billingapp.AutoChargeInput,
) (billingapp.AutoChargeResult, error) {
	f.got = input
	return f.result, f.err
}

func TestAutoChargeAdapterMapsDeferredError(t *testing.T) {
	fake := &autoChargeUseCaseFake{err: billingapp.ErrChargeDeferred}
	adapter := llmRequestAutoChargeAdapter{usecase: fake}

	result, err := adapter.Run(
		context.Background(),
		llmrequest.AutoChargeServiceInput{
			UserID:               "user-1",
			BillingSubjectUserID: "billing-1",
			Currency:             "RUB",
		},
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Deferred ||
		fake.got.UserID != "user-1" ||
		fake.got.BillingSubjectUserID != "billing-1" ||
		fake.got.Currency != "RUB" {
		t.Fatalf("result=%+v input=%+v", result, fake.got)
	}
}

type usagePricingResolverFake struct {
	got    pricingapp.ResolveUsageInput
	result pricingapp.ResolvedUsageResult
	err    error
}

func (f *usagePricingResolverFake) Resolve(
	_ context.Context,
	input pricingapp.ResolveUsageInput,
) (pricingapp.ResolvedUsageResult, error) {
	f.got = input
	return f.result, f.err
}

func TestUsagePricingResolverAdapterCopiesMutableInputs(t *testing.T) {
	fake := &usagePricingResolverFake{
		result: pricingapp.ResolvedUsageResult{
			Usage:        domain.TokenUsage{InputTokens: 1},
			Completeness: pricingapp.UsageCompletenessDetailed,
			Currency:     "RUB",
		},
	}
	adapter := llmRequestUsagePricingResolverAdapter{resolver: fake}
	requestBody := []byte{1, 2}
	responseBody := []byte{3, 4}
	actualUsage := domain.TokenUsage{InputTokens: 5}

	_, err := adapter.Resolve(
		context.Background(),
		llmrequest.UsagePricingInput{
			RequestBody:  requestBody,
			ResponseBody: responseBody,
			ActualUsage:  &actualUsage,
		},
	)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	requestBody[0] = 9
	responseBody[0] = 9
	actualUsage.InputTokens = 9

	if fake.got.RequestBody[0] != 1 ||
		fake.got.ResponseBody[0] != 3 ||
		fake.got.ActualUsage == nil ||
		fake.got.ActualUsage.InputTokens != 5 {
		t.Fatalf("resolver input was not defensively copied: %+v", fake.got)
	}
}

func TestUsagePricingResolverAdapterPropagatesResolverError(t *testing.T) {
	want := errors.New("resolver failed")
	adapter := llmRequestUsagePricingResolverAdapter{
		resolver: &usagePricingResolverFake{err: want},
	}

	_, err := adapter.Resolve(
		context.Background(),
		llmrequest.UsagePricingInput{
			RequestBody:  []byte{1},
			ResponseBody: []byte{2},
		},
	)
	if !errors.Is(err, want) {
		t.Fatalf("error=%v want=%v", err, want)
	}
}
