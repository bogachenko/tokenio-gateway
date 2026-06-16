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

func TestOperationalStoresIntegration(t *testing.T) {
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

	sessions, err := NewBillingSessionStore(db)
	if err != nil {
		t.Fatal(err)
	}
	events, err := NewRouteEventStore(db)
	if err != nil {
		t.Fatal(err)
	}
	alerts, err := NewTelegramAlertStore(db)
	if err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "operational-user-" + suffix
	billingSubject := "operational-billing-" + suffix
	resellerID := "operational-reseller-" + suffix
	routeID := "operational-route-" + suffix
	eventID := "operational-event-" + suffix
	alertID := "operational-alert-" + suffix
	suppressedAlertID := "operational-alert-suppressed-" + suffix
	model := "operational-model-" + suffix
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
			"DELETE FROM tokenio_telegram_alerts WHERE id IN ($1, $2)",
			alertID,
			suppressedAlertID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_route_events WHERE id = $1",
			eventID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_billing_sessions WHERE user_id = $1",
			userID,
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

	session := domain.BillingSession{
		UserID:                   userID,
		BillingSubjectUserID:     billingSubject,
		RemoteBalanceCents:       1000,
		PendingAmountCentsCached: 100,
		Currency:                 "RUB",
		FetchedAt:                now,
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	createdSession, err := sessions.UpsertBillingSession(
		ctx,
		nil,
		session,
	)
	if err != nil {
		t.Fatalf("create billing session: %v", err)
	}
	if !sameBillingSession(createdSession, session) {
		t.Fatalf("created session = %+v", createdSession)
	}

	nextSession := createdSession
	nextSession.RemoteBalanceCents = 900
	nextSession.PendingAmountCentsCached = 150
	nextSession.FetchedAt = now.Add(time.Second)
	nextSession.UpdatedAt = nextSession.FetchedAt
	updatedSession, err := sessions.UpsertBillingSession(
		ctx,
		&createdSession,
		nextSession,
	)
	if err != nil {
		t.Fatalf("update billing session: %v", err)
	}
	if !sameBillingSession(updatedSession, nextSession) {
		t.Fatalf("updated session = %+v", updatedSession)
	}

	staleSession := nextSession
	staleSession.RemoteBalanceCents = 800
	staleSession.FetchedAt = now.Add(2 * time.Second)
	staleSession.UpdatedAt = staleSession.FetchedAt
	if _, err := sessions.UpsertBillingSession(
		ctx,
		&createdSession,
		staleSession,
	); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("stale session error = %v", err)
	}

	event := domain.RouteEvent{
		ID:             eventID,
		RouteID:        routeID,
		ResellerID:     resellerID,
		ProviderType:   domain.ProviderOpenAI,
		APIFamily:      domain.APIFamilyOpenAICompatible,
		EndpointKind:   domain.EndpointChat,
		ClientModel:    model,
		EventType:      domain.RouteEventTypeSelected,
		LocalRequestID: "llmreq-" + suffix,
		Metadata: domain.RouteEventMetadata{
			"attempt": 1,
		},
		CreatedAt: now.Add(3 * time.Second),
	}
	if err := events.AppendRouteEvent(ctx, event); err != nil {
		t.Fatalf("AppendRouteEvent: %v", err)
	}
	if err := events.AppendRouteEvent(ctx, event); err != nil {
		t.Fatalf("AppendRouteEvent replay: %v", err)
	}
	differentEvent := event
	differentEvent.Reason = "different"
	if err := events.AppendRouteEvent(
		ctx,
		differentEvent,
	); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("event conflict error = %v", err)
	}

	eventPage, err := events.ListRouteEvents(
		ctx,
		ports.RouteEventListFilter{
			RouteID:   routeID,
			EventType: domain.RouteEventTypeSelected,
			Page:      ports.PageRequest{Limit: 10},
		},
	)
	if err != nil {
		t.Fatalf("ListRouteEvents: %v", err)
	}
	if eventPage.Total != 1 ||
		len(eventPage.Items) != 1 ||
		eventPage.Items[0].ID != eventID {
		t.Fatalf("event page = %+v", eventPage)
	}

	alert := domain.TelegramAlert{
		ID:         alertID,
		AlertType:  "balance_low",
		DedupeKey:  resellerID,
		ResellerID: resellerID,
		RouteID:    routeID,
		Message:    "Low balance",
		Status:     domain.TelegramAlertStatusPending,
		CreatedAt:  now.Add(4 * time.Second),
	}
	createdAlert, err := alerts.CreateOrSuppressTelegramAlert(
		ctx,
		alert,
		5*time.Minute,
	)
	if err != nil {
		t.Fatalf("CreateOrSuppressTelegramAlert: %v", err)
	}
	if createdAlert.Status != domain.TelegramAlertStatusPending {
		t.Fatalf("created alert = %+v", createdAlert)
	}

	suppressed := alert
	suppressed.ID = suppressedAlertID
	suppressed.CreatedAt = alert.CreatedAt.Add(time.Second)
	suppressedResult, err := alerts.CreateOrSuppressTelegramAlert(
		ctx,
		suppressed,
		5*time.Minute,
	)
	if err != nil {
		t.Fatalf("create suppressed alert: %v", err)
	}
	if suppressedResult.Status !=
		domain.TelegramAlertStatusSuppressed {
		t.Fatalf("suppressed alert = %+v", suppressedResult)
	}

	failed := createdAlert
	failed.Status = domain.TelegramAlertStatusFailed
	failed.Error = "telegram_unavailable"
	failedResult, err := alerts.CompareAndSwapTelegramAlert(
		ctx,
		createdAlert,
		failed,
	)
	if err != nil {
		t.Fatalf("mark alert failed: %v", err)
	}

	retry := failedResult
	retry.Status = domain.TelegramAlertStatusPending
	retry.Error = ""
	retryResult, err := alerts.CompareAndSwapTelegramAlert(
		ctx,
		failedResult,
		retry,
	)
	if err != nil {
		t.Fatalf("retry alert: %v", err)
	}

	sent := retryResult
	sent.Status = domain.TelegramAlertStatusSent
	sentAt := now.Add(6 * time.Second)
	sent.SentAt = &sentAt
	sentResult, err := alerts.CompareAndSwapTelegramAlert(
		ctx,
		retryResult,
		sent,
	)
	if err != nil {
		t.Fatalf("mark alert sent: %v", err)
	}
	if sentResult.Status != domain.TelegramAlertStatusSent {
		t.Fatalf("sent alert = %+v", sentResult)
	}

	alertPage, err := alerts.ListTelegramAlerts(
		ctx,
		ports.TelegramAlertListFilter{
			AlertType:  "balance_low",
			ResellerID: resellerID,
			Page:       ports.PageRequest{Limit: 10},
		},
	)
	if err != nil {
		t.Fatalf("ListTelegramAlerts: %v", err)
	}
	if alertPage.Total != 2 || len(alertPage.Items) != 2 {
		t.Fatalf("alert page = %+v", alertPage)
	}

	assertOperationalRouteDeleteProtected(
		t,
		ctx,
		db,
		routeID,
		eventID,
		alertID,
		suppressedAlertID,
	)
}

