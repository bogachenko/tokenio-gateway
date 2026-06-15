package domain

import (
	"testing"
	"time"
)

func TestTelegramDeliveryAttemptCanonicalStates(t *testing.T) {
	statuses := []TelegramDeliveryAttemptStatus{
		TelegramDeliveryAttemptStatusStarted,
		TelegramDeliveryAttemptStatusSucceeded,
		TelegramDeliveryAttemptStatusFailed,
	}
	wantStatuses := []string{"started", "succeeded", "failed"}
	for index := range statuses {
		if string(statuses[index]) != wantStatuses[index] {
			t.Fatalf(
				"status[%d] = %q, want %q",
				index,
				statuses[index],
				wantStatuses[index],
			)
		}
	}

	states := []TelegramDeliveryAttemptState{
		TelegramDeliveryAttemptStateNotSent,
		TelegramDeliveryAttemptStateSentNoResponse,
		TelegramDeliveryAttemptStateResponseReceived,
	}
	wantStates := []string{
		"not_sent",
		"sent_no_response",
		"response_received",
	}
	for index := range states {
		if string(states[index]) != wantStates[index] {
			t.Fatalf(
				"state[%d] = %q, want %q",
				index,
				states[index],
				wantStates[index],
			)
		}
	}
}

func TestTelegramDeliveryAttemptCarriesDurableIdentity(t *testing.T) {
	startedAt := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	attempt := TelegramDeliveryAttempt{
		ID:            "tgattempt_1",
		AlertID:       "tgalt_1",
		AttemptNumber: 1,
		Status:        TelegramDeliveryAttemptStatusStarted,
		StartedAt:     startedAt,
	}

	if attempt.ID != "tgattempt_1" ||
		attempt.AlertID != "tgalt_1" ||
		attempt.AttemptNumber != 1 ||
		attempt.Status != TelegramDeliveryAttemptStatusStarted ||
		attempt.AttemptState != "" ||
		attempt.FailureCode != "" ||
		attempt.StartedAt != startedAt ||
		attempt.CompletedAt != nil {
		t.Fatalf("attempt = %#v", attempt)
	}
}
