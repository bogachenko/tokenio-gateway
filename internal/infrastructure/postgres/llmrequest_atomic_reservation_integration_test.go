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
	reservation "github.com/bogachenko/tokenio-gateway/internal/ports/llmrequestreservation"
)

func TestLLMRequestAtomicReservationIntegration(t *testing.T) {
	fixture := newLLMRequestAtomicReservationFixture(t, 700)

	created, err := fixture.store.Reserve(
		t.Context(),
		fixture.input,
	)
	if err != nil {
		t.Fatalf("Reserve created: %v", err)
	}
	if created.Disposition !=
		reservation.ReservationDispositionCreated ||
		created.Reseller == nil ||
		created.Reseller.ReservedCents != 800 ||
		created.Usage.Status != domain.UsageStatusReserved ||
		created.Usage.EstimatedUpstreamCostCents != 700 {
		t.Fatalf("created result = %+v", created)
	}

	replayed, err := fixture.store.Reserve(
		t.Context(),
		fixture.input,
	)
	if err != nil {
		t.Fatalf("Reserve replay: %v", err)
	}
	if replayed.Disposition !=
		reservation.ReservationDispositionAlreadyReserved ||
		replayed.Reseller != nil ||
		replayed.Usage.LocalRequestID !=
			fixture.input.LocalRequestID {
		t.Fatalf("replayed result = %+v", replayed)
	}

	fixture.assertResellerReservedCents(t, 800)
	fixture.assertUsageCount(t, 1)
}

func TestLLMRequestAtomicReservationConcurrentReplayIntegration(
	t *testing.T,
) {
	fixture := newLLMRequestAtomicReservationFixture(t, 700)

	type callResult struct {
		value reservation.ReservationResult
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
			value, callErr := fixture.store.Reserve(
				t.Context(),
				fixture.input,
			)
			results <- callResult{
				value: value,
				err:   callErr,
			}
		}()
	}

	ready.Wait()
	close(start)

	created := 0
	replayed := 0
	for range 2 {
		call := <-results
		if call.err != nil {
			t.Fatalf("concurrent reserve error: %v", call.err)
		}
		switch call.value.Disposition {
		case reservation.ReservationDispositionCreated:
			created++
		case reservation.ReservationDispositionAlreadyReserved:
			replayed++
		default:
			t.Fatalf(
				"unexpected disposition %q",
				call.value.Disposition,
			)
		}
	}
	if created != 1 || replayed != 1 {
		t.Fatalf(
			"created = %d, replayed = %d, want 1 and 1",
			created,
			replayed,
		)
	}

	fixture.assertResellerReservedCents(t, 800)
	fixture.assertUsageCount(t, 1)
}

func TestLLMRequestAtomicReservationInsufficientBalanceRollsBack(
	t *testing.T,
) {
	fixture := newLLMRequestAtomicReservationFixture(t, 701)

	_, err := fixture.store.Reserve(
		t.Context(),
		fixture.input,
	)
	if !errors.Is(
		err,
		reservation.ErrResellerReserveUnavailable,
	) {
		t.Fatalf(
			"error = %v, want reseller reserve unavailable",
			err,
		)
	}

	fixture.assertResellerReservedCents(t, 100)
	fixture.assertUsageCount(t, 0)
}

func TestLLMRequestAtomicReservationInsertFailureRollsBack(
	t *testing.T,
) {
	fixture := newLLMRequestAtomicReservationFixture(t, 700)
	fixture.input.Principal.APIKeyID = "missing-api-key"

	_, err := fixture.store.Reserve(
		t.Context(),
		fixture.input,
	)
	if !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("error = %v, want store conflict", err)
	}

	fixture.assertResellerReservedCents(t, 100)
	fixture.assertUsageCount(t, 0)
}

func TestLLMRequestAtomicReservationIdempotencyConflict(
	t *testing.T,
) {
	fixture := newLLMRequestAtomicReservationFixture(t, 700)

	if _, err := fixture.store.Reserve(
		t.Context(),
		fixture.input,
	); err != nil {
		t.Fatalf("first Reserve: %v", err)
	}

	conflicting := fixture.input
	conflicting.LocalRequestID += "_other"

	_, err := fixture.store.Reserve(
		t.Context(),
		conflicting,
	)
	if !errors.Is(err, reservation.ErrRequestInProgress) {
		t.Fatalf("error = %v, want request in progress", err)
	}

	fixture.assertResellerReservedCents(t, 800)
	fixture.assertUsageCount(t, 1)
}

func TestLLMRequestAtomicReservationLocalRequestConflict(
	t *testing.T,
) {
	fixture := newLLMRequestAtomicReservationFixture(t, 700)

	if _, err := fixture.store.Reserve(
		t.Context(),
		fixture.input,
	); err != nil {
		t.Fatalf("first Reserve: %v", err)
	}

	conflicting := fixture.input
	conflicting.EstimatedClientAmountCents++

	_, err := fixture.store.Reserve(
		t.Context(),
		conflicting,
	)
	if !errors.Is(err, reservation.ErrLocalRequestConflict) {
		t.Fatalf("error = %v, want local request conflict", err)
	}

	fixture.assertResellerReservedCents(t, 800)
	fixture.assertUsageCount(t, 1)
}

