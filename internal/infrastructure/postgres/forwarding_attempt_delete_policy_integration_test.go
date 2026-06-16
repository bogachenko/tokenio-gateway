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

func TestForwardingAttemptOwnershipDeletePolicyIntegration(t *testing.T) {
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

	store, err := NewForwardingAttemptStore(db)
	if err != nil {
		t.Fatalf("NewForwardingAttemptStore: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "forwarding-delete-user-" + suffix
	resellerID := "forwarding-delete-reseller-" + suffix
	routeID := "forwarding-delete-route-" + suffix
	localRequestID := "forwarding-delete-request-" + suffix
	model := "forwarding-delete-model-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	insertOperationalRegistry(
		t,
		db,
		userID,
		"forwarding-delete-billing-"+suffix,
		resellerID,
		routeID,
		model,
		now,
	)

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_usage_records WHERE local_request_id = $1",
			localRequestID,
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

	reservedAt := now
	record := domain.UsageRecord{
		LocalRequestID:     localRequestID,
		UserID:             userID,
		APIFamily:          domain.APIFamilyOpenAICompatible,
		EndpointKind:       domain.EndpointChat,
		ClientModel:        model,
		BillingModel:       "openai:" + model,
		SelectedRouteID:    routeID,
		SelectedResellerID: resellerID,
		ProviderType:       domain.ProviderOpenAI,
		ProviderModel:      model,
		EstimatedUsage: domain.TokenUsage{
			InputTokens:  1,
			OutputTokens: 1,
		},
		EstimatedClientAmountCents: 1,
		EstimatedUpstreamCostCents: 1,
		Currency:                   "RUB",
		UsageCompleteness:          "missing",
		Status:                     domain.UsageStatusReserved,
		CreatedAt:                  now,
		ReservedAt:                 &reservedAt,
		UpdatedAt:                  now,
	}
	if _, err := db.Exec(
		ctx,
		insertUsageRecordSQL,
		usageRecordNamedArgs(record),
	); err != nil {
		t.Fatalf("insert usage record: %v", err)
	}

	attempt := domain.ForwardingAttempt{
		LocalRequestID: localRequestID,
		AttemptNumber:  1,
		RouteID:        routeID,
		ResellerID:     resellerID,
		APIFamily:      domain.APIFamilyOpenAICompatible,
		EndpointKind:   domain.EndpointChat,
		ClientModel:    model,
		ProviderType:   domain.ProviderOpenAI,
		ProviderModel:  model,
		Status:         domain.ForwardingAttemptStatusStarted,
		StartedAt:      now.Add(time.Second),
	}
	started, err := store.StartAttempt(ctx, attempt)
	if err != nil {
		t.Fatalf("StartAttempt: %v", err)
	}
	if !forwardingAttemptsEqual(started, attempt) {
		t.Fatalf("started attempt = %+v, want %+v", started, attempt)
	}

	if _, err := db.Exec(
		ctx,
		`
UPDATE tokenio_usage_records
SET
    selected_route_id = NULL,
    selected_reseller_id = NULL
WHERE local_request_id = $1
`,
		localRequestID,
	); err != nil {
		t.Fatalf("detach nullable usage references: %v", err)
	}

	if _, err := db.Exec(
		ctx,
		"DELETE FROM tokenio_routes WHERE id = $1",
		routeID,
	); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("route delete error=%v, want ErrStoreConflict", err)
	}
	assertForwardingDeletePolicyCount(
		t,
		ctx,
		db,
		"tokenio_routes",
		"id",
		routeID,
		1,
	)
	assertForwardingDeletePolicyCount(
		t,
		ctx,
		db,
		"tokenio_forwarding_attempts",
		"local_request_id",
		localRequestID,
		1,
	)

	tag, err := db.Exec(
		ctx,
		"DELETE FROM tokenio_usage_records WHERE local_request_id = $1",
		localRequestID,
	)
	if err != nil {
		t.Fatalf("delete owning usage record: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf(
			"deleted usage rows=%d, want 1",
			tag.RowsAffected(),
		)
	}

	assertForwardingDeletePolicyCount(
		t,
		ctx,
		db,
		"tokenio_forwarding_attempts",
		"local_request_id",
		localRequestID,
		0,
	)

	tag, err = db.Exec(
		ctx,
		"DELETE FROM tokenio_routes WHERE id = $1",
		routeID,
	)
	if err != nil {
		t.Fatalf("delete route after owned attempts cascade: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf(
			"deleted route rows=%d, want 1",
			tag.RowsAffected(),
		)
	}

	assertForwardingDeletePolicyCount(
		t,
		ctx,
		db,
		"tokenio_resellers",
		"id",
		resellerID,
		1,
	)
}

func assertForwardingDeletePolicyCount(
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
