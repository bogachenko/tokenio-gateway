package telegramalert

import (
	"context"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestCheckResellerResetsPendingLowBalanceAlertAfterRecovery(
	t *testing.T,
) {
	at := time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC)
	reader := &resellerReaderFake{
		value: &domain.Reseller{
			ID:            "reseller-1",
			ProviderType:  domain.ProviderOpenAI,
			BalanceCents:  20_000,
			ReservedCents: 4_000,
		},
	}
	store := &alertStoreFake{}
	service := mustServiceWithClock(
		t,
		reader,
		store,
		clockFake{now: at},
	)

	result, err := service.CheckReseller(
		context.Background(),
		"reseller-1",
	)
	if err != nil {
		t.Fatalf("CheckReseller: %v", err)
	}
	if result.BelowThreshold || result.Alert != nil {
		t.Fatalf("result = %#v", result)
	}
	if store.resetCalls != 1 ||
		store.resetType != AlertTypeResellerBalanceLow ||
		store.resetKey != "reseller-1" {
		t.Fatalf(
			"reset calls=%d type=%q key=%q",
			store.resetCalls,
			store.resetType,
			store.resetKey,
		)
	}
}

func TestCheckResellerCreatesNewAlertAfterRecoveredPendingWasSuppressed(
	t *testing.T,
) {
	at := time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC)
	reader := &resellerReaderFake{
		value: &domain.Reseller{
			ID:           "reseller-1",
			ProviderType: domain.ProviderOpenAI,
			BalanceCents: 10_000,
		},
	}
	store := &alertStoreFake{}
	service := mustServiceWithClock(
		t,
		reader,
		store,
		clockFake{now: at},
	)

	result, err := service.CheckReseller(
		context.Background(),
		"reseller-1",
	)
	if err != nil {
		t.Fatalf("CheckReseller: %v", err)
	}
	if !result.BelowThreshold ||
		result.Alert == nil ||
		result.Alert.Status != domain.TelegramAlertStatusPending {
		t.Fatalf("result = %#v", result)
	}
	if store.resetCalls != 0 {
		t.Fatalf("reset calls=%d, want 0", store.resetCalls)
	}
}
