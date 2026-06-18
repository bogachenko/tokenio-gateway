package postgres

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestUsageResellerReleaseIntegration(t *testing.T) {
	fixture := newUsageResellerReleaseFixture(t, 40, 40)
	releasedAt := fixture.now.Add(time.Second)

	released, err := fixture.ledger.
		ReleaseReservedUsageAndResellerReserve(
			t.Context(),
			fixture.localRequestID,
			"connection_error",
			releasedAt,
		)
	if err != nil {
		t.Fatalf("release usage and reseller reserve: %v", err)
	}
	if !released.Applied ||
		released.Usage.Status != domain.UsageStatusReleased ||
		released.Usage.FailureReason != "connection_error" ||
		released.Usage.ReleasedAt == nil ||
		!released.Usage.ReleasedAt.Equal(releasedAt) {
		t.Fatalf("released result = %+v", released)
	}

	replayed, err := fixture.ledger.
		ReleaseReservedUsageAndResellerReserve(
			t.Context(),
			fixture.localRequestID,
			"connection_error",
			releasedAt,
		)
	if err != nil {
		t.Fatalf("release replay: %v", err)
	}
	if replayed.Applied ||
		replayed.Usage.Status != domain.UsageStatusReleased ||
		replayed.Usage.FailureReason != "connection_error" {
		t.Fatalf("replayed result = %+v", replayed)
	}

	currentReseller := fixture.loadReseller(t)
	if currentReseller.BalanceCents != 1000 ||
		currentReseller.ReservedCents != 0 {
		t.Fatalf("current reseller = %+v", currentReseller)
	}
}

func TestUsageResellerReleaseConcurrentReplayIntegration(
	t *testing.T,
) {
	fixture := newUsageResellerReleaseFixture(t, 40, 40)
	releasedAt := fixture.now.Add(time.Second)

	type callResult struct {
		value ports.UsageResellerReleaseResult
		err   error
	}

	start := make(chan struct{})
	results := make(chan callResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)

	for range 2 {
		go func() {
			ready.Done()
			<-start
			value, callErr := fixture.ledger.
				ReleaseReservedUsageAndResellerReserve(
					t.Context(),
					fixture.localRequestID,
					"connection_error",
					releasedAt,
				)
			results <- callResult{
				value: value,
				err:   callErr,
			}
		}()
	}

	ready.Wait()
	close(start)

	applied := 0
	for range 2 {
		call := <-results
		if call.err != nil {
			t.Fatalf("concurrent release error: %v", call.err)
		}
		if call.value.Applied {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("applied release count = %d, want 1", applied)
	}

	currentReseller := fixture.loadReseller(t)
	if currentReseller.ReservedCents != 0 {
		t.Fatalf("current reseller = %+v", currentReseller)
	}
	currentUsage, err := fixture.ledger.FindByLocalRequestID(
		t.Context(),
		fixture.localRequestID,
	)
	if err != nil {
		t.Fatalf("find released usage: %v", err)
	}
	if currentUsage.Status != domain.UsageStatusReleased {
		t.Fatalf("current usage = %+v", currentUsage)
	}
}

func TestUsageResellerReleaseUnderflowRollsBackIntegration(
	t *testing.T,
) {
	fixture := newUsageResellerReleaseFixture(t, 39, 40)

	_, err := fixture.ledger.
		ReleaseReservedUsageAndResellerReserve(
			t.Context(),
			fixture.localRequestID,
			"connection_error",
			fixture.now.Add(time.Second),
		)
	if !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf(
			"underflow error = %v, want contract violation",
			err,
		)
	}

	currentReseller := fixture.loadReseller(t)
	if currentReseller.ReservedCents != 39 {
		t.Fatalf("current reseller = %+v", currentReseller)
	}
	currentUsage, findErr := fixture.ledger.FindByLocalRequestID(
		t.Context(),
		fixture.localRequestID,
	)
	if findErr != nil {
		t.Fatalf("find usage after rollback: %v", findErr)
	}
	if currentUsage.Status != domain.UsageStatusReserved ||
		currentUsage.ReleasedAt != nil {
		t.Fatalf("current usage = %+v", currentUsage)
	}
}