func assertOperationalRouteDeleteProtected(
	t *testing.T,
	ctx context.Context,
	db *DB,
	routeID string,
	eventID string,
	alertID string,
	suppressedAlertID string,
) {
	t.Helper()

	if _, err := db.Exec(
		ctx,
		"DELETE FROM tokenio_routes WHERE id = $1",
		routeID,
	); err == nil {
		t.Fatal("deleting route referenced by operational history unexpectedly succeeded")
	}

	assertOperationalRowCount(
		t,
		ctx,
		db,
		"tokenio_routes",
		"id",
		routeID,
		1,
	)
	assertOperationalRowCount(
		t,
		ctx,
		db,
		"tokenio_route_events",
		"id",
		eventID,
		1,
	)

	var alertCount int
	if err := db.QueryRow(
		ctx,
		`
SELECT COUNT(*)
FROM tokenio_telegram_alerts
WHERE id IN ($1, $2)
`,
		alertID,
		suppressedAlertID,
	).Scan(&alertCount); err != nil {
		t.Fatalf("count persisted alerts: %v", err)
	}
	if alertCount != 2 {
		t.Fatalf("persisted alert count=%d, want 2", alertCount)
	}
}

func assertOperationalRowCount(
	t *testing.T,
	ctx context.Context,
	db *DB,
	table string,
	column string,
	value string,
	want int,
) {
	t.Helper()

	query := "SELECT COUNT(*) FROM " + table + " WHERE " + column + " = $1"
	var got int
	if err := db.QueryRow(ctx, query, value).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count=%d, want %d", table, got, want)
	}
}

func insertOperationalRegistry(
	t *testing.T,
	db *DB,
	userID string,
	billingSubject string,
	resellerID string,
	routeID string,
	model string,
	now time.Time,
) {
	t.Helper()
	ctx := t.Context()

	statements := []struct {
		sql  string
		args []any
	}{
		{
			`
INSERT INTO tokenio_users (
    id,
    external_billing_user_id,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $3)
`,
			[]any{userID, billingSubject, now},
		},
		{
			`
INSERT INTO tokenio_resellers (
    id,
    name,
    provider_type,
    base_url,
    api_key_env,
    enabled,
    balance_cents,
    reserved_cents,
    minimum_balance_cents,
    created_at,
    updated_at
)
VALUES (
    $1,
    'operational-test',
    'openai',
    'https://example.test',
    $2,
    TRUE,
    1000,
    0,
    0,
    $3,
    $3
)
`,
			[]any{resellerID, "OPERATIONAL_KEY_" + resellerID, now},
		},
		{
			`
INSERT INTO tokenio_routes (
    id,
    reseller_id,
    provider_type,
    api_family,
    endpoint_kind,
    client_model,
    provider_model,
    default_max_output_tokens,
    capabilities,
    created_at,
    updated_at
)
VALUES (
    $1,
    $2,
    'openai',
    'openai_compatible',
    'chat',
    $3,
    $3,
    1024,
    '{"chat":true}'::jsonb,
    $4,
    $4
)
`,
			[]any{routeID, resellerID, model, now},
		},
	}

	for _, statement := range statements {
		if _, err := db.Exec(
			ctx,
			statement.sql,
			statement.args...,
		); err != nil {
			t.Fatalf("insert operational dependency: %v", err)
		}
	}
}
