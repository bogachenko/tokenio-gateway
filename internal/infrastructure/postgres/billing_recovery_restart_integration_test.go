package postgres

import (
	"context"
	"os"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	billingrecovery "github.com/bogachenko/tokenio-gateway/internal/worker/billingrecovery"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type billingRecoveryIntegrationIdentity struct{}

func (billingRecoveryIntegrationIdentity) TokenForSubject(
	context.Context,
	string,
) (string, error) {
	return "unused-recovery-token", nil
}

type billingRecoveryIntegrationBalance struct{}

func (billingRecoveryIntegrationBalance) GetBalance(
	context.Context,
	string,
) (ports.BillingBalance, error) {
	return ports.BillingBalance{
		Currency:     "RUB",
		BalanceCents: 1000,
	}, nil
}

type billingRecoveryIntegrationCharge struct {
	mu       sync.Mutex
	requests []ports.BillingChargeRequest
	balance  int64
}

func (f *billingRecoveryIntegrationCharge) Charge(
	_ context.Context,
	request ports.BillingChargeRequest,
) (ports.BillingChargeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.requests = append(f.requests, request)
	balance := f.balance
	return ports.BillingChargeResult{
		BalanceCents: &balance,
	}, nil
}

func (f *billingRecoveryIntegrationCharge) Requests() []ports.BillingChargeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]ports.BillingChargeRequest(nil), f.requests...)
}

type billingRecoveryIntegrationClock struct {
	now time.Time
}

func (c billingRecoveryIntegrationClock) Now() time.Time {
	return c.now
}

type billingRecoveryIntegrationObserver struct {
	cycles chan billingrecovery.Cycle
}

func (o billingRecoveryIntegrationObserver) ObserveBillingRecoveryCycle(
	cycle billingrecovery.Cycle,
) {
	o.cycles <- cycle
}

func TestBillingRecoveryWorkerRestartSafeIntegration(t *testing.T) {
	dsn := os.Getenv("TOKENIO_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TOKENIO_TEST_DATABASE_DSN is not set")
	}

	for _, initialStatus := range []domain.BillingChargeStatus{
		domain.BillingChargeStatusPending,
		domain.BillingChargeStatusFailed,
	} {
		t.Run(string(initialStatus), func(t *testing.T) {
			runBillingRecoveryRestartScenario(t, dsn, initialStatus)
		})
	}
}