type usageResellerReleaseFixture struct {
	ledger         *UsageLedger
	resellers      *ResellerRepository
	localRequestID string
	resellerID     string
	now            time.Time
}

func newUsageResellerReleaseFixture(
	t *testing.T,
	reservedCents int64,
	estimatedUpstreamCostCents int64,
) usageResellerReleaseFixture {
	t.Helper()

	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)

	ledger, err := NewUsageLedger(db)
	if err != nil {
		t.Fatalf("NewUsageLedger: %v", err)
	}
	resellers, err := NewResellerRepository(db)
	if err != nil {
		t.Fatalf("NewResellerRepository: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "usage-release-user-" + suffix
	keyID := "usage-release-key-" + suffix
	resellerID := "usage-release-reseller-" + suffix
	routeID := "usage-release-route-" + suffix
	localRequestID := "usage-release-request-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		for _, statement := range []struct {
			sql string
			arg string
		}{
			{
				"DELETE FROM tokenio_usage_records WHERE user_id = $1",
				userID,
			},
			{
				"DELETE FROM tokenio_routes WHERE id = $1",
				routeID,
			},
			{
				"DELETE FROM tokenio_resellers WHERE id = $1",
				resellerID,
			},
			{
				"DELETE FROM tokenio_api_keys WHERE id = $1",
				keyID,
			},
			{
				"DELETE FROM tokenio_users WHERE id = $1",
				userID,
			},
		} {
			_, _ = db.Exec(cleanupCtx, statement.sql, statement.arg)
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
VALUES ($1, $2, 'release-test', $3, 'sk_test', $4, $4)
`,
		keyID,
		userID,
		"usage-release-hash-"+suffix,
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
    reserved_cents,
    created_at,
    updated_at
)
VALUES (
    $1,
    'release-test',
    'openai',
    'https://example.test',
    $2,
    1000,
    $3,
    $4,
    $4
)
`,
		resellerID,
		"USAGE_RELEASE_TEST_KEY_"+suffix,
		reservedCents,
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
		routeID,
		resellerID,
		"usage-release-model-"+suffix,
		now,
	); err != nil {
		t.Fatalf("insert route: %v", err)
	}

	reservedAt := now
	record := domain.UsageRecord{
		LocalRequestID:     localRequestID,
		IdempotencyKey:     "usage-release-idempotency-" + suffix,
		UserID:             userID,
		APIKeyID:           keyID,
		APIFamily:          domain.APIFamilyOpenAICompatible,
		EndpointKind:       domain.EndpointChat,
		ClientModel:        "usage-release-model-" + suffix,
		BillingModel:       "openai:usage-release-model-" + suffix,
		SelectedRouteID:    routeID,
		SelectedResellerID: resellerID,
		ProviderType:       domain.ProviderOpenAI,
		ProviderModel:      "usage-release-model-" + suffix,
		EstimatedUsage: domain.TokenUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
		EstimatedClientAmountCents: 100,
		EstimatedUpstreamCostCents: estimatedUpstreamCostCents,
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
	if created.Outcome != ports.UsageReserveOutcomeCreated {
		t.Fatalf("created usage result = %+v", created)
	}

	return usageResellerReleaseFixture{
		ledger:         ledger,
		resellers:      resellers,
		localRequestID: localRequestID,
		resellerID:     resellerID,
		now:            now,
	}
}

func (f usageResellerReleaseFixture) loadReseller(
	t *testing.T,
) domain.Reseller {
	t.Helper()

	values, err := f.resellers.FindByIDs(
		t.Context(),
		[]string{f.resellerID},
	)
	if err != nil {
		t.Fatalf("FindByIDs: %v", err)
	}
	value, exists := values[f.resellerID]
	if !exists {
		t.Fatalf("reseller %q not found", f.resellerID)
	}
	return value
}
