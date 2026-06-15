package domain

import (
	"testing"
	"time"
)

func TestForwardingAttemptCanonicalStates(t *testing.T) {
	statuses := []ForwardingAttemptStatus{
		ForwardingAttemptStatusStarted,
		ForwardingAttemptStatusSucceeded,
		ForwardingAttemptStatusFailed,
	}
	wantStatuses := []string{
		"started",
		"succeeded",
		"failed",
	}
	for index, status := range statuses {
		if string(status) != wantStatuses[index] {
			t.Fatalf(
				"status[%d] = %q, want %q",
				index,
				status,
				wantStatuses[index],
			)
		}
	}

	states := []ForwardingAttemptState{
		ForwardingAttemptStateNotSent,
		ForwardingAttemptStateSentNoResponse,
		ForwardingAttemptStateResponseReceived,
	}
	wantStates := []string{
		"not_sent",
		"sent_no_response",
		"response_received",
	}
	for index, state := range states {
		if string(state) != wantStates[index] {
			t.Fatalf(
				"state[%d] = %q, want %q",
				index,
				state,
				wantStates[index],
			)
		}
	}
}

func TestForwardingAttemptCarriesImmutableRoutingSnapshot(t *testing.T) {
	startedAt := time.Date(
		2026,
		time.June,
		15,
		12,
		0,
		0,
		0,
		time.UTC,
	)
	attempt := ForwardingAttempt{
		LocalRequestID: "llmreq_test",
		AttemptNumber:  2,
		RouteID:        "route-fallback",
		ResellerID:     "reseller-fallback",
		APIFamily:      APIFamilyOpenAICompatible,
		EndpointKind:   EndpointChat,
		ClientModel:    "client-model",
		ProviderType:   ProviderOpenAI,
		ProviderModel:  "provider-model",
		Status:         ForwardingAttemptStatusStarted,
		StartedAt:      startedAt,
	}

	if attempt.LocalRequestID != "llmreq_test" ||
		attempt.AttemptNumber != 2 ||
		attempt.RouteID != "route-fallback" ||
		attempt.ResellerID != "reseller-fallback" ||
		attempt.APIFamily != APIFamilyOpenAICompatible ||
		attempt.EndpointKind != EndpointChat ||
		attempt.ClientModel != "client-model" ||
		attempt.ProviderType != ProviderOpenAI ||
		attempt.ProviderModel != "provider-model" ||
		attempt.Status != ForwardingAttemptStatusStarted ||
		!attempt.StartedAt.Equal(startedAt) {
		t.Fatalf("attempt snapshot = %+v", attempt)
	}
	if attempt.CompletedAt != nil ||
		attempt.AttemptState != "" ||
		attempt.UpstreamStatusCode != 0 ||
		attempt.FailureKind != "" ||
		attempt.RouteRetryCandidate {
		t.Fatalf("started attempt contains terminal facts: %+v", attempt)
	}
}