func runBillingRecoveryRestartScenario(
	t *testing.T,
	dsn string,
	initialStatus domain.BillingChargeStatus,
) {
	t.Helper()

	ctx := t.Context()
	openIsolatedDB, cleanupSchema := isolatedBillingRecoverySchema(t, dsn)
	defer cleanupSchema()

	firstDB, err := openIsolatedDB(ctx)
	if err != nil {
		t.Fatalf("open first DB: %v", err)
	}
	if err := firstDB.ApplyMigrations(ctx); err != nil {
		firstDB.Close()
		t.Fatalf("apply migrations: %v", err)
	}

	firstLedger, err := NewUsageLedger(firstDB)
	if err != nil {
		firstDB.Close()
		t.Fatalf("new first usage ledger: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	statusCode := "p"
	if initialStatus == domain.BillingChargeStatusFailed {
		statusCode = "f"
	}
	prefix := "br" + statusCode + suffix
	userID := prefix + "u"
	keyID := prefix + "k"
	resellerID := prefix + "s"
	routeID := prefix + "r"
	requestID := "llmreq_" + suffix
	model := prefix + "m"
	billingSubjectUserID := prefix + "b"
	now := time.Now().UTC().Truncate(time.Microsecond)

	insertChargeTestRegistry(
		t,
		firstDB,
		userID,
		keyID,
		resellerID,
		routeID,
		model,
		suffix,
		now,
	)

	reservedAt := now.Add(-time.Minute)
	billableAt := now.Add(-time.Second)
	record := domain.UsageRecord{
		LocalRequestID:             requestID,
		UserID:                     userID,
		APIKeyID:                   keyID,
		APIFamily:                  domain.APIFamilyOpenAICompatible,
		EndpointKind:               domain.EndpointChat,
		ClientModel:                model,
		BillingModel:               "openai:" + model,
		SelectedRouteID:            routeID,
		SelectedResellerID:         resellerID,
		ProviderType:               domain.ProviderOpenAI,
		ProviderModel:              model,
		EstimatedUsage:             domain.TokenUsage{InputTokens: 10, OutputTokens: 5},
		Usage:                      domain.TokenUsage{InputTokens: 8, OutputTokens: 4},
		EstimatedClientAmountCents: 100,
		EstimatedUpstreamCostCents: 40,
		ClientAmountCents:          90,
		RemainingAmountCents:       90,
		ActualUpstreamCostCents:    35,
		Currency:                   "RUB",
		UsageCompleteness:          "detailed",
		Status:                     domain.UsageStatusBillable,
		CreatedAt:                  now.Add(-time.Hour),
		ReservedAt:                 &reservedAt,
		BillableAt:                 &billableAt,
		UpdatedAt:                  billableAt,
	}
	if _, err := firstDB.Exec(
		ctx,
		insertUsageRecordSQL,
		usageRecordNamedArgs(record),
	); err != nil {
		firstDB.Close()
		t.Fatalf("insert usage: %v", err)
	}

	plan, err := billingapp.BuildChargePlan(
		billingSubjectUserID,
		[]domain.UsageRecord{record},
		90,
		now,
	)
	if err != nil {
		firstDB.Close()
		t.Fatalf("build canonical charge plan: %v", err)
	}
	batch := plan.Batch
	batchID := batch.ID
	prepared, err := firstLedger.PrepareChargeBatch(ctx, plan)
	if err != nil {
		firstDB.Close()
		t.Fatalf("prepare charge batch: %v", err)
	}

	if initialStatus == domain.BillingChargeStatusFailed {
		if err := firstLedger.MarkChargeBatchFailed(
			ctx,
			batchID,
			domain.BillingChargeStatusPending,
			"billing_unavailable",
			now.Add(time.Second),
		); err != nil {
			firstDB.Close()
			t.Fatalf("mark charge batch failed: %v", err)
		}
	}

	firstDB.Close()

	secondDB, err := openIsolatedDB(ctx)
	if err != nil {
		t.Fatalf("open second DB: %v", err)
	}
	defer secondDB.Close()

	secondLedger, err := NewUsageLedger(secondDB)
	if err != nil {
		t.Fatalf("new second usage ledger: %v", err)
	}
	charge := &billingRecoveryIntegrationCharge{balance: 910}
	clock := billingRecoveryIntegrationClock{
		now: now.Add(2 * time.Second),
	}
	autoCharge, err := billingapp.NewAutoChargeService(
		billingRecoveryIntegrationIdentity{},
		billingRecoveryIntegrationBalance{},
		charge,
		secondLedger,
		clock,
		billingapp.AutoChargeConfig{
			ThresholdCents:     1,
			MinimumChargeCents: 1,
		},
	)
	if err != nil {
		t.Fatalf("new auto-charge service: %v", err)
	}
	recovery, err := billingapp.NewRecoveryService(
		secondLedger,
		autoCharge,
	)
	if err != nil {
		t.Fatalf("new recovery service: %v", err)
	}

	observer := billingRecoveryIntegrationObserver{
		cycles: make(chan billingrecovery.Cycle, 1),
	}
	worker, err := billingrecovery.New(
		recovery,
		observer,
		time.Hour,
		10,
	)
	if err != nil {
		t.Fatalf("new recovery worker: %v", err)
	}

	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- worker.Run(workerCtx)
	}()

	var cycle billingrecovery.Cycle
	select {
	case cycle = <-observer.cycles:
		cancel()
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("immediate recovery cycle was not observed")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery worker did not stop")
	}

	if cycle.Err != nil {
		t.Fatalf("recovery cycle error: %v", cycle.Err)
	}
	if !reflect.DeepEqual(
		cycle.Result.DiscoveredBatchIDs,
		[]string{batchID},
	) {
		t.Fatalf(
			"discovered batch IDs=%v, want [%s]",
			cycle.Result.DiscoveredBatchIDs,
			batchID,
		)
	}
	if !reflect.DeepEqual(
		cycle.Result.ProcessedBatchIDs,
		[]string{batchID},
	) {
		t.Fatalf(
			"processed batch IDs=%v, want [%s]",
			cycle.Result.ProcessedBatchIDs,
			batchID,
		)
	}

	requests := charge.Requests()
	if len(requests) != 1 {
		t.Fatalf("billing requests=%d, want 1: %+v", len(requests), requests)
	}
	wantRequest := ports.BillingChargeRequest{
		RequestID:    batchID,
		UserID:       billingSubjectUserID,
		Model:        batch.BillingModel,
		InputTokens:  batch.InputTokens,
		OutputTokens: batch.OutputTokens,
		AmountCents:  batch.AmountCents,
		Currency:     batch.Currency,
	}
	if requests[0] != wantRequest {
		t.Fatalf("billing request=%+v, want %+v", requests[0], wantRequest)
	}

	completed, err := loadBillingChargeSnapshot(ctx, secondDB, batchID, false)
	if err != nil {
		t.Fatalf("load completed batch: %v", err)
	}
	if completed.Batch.Status != domain.BillingChargeStatusSucceeded {
		t.Fatalf("completed batch status=%s", completed.Batch.Status)
	}
	if completed.Batch.ID != prepared.Batch.ID ||
		!reflect.DeepEqual(completed.Allocations, prepared.Allocations) ||
		!reflect.DeepEqual(completed.ExpectedRecords, prepared.ExpectedRecords) {
		t.Fatalf(
			"completed immutable command mismatch\ncompleted=%+v\nprepared=%+v",
			completed,
			prepared,
		)
	}

	found, err := secondLedger.FindByLocalRequestID(ctx, requestID)
	if err != nil {
		t.Fatalf("find charged usage: %v", err)
	}
	if found.Status != domain.UsageStatusCharged ||
		found.BillingChargeRequestID != batchID ||
		found.ChargedAmountCents != 90 ||
		found.RemainingAmountCents != 0 {
		t.Fatalf("charged usage=%+v", found)
	}

	open, err := secondLedger.ListOpenChargeBatchesForRecovery(ctx, 10)
	if err != nil {
		t.Fatalf("list recovery batches after success: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("recovery discovery after success=%+v, want empty", open)
	}

	var commandCount int
	if err := secondDB.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM tokenio_billing_charge_batches
		 WHERE user_id = $1`,
		userID,
	).Scan(&commandCount); err != nil {
		t.Fatalf("count billing commands: %v", err)
	}
	if commandCount != 1 {
		t.Fatalf("billing command count=%d, want 1", commandCount)
	}
}

func isolatedBillingRecoverySchema(
	t *testing.T,
	dsn string,
) (func(context.Context) (*DB, error), func()) {
	t.Helper()

	ctx := t.Context()
	adminDB, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open schema administrator DB: %v", err)
	}

	schema := "tokenio_billing_recovery_" +
		strconv.FormatInt(time.Now().UnixNano(), 10)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := adminDB.Exec(
		ctx,
		"CREATE SCHEMA "+identifier,
	); err != nil {
		adminDB.Close()
		t.Fatalf("create isolated schema: %v", err)
	}

	openIsolatedDB := func(openCtx context.Context) (*DB, error) {
		config, err := poolConfig(dsn)
		if err != nil {
			return nil, err
		}
		config.ConnConfig.RuntimeParams["search_path"] = schema

		pool, err := pgxpool.NewWithConfig(openCtx, config)
		if err != nil {
			return nil, NormalizeError(err)
		}
		db := &DB{pool: pool}
		if err := db.Ping(openCtx); err != nil {
			pool.Close()
			return nil, err
		}
		return db, nil
	}

	cleanup := func() {
		if _, err := adminDB.Exec(
			context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE",
		); err != nil {
			t.Logf("drop isolated schema %s: %v", schema, err)
		}
		adminDB.Close()
	}
	return openIsolatedDB, cleanup
}