type llmRequestAtomicReservationFixture struct {
	db         *DB
	store      *LLMRequestAtomicReservation
	input      reservation.ReservationInput
	userID     string
	apiKeyID   string
	resellerID string
	routeID    string
}

func newLLMRequestAtomicReservationFixture(
	t *testing.T,
	estimatedUpstreamCostCents int64,
) llmRequestAtomicReservationFixture {
	t.Helper()

	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := "atomic-reservation-user-" + suffix
	apiKeyID := "atomic-reservation-key-" + suffix
	resellerID := "atomic-reservation-reseller-" + suffix
	routeID := "atomic-reservation-route-" + suffix
	model := "atomic-reservation-model-" + suffix
	localRequestID := "llmreq_atomic_" + suffix
	idempotencyKey := "atomic-reservation-idem-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	store, err := NewLLMRequestAtomicReservation(
		db,
		llmRequestAtomicReservationClock{now: now},
	)
	if err != nil {
		t.Fatalf("NewLLMRequestAtomicReservation: %v", err)
	}

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
				apiKeyID,
			},
			{
				"DELETE FROM tokenio_users WHERE id = $1",
				userID,
			},
		} {
			_, _ = db.Exec(
				cleanupCtx,
				statement.sql,
				statement.arg,
			)
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
VALUES ($1, $2, 'atomic-reservation', $3, 'sk_test', $4, $4)
`,
		apiKeyID,
		userID,
		"atomic-reservation-hash-"+suffix,
		now,
	); err != nil {
		t.Fatalf("insert API key: %v", err)
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
    enabled,
    balance_cents,
    reserved_cents,
    minimum_balance_cents,
    created_at,
    updated_at
)
VALUES (
    $1,
    'atomic-reservation',
    'openai',
    'https://example.test',
    $2,
    TRUE,
    1000,
    100,
    200,
    $3,
    $3
)
`,
		resellerID,
		"ATOMIC_RESERVATION_KEY_"+suffix,
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
    enabled,
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
    TRUE,
    1024,
    '{"chat":true}'::jsonb,
    $4,
    $4
)
`,
		routeID,
		resellerID,
		model,
		now,
	); err != nil {
		t.Fatalf("insert route: %v", err)
	}

	return llmRequestAtomicReservationFixture{
		db:         db,
		store:      store,
		userID:     userID,
		apiKeyID:   apiKeyID,
		resellerID: resellerID,
		routeID:    routeID,
		input: reservation.ReservationInput{
			LocalRequestID: localRequestID,
			IdempotencyKey: &idempotencyKey,
			Principal: reservation.Principal{
				UserID:               userID,
				APIKeyID:             apiKeyID,
				BillingSubjectUserID: "billing-" + suffix,
			},
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			ClientModel:  model,
			BillingModel: "openai:" + model,
			Route: domain.Route{
				ID:            routeID,
				ResellerID:    resellerID,
				ProviderType:  domain.ProviderOpenAI,
				APIFamily:     domain.APIFamilyOpenAICompatible,
				EndpointKind:  domain.EndpointChat,
				ClientModel:   model,
				ProviderModel: model,
				Enabled:       true,
			},
			Reseller: domain.Reseller{
				ID:           resellerID,
				ProviderType: domain.ProviderOpenAI,
				Enabled:      true,
			},
			EstimatedUsage: domain.TokenUsage{
				InputTokens:  10,
				OutputTokens: 5,
			},
			EstimatedClientAmountCents: 100,
			EstimatedUpstreamCostCents: estimatedUpstreamCostCents,
			Currency:                   "RUB",
		},
	}
}

func (fixture llmRequestAtomicReservationFixture) assertResellerReservedCents(
	t *testing.T,
	want int64,
) {
	t.Helper()

	var got int64
	if err := fixture.db.QueryRow(
		t.Context(),
		`
SELECT reserved_cents
FROM tokenio_resellers
WHERE id = $1
`,
		fixture.resellerID,
	).Scan(&got); err != nil {
		t.Fatalf("load reseller reserved cents: %v", err)
	}
	if got != want {
		t.Fatalf("reserved_cents = %d, want %d", got, want)
	}
}

func (fixture llmRequestAtomicReservationFixture) assertUsageCount(
	t *testing.T,
	want int,
) {
	t.Helper()

	var got int
	if err := fixture.db.QueryRow(
		t.Context(),
		`
SELECT COUNT(*)
FROM tokenio_usage_records
WHERE user_id = $1
`,
		fixture.userID,
	).Scan(&got); err != nil {
		t.Fatalf("count usage records: %v", err)
	}
	if got != want {
		t.Fatalf("usage count = %d, want %d", got, want)
	}
}
