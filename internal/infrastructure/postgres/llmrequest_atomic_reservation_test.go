package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestValidateLLMRequestAtomicReservationInput(t *testing.T) {
	valid := validLLMRequestAtomicReservationInput()

	tests := []struct {
		name      string
		mutate    func(llmrequest.ReservationInput) llmrequest.ReservationInput
		wantError bool
	}{
		{
			name:   "valid",
			mutate: identityLLMRequestReservationInput,
		},
		{
			name: "invalid local request id",
			mutate: func(
				value llmrequest.ReservationInput,
			) llmrequest.ReservationInput {
				value.LocalRequestID = "request-1"
				return value
			},
			wantError: true,
		},
		{
			name: "blank API key id",
			mutate: func(
				value llmrequest.ReservationInput,
			) llmrequest.ReservationInput {
				value.Principal.APIKeyID = " "
				return value
			},
			wantError: true,
		},
		{
			name: "route reseller mismatch",
			mutate: func(
				value llmrequest.ReservationInput,
			) llmrequest.ReservationInput {
				value.Route.ResellerID = "other-reseller"
				return value
			},
			wantError: true,
		},
		{
			name: "negative upstream cost",
			mutate: func(
				value llmrequest.ReservationInput,
			) llmrequest.ReservationInput {
				value.EstimatedUpstreamCostCents = -1
				return value
			},
			wantError: true,
		},
		{
			name: "negative usage",
			mutate: func(
				value llmrequest.ReservationInput,
			) llmrequest.ReservationInput {
				value.EstimatedUsage.InputTokens = -1
				return value
			},
			wantError: true,
		},
		{
			name: "blank idempotency key",
			mutate: func(
				value llmrequest.ReservationInput,
			) llmrequest.ReservationInput {
				blank := " "
				value.IdempotencyKey = &blank
				return value
			},
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateLLMRequestAtomicReservationInput(
				test.mutate(valid),
			)
			if test.wantError {
				if !errors.Is(
					err,
					ports.ErrStoreContractViolation,
				) {
					t.Fatalf(
						"error = %v, want contract violation",
						err,
					)
				}
				return
			}
			if err != nil {
				t.Fatalf("validation error: %v", err)
			}
		})
	}
}

func TestClassifyLLMRequestReservationStatus(t *testing.T) {
	tests := []struct {
		status domain.UsageStatus
		want   error
	}{
		{
			status: domain.UsageStatusReserved,
			want:   llmrequest.ErrRequestInProgress,
		},
		{
			status: domain.UsageStatusBillable,
			want: llmrequest.
				ErrIdempotencyReplayNotAvailable,
		},
		{
			status: domain.UsageStatusReleased,
			want:   llmrequest.ErrIdempotencyKeyReused,
		},
		{
			status: domain.UsageStatusPricingFailed,
			want:   llmrequest.ErrUnresolvedUsage,
		},
		{
			status: domain.UsageStatus("unknown"),
			want:   ports.ErrStoreContractViolation,
		},
	}

	for _, test := range tests {
		t.Run(string(test.status), func(t *testing.T) {
			err := classifyLLMRequestReservationStatus(
				test.status,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func validLLMRequestAtomicReservationInput() llmrequest.ReservationInput {
	idempotencyKey := "idem-1"
	return llmrequest.ReservationInput{
		LocalRequestID: "llmreq_atomic_test",
		IdempotencyKey: &idempotencyKey,
		Principal: llmrequest.Principal{
			UserID:               "user-1",
			APIKeyID:             "key-1",
			BillingSubjectUserID: "billing-1",
		},
		APIFamily:    domain.APIFamilyOpenAICompatible,
		EndpointKind: domain.EndpointChat,
		ClientModel:  "model-1",
		BillingModel: "openai:model-1",
		Route: domain.Route{
			ID:            "route-1",
			ResellerID:    "reseller-1",
			ProviderType:  domain.ProviderOpenAI,
			APIFamily:     domain.APIFamilyOpenAICompatible,
			EndpointKind:  domain.EndpointChat,
			ClientModel:   "model-1",
			ProviderModel: "model-1",
			Enabled:       true,
		},
		Reseller: domain.Reseller{
			ID:           "reseller-1",
			ProviderType: domain.ProviderOpenAI,
			Enabled:      true,
		},
		EstimatedUsage: domain.TokenUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
		EstimatedClientAmountCents: 100,
		EstimatedUpstreamCostCents: 700,
		Currency:                   "RUB",
	}
}

func identityLLMRequestReservationInput(
	value llmrequest.ReservationInput,
) llmrequest.ReservationInput {
	return value
}

type llmRequestAtomicReservationClock struct {
	now time.Time
}

func (clock llmRequestAtomicReservationClock) Now() time.Time {
	return clock.now
}
