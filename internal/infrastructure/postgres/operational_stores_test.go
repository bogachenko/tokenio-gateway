package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestOperationalStoreConstructorsRejectNilDB(t *testing.T) {
	constructors := []func() error{
		func() error {
			_, err := NewBillingSessionStore(nil)
			return err
		},
		func() error {
			_, err := NewRouteEventStore(nil)
			return err
		},
		func() error {
			_, err := NewTelegramAlertStore(nil)
			return err
		},
	}
	for index, constructor := range constructors {
		if err := constructor(); !errors.Is(
			err,
			ErrInvalidDatabaseConfig,
		) {
			t.Fatalf(
				"constructor %d error = %v, want invalid config",
				index,
				err,
			)
		}
	}
}

func TestRouteEventMetadataRejectsSecrets(t *testing.T) {
	metadata := domain.RouteEventMetadata{
		"nested": map[string]any{
			"authorization": "Bearer secret",
		},
	}
	if _, err := encodeRouteEventMetadata(metadata); !errors.Is(
		err,
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("error = %v, want contract violation", err)
	}
}

func TestRouteEventMetadataRoundTrip(t *testing.T) {
	metadata := domain.RouteEventMetadata{
		"attempt": 1,
		"code":    "rate_limited",
		"nested": map[string]any{
			"retryable": true,
		},
	}
	body, err := encodeRouteEventMetadata(metadata)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRouteEventMetadata(body)
	if err != nil {
		t.Fatal(err)
	}
	if containsOperationalSecret(decoded) {
		t.Fatal("decoded metadata contains secret")
	}
}

func TestTelegramAlertTransitions(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	pending := domain.TelegramAlert{
		ID:        "alert-1",
		AlertType: "balance_low",
		DedupeKey: "reseller-1",
		Message:   "Low balance",
		Status:    domain.TelegramAlertStatusPending,
		CreatedAt: now,
	}

	failed := pending
	failed.Status = domain.TelegramAlertStatusFailed
	failed.Error = "telegram_unavailable"
	if err := validateTelegramAlertTransition(
		pending,
		failed,
	); err != nil {
		t.Fatalf("pending -> failed: %v", err)
	}

	retry := failed
	retry.Status = domain.TelegramAlertStatusPending
	retry.Error = ""
	if err := validateTelegramAlertTransition(
		failed,
		retry,
	); err != nil {
		t.Fatalf("failed -> pending: %v", err)
	}

	sent := retry
	sent.Status = domain.TelegramAlertStatusSent
	sentAt := now.Add(time.Second)
	sent.SentAt = &sentAt
	if err := validateTelegramAlertTransition(
		retry,
		sent,
	); err != nil {
		t.Fatalf("pending -> sent: %v", err)
	}

	if err := validateTelegramAlertTransition(
		sent,
		pending,
	); !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf("sent -> pending error = %v", err)
	}
}

func TestBillingSessionValidation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	value := domain.BillingSession{
		UserID:                   "user-1",
		BillingSubjectUserID:     "billing-user-1",
		RemoteBalanceCents:       1000,
		PendingAmountCentsCached: 100,
		Currency:                 "RUB",
		FetchedAt:                now,
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	if err := validateBillingSessionPersistence(value); err != nil {
		t.Fatalf("validation error: %v", err)
	}
	value.PendingAmountCentsCached = -1
	if err := validateBillingSessionPersistence(value); !errors.Is(
		err,
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("negative pending error = %v", err)
	}
}
