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

func TestUsageLedgerRecordLifecycleIntegration(t *testing.T) {
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

	ledger, err := NewUsageLedger(db)
	if err != nil {
		t.Fatalf("NewUsageLedger: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "usage-user-" + suffix
	keyID := "usage-key-" + suffix
	resellerID := "usage-reseller-" + suffix
	routeID := "usage-route-" + suffix
	localRequestID := "usage-request-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Cleanup(func() {
		for _, statement := range []struct {
			sql string
			arg string
		}{
			{"DELETE FROM tokenio_usage_records WHERE user_id = $1", userID},
			{"DELETE FROM tokenio_routes WHERE id = $1", routeID},
			{"DELETE FROM tokenio_resellers WHERE id = $1", resellerID},
			{"DELETE FROM tokenio_api_keys WHERE id = $1", keyID},
			{"DELETE FROM tokenio_users WHERE id = $1", userID},
		} {
			_, _ = db.Exec(context.Background(), statement.sql, statement.arg)
		}
	})

	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_users (
    id,
    external_billing_user_id,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $3)
`,
		userID,
		"billing-"+suffix,
		now,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_api_keys (
    id,
    user_id,
    name,
    key_hash,
    key_prefix,
    created_at,
    updated_at
)
VALUES ($1, $2, 'usage-test', $3, 'sk_test', $4, $4)
`,
		keyID,
		userID,
		"usage-hash-"+suffix,
		now,
	); err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_resellers (
    id,
    name,
    provider_type,
    base_url,
    api_key_env,
    balance_cents,
    created_at,
    updated_at
)
VALUES ($1, 'usage-test', 'openai', 'https://example.test', $2, 100000, $3, $3)
`,
		resellerID,
		"USAGE_TEST_KEY_"+suffix,
		now,
	); err != nil {
		t.Fatalf("insert reseller: %v", err)
	}
	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_routes (
    id,
    reseller_id,
    provider_type,
    api_family,
    endpoint_kind,
    client_model,
    provider_model,
    created_at,
    updated_at
)
VALUES ($1, $2, 'openai', 'openai_compatible', 'chat', $3, $3, $4, $4)
`,
		routeID,
		resellerID,
		"usage-model-"+suffix,
		now,
	); err != nil {
		t.Fatalf("insert route: %v", err)
	}

	reservedAt := now
	record := domain.UsageRecord{
		LocalRequestID:             localRequestID,
		IdempotencyKey:             "usage-idempotency-" + suffix,
		UserID:                     userID,
		APIKeyID:                   keyID,
		APIFamily:                  domain.APIFamilyOpenAICompatible,
		EndpointKind:               domain.EndpointChat,
		ClientModel:                "usage-model-" + suffix,
		BillingModel:               "openai:usage-model-" + suffix,
		SelectedRouteID:            routeID,
		SelectedResellerID:         resellerID,
		ProviderType:               domain.ProviderOpenAI,
		ProviderModel:              "usage-model-" + suffix,
		EstimatedUsage:             domain.TokenUsage{InputTokens: 10, OutputTokens: 5},
		EstimatedClientAmountCents: 100,
		EstimatedUpstreamCostCents: 40,
		Currency:                   "RUB",
		UsageCompleteness:          "missing",
		Status:                     domain.UsageStatusReserved,
		CreatedAt:                  now,
		ReservedAt:                 &reservedAt,
		UpdatedAt:                  now,
	}

	created, err := ledger.CreateReserved(ctx, record)
	if err != nil {
		t.Fatalf("CreateReserved: %v", err)
	}
	if created.Outcome != ports.UsageReserveOutcomeCreated ||
		created.Existing != nil {
		t.Fatalf("created result = %+v", created)
	}

	replayed, err := ledger.CreateReserved(ctx, record)
	if err != nil {
		t.Fatalf("CreateReserved replay: %v", err)
	}
	if replayed.Outcome != ports.UsageReserveOutcomeLocalRequestExists ||
		replayed.Existing == nil ||
		replayed.Existing.LocalRequestID != localRequestID {
		t.Fatalf("replayed result = %+v", replayed)
	}

	found, err := ledger.FindByLocalRequestID(ctx, localRequestID)
	if err != nil {
		t.Fatalf("FindByLocalRequestID: %v", err)
	}
	if found.EstimatedUsage != record.EstimatedUsage ||
		found.ReservedAt == nil ||
		!found.ReservedAt.Equal(now) {
		t.Fatalf("found record = %+v", found)
	}

	billableAt := now.Add(time.Second)
	next := *found
	next.Status = domain.UsageStatusBillable
	next.Usage = domain.TokenUsage{InputTokens: 8, OutputTokens: 4}
	next.UsageCompleteness = "detailed"
	next.ClientAmountCents = 90
	next.RemainingAmountCents = 90
	next.ActualUpstreamCostCents = 35
	next.BillableAt = &billableAt
	next.UpdatedAt = billableAt

	transition, err := ledger.CompareAndSwap(
		ctx,
		localRequestID,
		domain.UsageStatusReserved,
		next,
	)
	if err != nil {
		t.Fatalf("CompareAndSwap: %v", err)
	}
	if !transition.Applied || transition.Current != nil {
		t.Fatalf("transition = %+v", transition)
	}

	notApplied, err := ledger.CompareAndSwap(
		ctx,
		localRequestID,
		domain.UsageStatusReserved,
		next,
	)
	if err != nil {
		t.Fatalf("CompareAndSwap mismatch: %v", err)
	}
	if notApplied.Applied ||
		notApplied.Current == nil ||
		notApplied.Current.Status != domain.UsageStatusBillable {
		t.Fatalf("not-applied transition = %+v", notApplied)
	}

	exposure, err := ledger.LoadExposure(ctx, userID, "RUB")
	if err != nil {
		t.Fatalf("LoadExposure: %v", err)
	}
	if exposure.BillableRemainingAmountCents != 90 {
		t.Fatalf("exposure = %+v", exposure)
	}

	candidates, err := ledger.LoadChargeCandidates(ctx, userID, "RUB")
	if err != nil {
		t.Fatalf("LoadChargeCandidates: %v", err)
	}
	if len(candidates) != 1 ||
		candidates[0].LocalRequestID != localRequestID {
		t.Fatalf("candidates = %+v", candidates)
	}

	_, err = ledger.FindByLocalRequestID(ctx, "missing-"+suffix)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("missing error = %v, want ErrNotFound", err)
	}
}
