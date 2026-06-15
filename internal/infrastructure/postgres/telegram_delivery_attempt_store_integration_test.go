package postgres

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestTelegramDeliveryAttemptStoreIntegration(t *testing.T) {
	dsn := os.Getenv("TOKENIO_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TOKENIO_TEST_DATABASE_DSN is not set")
	}

	ctx := t.Context()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	alerts, err := NewTelegramAlertStore(db)
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := NewTelegramDeliveryAttemptStore(db)
	if err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "delivery-attempt-user-" + suffix
	billingSubject := "delivery-attempt-billing-" + suffix
	resellerID := "delivery-attempt-reseller-" + suffix
	routeID := "delivery-attempt-route-" + suffix
	model := "delivery-attempt-model-" + suffix
	alertID := "delivery-attempt-alert-" + suffix
	secondAlertID := "delivery-attempt-alert-b-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	insertOperationalRegistry(
		t,
		db,
		userID,
		billingSubject,
		resellerID,
		routeID,
		model,
		now,
	)

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			`DELETE FROM tokenio_telegram_delivery_attempts
WHERE alert_id IN ($1, $2)`,
			alertID,
			secondAlertID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_telegram_alerts WHERE id IN ($1, $2)",
			alertID,
			secondAlertID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_routes WHERE id = $1",
			routeID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_resellers WHERE id = $1",
			resellerID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_users WHERE id = $1",
			userID,
		)
	})

	for index, id := range []string{alertID, secondAlertID} {
		created, err := alerts.CreateOrSuppressTelegramAlert(
			ctx,
			domain.TelegramAlert{
				ID:         id,
				AlertType:  "balance_low",
				DedupeKey:  id,
				ResellerID: resellerID,
				RouteID:    routeID,
				Message:    "Low balance",
				Status:     domain.TelegramAlertStatusPending,
				CreatedAt:  now.Add(time.Duration(index) * time.Second),
			},
			time.Minute,
		)
		if err != nil {
			t.Fatalf("create alert %s: %v", id, err)
		}
		if created.Status != domain.TelegramAlertStatusPending {
			t.Fatalf("created alert = %+v", created)
		}
	}

	first := domain.TelegramDeliveryAttempt{
		ID:            "delivery-attempt-1-" + suffix,
		AlertID:       alertID,
		AttemptNumber: 1,
		Status:        domain.TelegramDeliveryAttemptStatusStarted,
		StartedAt:     now.Add(2 * time.Second),
	}
	started, err := attempts.StartTelegramDeliveryAttempt(ctx, first)
	if err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	replayed, err := attempts.StartTelegramDeliveryAttempt(ctx, first)
	if err != nil {
		t.Fatalf("start replay: %v", err)
	}
	if !telegramDeliveryAttemptsEqual(started, replayed) {
		t.Fatalf("replayed = %+v", replayed)
	}

	conflict := first
	conflict.ID = "delivery-attempt-conflict-" + suffix
	if _, err := attempts.StartTelegramDeliveryAttempt(
		ctx,
		conflict,
	); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("start conflict error = %v", err)
	}

	completedAt := first.StartedAt.Add(time.Second)
	succeeded := started
	succeeded.Status =
		domain.TelegramDeliveryAttemptStatusSucceeded
	succeeded.AttemptState =
		domain.TelegramDeliveryAttemptStateResponseReceived
	succeeded.CompletedAt = &completedAt

	completed, err := attempts.CompleteTelegramDeliveryAttempt(
		ctx,
		succeeded,
	)
	if err != nil {
		t.Fatalf("complete attempt: %v", err)
	}
	completedReplay, err := attempts.CompleteTelegramDeliveryAttempt(
		ctx,
		succeeded,
	)
	if err != nil {
		t.Fatalf("complete replay: %v", err)
	}
	if !telegramDeliveryAttemptsEqual(completed, completedReplay) {
		t.Fatalf("completed replay = %+v", completedReplay)
	}

	differentTerminal := succeeded
	differentTerminal.Status =
		domain.TelegramDeliveryAttemptStatusFailed
	differentTerminal.AttemptState =
		domain.TelegramDeliveryAttemptStateSentNoResponse
	differentTerminal.FailureCode = "transport_uncertain"
	if _, err := attempts.CompleteTelegramDeliveryAttempt(
		ctx,
		differentTerminal,
	); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("terminal conflict error = %v", err)
	}

	second := domain.TelegramDeliveryAttempt{
		ID:            "delivery-attempt-2-" + suffix,
		AlertID:       alertID,
		AttemptNumber: 2,
		Status:        domain.TelegramDeliveryAttemptStatusStarted,
		StartedAt:     now.Add(4 * time.Second),
	}
	if _, err := attempts.StartTelegramDeliveryAttempt(
		ctx,
		second,
	); err != nil {
		t.Fatalf("start second: %v", err)
	}

	other := domain.TelegramDeliveryAttempt{
		ID:            "delivery-attempt-other-" + suffix,
		AlertID:       secondAlertID,
		AttemptNumber: 1,
		Status:        domain.TelegramDeliveryAttemptStatusStarted,
		StartedAt:     second.StartedAt,
	}
	if _, err := attempts.StartTelegramDeliveryAttempt(
		ctx,
		other,
	); err != nil {
		t.Fatalf("start other: %v", err)
	}

	loaded, err := attempts.LoadTelegramDeliveryAttempts(
		ctx,
		alertID,
		10,
	)
	if err != nil {
		t.Fatalf("load attempts: %v", err)
	}
	if len(loaded) != 2 ||
		loaded[0].AttemptNumber != 1 ||
		loaded[1].AttemptNumber != 2 {
		t.Fatalf("loaded = %+v", loaded)
	}

	stale, err := attempts.LoadStartedTelegramDeliveryAttemptsBefore(
		ctx,
		second.StartedAt.Add(time.Second),
		10,
	)
	if err != nil {
		t.Fatalf("load stale: %v", err)
	}

	var selected []domain.TelegramDeliveryAttempt
	for _, attempt := range stale {
		if attempt.AlertID == alertID ||
			attempt.AlertID == secondAlertID {
			selected = append(selected, attempt)
		}
	}
	if len(selected) != 2 ||
		selected[0].AlertID != alertID ||
		selected[0].AttemptNumber != 2 ||
		selected[1].AlertID != secondAlertID ||
		selected[1].AttemptNumber != 1 {
		t.Fatalf("selected stale = %+v", selected)
	}
}
