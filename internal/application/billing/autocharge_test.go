package billing

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type testClock struct{ t time.Time }

func (c testClock) Now() time.Time { return c.t }

type fakeIdentity struct {
	token    string
	err      error
	subjects []string
}

func (f *fakeIdentity) TokenForSubject(ctx context.Context, subject string) (string, error) {
	f.subjects = append(f.subjects, subject)
	if f.err != nil {
		return "", f.err
	}
	return f.token, nil
}

type fakeBalance struct {
	balance ports.BillingBalance
	err     error
	tokens  []string
}

func (f *fakeBalance) GetBalance(ctx context.Context, token string) (ports.BillingBalance, error) {
	f.tokens = append(f.tokens, token)
	if f.err != nil {
		return ports.BillingBalance{}, f.err
	}
	return f.balance, nil
}

type fakeCharge struct {
	err      error
	result   ports.BillingChargeResult
	results  []ports.BillingChargeResult
	requests []ports.BillingChargeRequest
}

func (f *fakeCharge) Charge(ctx context.Context, req ports.BillingChargeRequest) (ports.BillingChargeResult, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return ports.BillingChargeResult{}, f.err
	}
	if len(f.results) > 0 {
		result := f.results[0]
		f.results = f.results[1:]
		return result, nil
	}
	return f.result, nil
}

type fakeUsageLedger struct {
	mu                  sync.Mutex
	exposure            ports.UsageExposureSnapshot
	exposureErr         error
	candidates          []domain.UsageRecord
	open                []ports.BillingChargeBatchSnapshot
	batches             map[string]ports.BillingChargeBatchSnapshot
	records             map[string]domain.UsageRecord
	prepareCalls        int
	markFailedCalls     int
	applyCalls          int
	applyErr            error
	markErr             error
	prepareBeforeCharge *bool
	chargeCalls         *int
}

func newFakeLedger(records []domain.UsageRecord) *fakeUsageLedger {
	m := make(map[string]domain.UsageRecord)
	for _, r := range records {
		m[r.LocalRequestID] = r
	}
	return &fakeUsageLedger{candidates: append([]domain.UsageRecord(nil), records...), records: m, batches: make(map[string]ports.BillingChargeBatchSnapshot)}
}
func (f *fakeUsageLedger) CreateReserved(ctx context.Context, record domain.UsageRecord) (ports.UsageReserveResult, error) {
	return ports.UsageReserveResult{}, nil
}
func (f *fakeUsageLedger) FindByLocalRequestID(ctx context.Context, id string) (*domain.UsageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.records[id]
	return &r, nil
}
func (f *fakeUsageLedger) CompareAndSwap(ctx context.Context, id string, st domain.UsageStatus, next domain.UsageRecord) (ports.UsageTransitionResult, error) {
	return ports.UsageTransitionResult{}, nil
}
func (f *fakeUsageLedger) LoadExposure(ctx context.Context, userID string, currency string) (ports.UsageExposureSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exposure, f.exposureErr
}
func (f *fakeUsageLedger) LoadOpenChargeBatches(ctx context.Context, userID string, subject string, currency string) ([]ports.BillingChargeBatchSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ports.BillingChargeBatchSnapshot(nil), f.open...), nil
}
func (f *fakeUsageLedger) LoadChargeCandidates(ctx context.Context, userID string, currency string) ([]domain.UsageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.UsageRecord, 0, len(f.records))
	for _, record := range f.records {
		if record.UserID != userID || record.Currency != currency || record.RemainingAmountCents <= 0 {
			continue
		}
		switch record.Status {
		case domain.UsageStatusBillable:
			if record.BillingChargeRequestID == "" {
				out = append(out, record)
			}
		case domain.UsageStatusPartiallyCharged:
			if f.batchSucceededLocked(record.BillingChargeRequestID) {
				out = append(out, record)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LocalRequestID < out[j].LocalRequestID })
	return out, nil
}
func (f *fakeUsageLedger) PrepareChargeBatch(ctx context.Context, plan ports.UsageChargeBatchPlan) (ports.BillingChargeBatchSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepareCalls++
	if f.prepareBeforeCharge != nil && f.chargeCalls != nil {
		v := *f.chargeCalls == 0
		*f.prepareBeforeCharge = v
	}
	if existing, ok := f.batches[plan.Batch.ID]; ok {
		if existing.Batch != plan.Batch || !reflect.DeepEqual(existing.Allocations, plan.Allocations) || !reflect.DeepEqual(existing.ExpectedRecords, claimedExpected(plan)) {
			return ports.BillingChargeBatchSnapshot{}, errors.New("idempotency payload mismatch")
		}
		return existing, nil
	}
	for _, exp := range plan.ExpectedRecords {
		cur := f.records[exp.LocalRequestID]
		if cur != exp {
			return ports.BillingChargeBatchSnapshot{}, errors.New("expectation mismatch")
		}
		if cur.Status == domain.UsageStatusPartiallyCharged && !f.batchSucceededLocked(cur.BillingChargeRequestID) {
			return ports.BillingChargeBatchSnapshot{}, errors.New("active partial claim")
		}
	}
	claimedExpected := make([]domain.UsageRecord, 0, len(plan.ExpectedRecords))
	for _, exp := range plan.ExpectedRecords {
		cur := f.records[exp.LocalRequestID]
		cur.BillingChargeRequestID = plan.Batch.ID
		f.records[exp.LocalRequestID] = cur
		claimedExpected = append(claimedExpected, cur)
	}
	snap := ports.BillingChargeBatchSnapshot{Batch: plan.Batch, Allocations: append([]domain.BillingChargeAllocation(nil), plan.Allocations...), ExpectedRecords: claimedExpected}
	f.batches[plan.Batch.ID] = snap
	return snap, nil
}
func (f *fakeUsageLedger) batchSucceededLocked(batchID string) bool {
	if batchID == "" {
		return false
	}
	if snap, ok := f.batches[batchID]; ok {
		return snap.Batch.Status == domain.BillingChargeStatusSucceeded
	}
	for _, snap := range f.open {
		if snap.Batch.ID == batchID {
			return snap.Batch.Status == domain.BillingChargeStatusSucceeded
		}
	}
	return false
}

func (f *fakeUsageLedger) seedSucceededBatchForRecord(record domain.UsageRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if record.BillingChargeRequestID == "" {
		return
	}
	now := time.Unix(1, 0).UTC()
	f.batches[record.BillingChargeRequestID] = ports.BillingChargeBatchSnapshot{Batch: domain.BillingChargeBatch{ID: record.BillingChargeRequestID, UserID: record.UserID, BillingSubjectUserID: "billing", ProviderType: record.ProviderType, ClientModel: record.ClientModel, BillingModel: record.BillingModel, InputTokens: 1, OutputTokens: 0, AmountCents: record.ChargedAmountCents, Currency: record.Currency, Status: domain.BillingChargeStatusSucceeded, CreatedAt: now, ChargedAt: &now, UpdatedAt: now}}
}

func (f *fakeUsageLedger) MarkChargeBatchFailed(ctx context.Context, batchID string, expectedStatus domain.BillingChargeStatus, code string, failedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markFailedCalls++
	if f.markErr != nil {
		return f.markErr
	}
	snap, ok := f.batches[batchID]
	if !ok {
		for _, open := range f.open {
			if open.Batch.ID == batchID {
				snap = open
				ok = true
				break
			}
		}
	}
	if !ok {
		return errors.New("missing batch")
	}
	if snap.Batch.Status == domain.BillingChargeStatusSucceeded {
		return nil
	}
	if snap.Batch.Status != expectedStatus {
		return errors.New("status mismatch")
	}
	if snap.Batch.Status != domain.BillingChargeStatusPending {
		return nil
	}
	failedAt = failedAt.UTC()
	snap.Batch.Status = domain.BillingChargeStatusFailed
	snap.Batch.BillingErrorCode = code
	snap.Batch.FailedAt = &failedAt
	snap.Batch.UpdatedAt = failedAt
	f.batches[batchID] = snap
	return nil
}
func (f *fakeUsageLedger) ApplyChargeSuccess(ctx context.Context, success ports.UsageChargeSuccess) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applyCalls++
	if f.applyErr != nil {
		return f.applyErr
	}
	snap, ok := f.batches[success.BatchID]
	if !ok {
		for _, o := range f.open {
			if o.Batch.ID == success.BatchID {
				snap = o
				ok = true
			}
		}
	}
	if !ok {
		return errors.New("missing batch")
	}
	if len(success.ExpectedRecords) == 0 {
		return errors.New("missing expected records")
	}
	if !reflect.DeepEqual(success.Allocations, snap.Allocations) || !reflect.DeepEqual(success.ExpectedRecords, snap.ExpectedRecords) {
		return errors.New("success command mismatch")
	}
	if snap.Batch.Status == domain.BillingChargeStatusSucceeded {
		return nil
	}
	if snap.Batch.Status != domain.BillingChargeStatusPending && snap.Batch.Status != domain.BillingChargeStatusFailed {
		return errors.New("status mismatch")
	}
	expected := make(map[string]domain.UsageRecord, len(snap.ExpectedRecords))
	for _, record := range snap.ExpectedRecords {
		expected[record.LocalRequestID] = record
	}
	success.ChargedAt = success.ChargedAt.UTC()
	for _, alloc := range snap.Allocations {
		r := f.records[alloc.LocalRequestID]
		exp, ok := expected[alloc.LocalRequestID]
		if !ok || r != exp || r.BillingChargeRequestID != success.BatchID || (r.Status != domain.UsageStatusBillable && r.Status != domain.UsageStatusPartiallyCharged) || alloc.RemainingAmountCents != r.RemainingAmountCents-alloc.ChargedAmountCents {
			return errors.New("expectation mismatch")
		}
		r.ChargedAmountCents += alloc.ChargedAmountCents
		r.RemainingAmountCents = alloc.RemainingAmountCents
		r.BillingChargeRequestID = success.BatchID
		r.ChargedAt = &success.ChargedAt
		if r.RemainingAmountCents == 0 {
			r.Status = domain.UsageStatusCharged
		} else {
			r.Status = domain.UsageStatusPartiallyCharged
		}
		if r.ChargedAmountCents+r.RemainingAmountCents != r.ClientAmountCents {
			return errors.New("bad invariant")
		}
		f.records[alloc.LocalRequestID] = r
		for i := range f.candidates {
			if f.candidates[i].LocalRequestID == alloc.LocalRequestID {
				f.candidates[i] = r
			}
		}
	}
	snap.Batch.Status = domain.BillingChargeStatusSucceeded
	snap.Batch.ChargedAt = &success.ChargedAt
	snap.Batch.FailedAt = nil
	snap.Batch.BillingErrorCode = ""
	snap.Batch.BillingResponseBalanceCents = success.BillingBalanceCents
	snap.Batch.UpdatedAt = success.ChargedAt
	f.batches[success.BatchID] = snap
	return nil
}

func TestBalanceAdmissionAndPricingFailedBlock(t *testing.T) {
	id := &fakeIdentity{token: "jwt"}
	bal := &fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 1000}}
	fl := newFakeLedger(nil)
	fl.exposure = ports.UsageExposureSnapshot{Currency: "RUB", BillableRemainingAmountCents: 200}
	svc, _ := NewAdmissionService(id, bal, fl, AdmissionConfig{})
	res, err := svc.Admit(t.Context(), AdmissionInput{UserID: "local", BillingSubjectUserID: "billing", RequiredReserveCents: 500})
	if err != nil || !res.Allowed || res.EffectiveBalanceCents != 800 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if id.subjects[0] != "billing" || bal.tokens[0] != "jwt" {
		t.Fatalf("identity boundary failed")
	}
	fl.exposure.PricingFailedCount = 1
	_, err = svc.Admit(t.Context(), AdmissionInput{UserID: "local", BillingSubjectUserID: "billing"})
	if !errors.Is(err, domain.ErrUnresolvedUsage) {
		t.Fatalf("err=%v", err)
	}
}

func TestAdmissionEnforcesMinimumRequestBalance(t *testing.T) {
	identity := &fakeIdentity{token: "jwt"}
	balance := &fakeBalance{
		balance: ports.BillingBalance{
			Currency:     "RUB",
			BalanceCents: 1000,
		},
	}
	ledger := newFakeLedger(nil)
	ledger.exposure = ports.UsageExposureSnapshot{
		Currency:                     "RUB",
		BillableRemainingAmountCents: 200,
	}

	service, err := NewAdmissionService(
		identity,
		balance,
		ledger,
		AdmissionConfig{MinimumRequestBalanceCents: 900},
	)
	if err != nil {
		t.Fatalf("NewAdmissionService: %v", err)
	}

	result, err := service.Admit(
		t.Context(),
		AdmissionInput{
			UserID:               "local",
			BillingSubjectUserID: "billing",
			RequiredReserveCents: 500,
		},
	)
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("err=%v result=%+v", err, result)
	}
	if result.Allowed ||
		result.EffectiveBalanceCents != 800 ||
		result.RequiredReserveCents != 500 {
		t.Fatalf("result=%+v", result)
	}

	service, err = NewAdmissionService(
		identity,
		balance,
		ledger,
		AdmissionConfig{MinimumRequestBalanceCents: 700},
	)
	if err != nil {
		t.Fatalf("NewAdmissionService: %v", err)
	}
	result, err = service.Admit(
		t.Context(),
		AdmissionInput{
			UserID:               "local",
			BillingSubjectUserID: "billing",
			RequiredReserveCents: 500,
		},
	)
	if err != nil || !result.Allowed {
		t.Fatalf("err=%v result=%+v", err, result)
	}

	if _, err := NewAdmissionService(
		identity,
		balance,
		ledger,
		AdmissionConfig{MinimumRequestBalanceCents: -1},
	); !errors.Is(err, ErrInvalidBillingInput) {
		t.Fatalf("negative config err=%v", err)
	}
}

func TestGroupingOrderingThresholdAndAllocation(t *testing.T) {
	recs := []domain.UsageRecord{rec("b", domain.ProviderOpenAI, "z", 20, 0, 20, 2), rec("a", domain.ProviderAnthropic, "a", 50, 0, 50, 1), rec("c", domain.ProviderAnthropic, "a", 100, 20, 80, 3)}
	orig := append([]domain.UsageRecord(nil), recs...)
	groups, err := BuildChargeGroups("u", "RUB", recs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recs, orig) {
		t.Fatal("mutated input")
	}
	if len(groups) != 2 || groups[0].key.ProviderType != domain.ProviderAnthropic || groups[0].records[0].LocalRequestID != "llmreq_a" {
		t.Fatalf("groups=%+v", groups)
	}
	plan, err := BuildChargePlan("billing", groups[0].records, 90, time.Unix(10, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Allocations) != 2 || plan.Allocations[0].ChargedAmountCents != 50 || plan.Allocations[1].ChargedAmountCents != 40 || plan.Allocations[1].RemainingAmountCents != 40 {
		t.Fatalf("allocs=%+v", plan.Allocations)
	}
	if plan.Batch.AmountCents != 90 || plan.Batch.BillingModel != "anthropic:a" {
		t.Fatalf("batch=%+v", plan.Batch)
	}
}

func TestPartialChargeClaimsOnlyAllocatedRecordsAndPartialCanBeCandidate(t *testing.T) {
	records := []domain.UsageRecord{rec("claim-a", domain.ProviderOpenAI, "m", 100, 0, 100, 1), rec("claim-b", domain.ProviderOpenAI, "m", 100, 0, 100, 2)}
	plan, err := BuildChargePlan("billing", records, 50, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Allocations) != 1 || len(plan.ExpectedRecords) != 1 || plan.ExpectedRecords[0].LocalRequestID != "llmreq_claim-a" {
		t.Fatalf("allocations=%+v expected=%+v", plan.Allocations, plan.ExpectedRecords)
	}
	partial := rec("partial-candidate", domain.ProviderOpenAI, "m", 100, 40, 60, 1)
	if partial.BillingChargeRequestID == "" {
		t.Fatal("test helper produced invalid partial")
	}
	groups, err := BuildChargeGroups("u", "RUB", []domain.UsageRecord{partial})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].pending != 60 {
		t.Fatalf("groups=%+v", groups)
	}
}

func TestProcessPreparedBatchTreatsSucceededPrepareReplayAsIdempotentSuccess(t *testing.T) {
	record := rec("succeeded-prepare-replay", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{record}, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}

	chargedAt := time.Unix(2, 0).UTC()
	balance := int64(900)
	snapshot := ports.BillingChargeBatchSnapshot{
		Batch:           plan.Batch,
		Allocations:     plan.Allocations,
		ExpectedRecords: claimedExpected(plan),
	}
	snapshot.Batch.Status = domain.BillingChargeStatusSucceeded
	snapshot.Batch.BillingResponseBalanceCents = &balance
	snapshot.Batch.ChargedAt = &chargedAt
	snapshot.Batch.UpdatedAt = chargedAt

	charge := &fakeCharge{}
	usageLedger := newFakeLedger(nil)
	service, err := NewAutoChargeService(
		&fakeIdentity{token: "jwt"},
		&fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 1000}},
		charge,
		usageLedger,
		testClock{chargedAt},
		AutoChargeConfig{ThresholdCents: 100, MinimumChargeCents: 1},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.processPreparedBatch(
		t.Context(),
		AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing", Currency: "RUB"},
		snapshot,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(charge.requests) != 0 {
		t.Fatalf("succeeded replay called Billing: %+v", charge.requests)
	}
	if usageLedger.applyCalls != 0 || usageLedger.markFailedCalls != 0 {
		t.Fatalf("succeeded replay mutated ledger: apply=%d failed=%d", usageLedger.applyCalls, usageLedger.markFailedCalls)
	}
	if result.ProcessedBatchID != snapshot.Batch.ID || len(result.ProcessedBatchIDs) != 1 || result.ProcessedBatchIDs[0] != snapshot.Batch.ID {
		t.Fatalf("processed result=%+v", result)
	}
	if result.ChargedAmountCents != snapshot.Batch.AmountCents || !result.UsedBillingBalanceCents || result.BillingBalanceCents == nil || *result.BillingBalanceCents != balance {
		t.Fatalf("billing result=%+v", result)
	}
}

func TestValidateChargeSnapshotRejectsStableIDMismatch(t *testing.T) {
	r := rec("stable-mismatch", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{r}, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := ports.BillingChargeBatchSnapshot{Batch: plan.Batch, Allocations: plan.Allocations, ExpectedRecords: claimedExpected(plan)}
	snapshot.Batch.AmountCents = 99
	if err := ValidateChargeSnapshot(AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing", Currency: "RUB"}, snapshot); !errors.Is(err, ErrInvalidChargePlan) {
		t.Fatalf("err=%v", err)
	}
}

func TestPrepareExistingBatchRejectsPayloadMismatch(t *testing.T) {
	r := rec("existing", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{r}, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	fl := newFakeLedger([]domain.UsageRecord{r})
	if _, err := fl.PrepareChargeBatch(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	changed := plan
	changed.Allocations = append([]domain.BillingChargeAllocation(nil), plan.Allocations...)
	changed.Allocations[0].ChargedAmountCents = 99
	if _, err := fl.PrepareChargeBatch(t.Context(), changed); err == nil {
		t.Fatal("expected payload mismatch")
	}
}

func TestPartialTokenAllocationStableIDs(t *testing.T) {
	r := rec("r1", domain.ProviderOpenAI, "gpt", 100, 25, 75, 1)
	r.Usage.InputTokens = 10
	r.Usage.OutputTokens = 7
	p1, err := BuildChargePlan("billing", []domain.UsageRecord{r}, 25, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if p1.Batch.InputTokens != 3 || p1.Batch.OutputTokens != 2 {
		t.Fatalf("tokens=%d/%d", p1.Batch.InputTokens, p1.Batch.OutputTokens)
	}
	p2, _ := BuildChargePlan("billing", []domain.UsageRecord{r}, 25, time.Unix(99, 0))
	if p1.Batch.ID != p2.Batch.ID || p1.Allocations[0].ID != p2.Allocations[0].ID {
		t.Fatal("ids not stable")
	}
	p3, _ := BuildChargePlan("billing", []domain.UsageRecord{r}, 30, time.Unix(1, 0).UTC())
	if p1.Batch.ID == p3.Batch.ID {
		t.Fatal("amount change did not change id")
	}
	r.ChargedAmountCents = 50
	r.RemainingAmountCents = 50
	p4, _ := BuildChargePlan("billing", []domain.UsageRecord{r}, 25, time.Unix(1, 0).UTC())
	if p1.Batch.ID == p4.Batch.ID {
		t.Fatal("previous charged change did not change id")
	}
}

func TestAutoChargeLifecycleFailureRetrySuccessAndReconciliation(t *testing.T) {
	now := time.Unix(100, 0)
	chargeCalls := 0
	preparedBefore := false
	records := []domain.UsageRecord{rec("r1", domain.ProviderOpenAI, "gpt", 100, 0, 100, 1)}
	fl := newFakeLedger(records)
	fl.prepareBeforeCharge = &preparedBefore
	fl.chargeCalls = &chargeCalls
	ch := &fakeCharge{err: errors.New("remote down")}
	id := &fakeIdentity{token: "jwt"}
	bal := &fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 1000}}
	svc, _ := NewAutoChargeService(id, bal, ch, fl, testClock{now}, AutoChargeConfig{ThresholdCents: 100, MinimumChargeCents: 1})
	_, err := svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	chargeCalls = len(ch.requests)
	if !errors.Is(err, ErrBillingUnavailable) || !preparedBefore || fl.markFailedCalls != 1 {
		t.Fatalf("err=%v prepared=%v failed=%d", err, preparedBefore, fl.markFailedCalls)
	}
	for _, r := range fl.records {
		if r.ChargedAmountCents != 0 || r.RemainingAmountCents != 100 {
			t.Fatal("usage changed on failure")
		}
	}
	var failed ports.BillingChargeBatchSnapshot
	for _, b := range fl.batches {
		failed = b
	}
	fl.open = []ports.BillingChargeBatchSnapshot{failed}
	ch.err = nil
	ch.requests = nil
	_, err = svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.requests) != 1 || ch.requests[0].RequestID != failed.Batch.ID {
		t.Fatalf("retry request=%+v failed=%s", ch.requests, failed.Batch.ID)
	}
	if fl.records["llmreq_r1"].Status != domain.UsageStatusCharged || fl.applyCalls != 1 {
		t.Fatalf("record=%+v apply=%d", fl.records["llmreq_r1"], fl.applyCalls)
	}
	fl.open = []ports.BillingChargeBatchSnapshot{failed}
	fl.applyErr = errors.New("store write failed")
	ch.requests = nil
	_, err = svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if !errors.Is(err, ErrChargeReconciliationRequired) || len(ch.requests) != 1 {
		t.Fatalf("err=%v calls=%d", err, len(ch.requests))
	}
}

func TestOpenBatchesProcessedFirstMixedGroupsAndConcurrentPrepare(t *testing.T) {
	records := []domain.UsageRecord{rec("a", domain.ProviderAnthropic, "m", 100, 0, 100, 1), rec("b", domain.ProviderOpenAI, "m", 100, 0, 100, 2)}
	groups, _ := BuildChargeGroups("u", "RUB", records)
	if len(groups) != 2 {
		t.Fatalf("groups=%d", len(groups))
	}
	plan, err := BuildChargePlan("billing", groups[0].records, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	open := ports.BillingChargeBatchSnapshot{Batch: plan.Batch, Allocations: plan.Allocations, ExpectedRecords: claimedExpected(plan)}
	fl := newFakeLedger(records)
	for _, expected := range open.ExpectedRecords {
		fl.records[expected.LocalRequestID] = expected
	}
	fl.open = []ports.BillingChargeBatchSnapshot{open}
	ch := &fakeCharge{}
	svc, _ := NewAutoChargeService(&fakeIdentity{token: "jwt"}, &fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 1000}}, ch, fl, testClock{time.Unix(1, 0).UTC()}, AutoChargeConfig{ThresholdCents: 100, MinimumChargeCents: 1})
	_, err = svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.requests) != 2 || ch.requests[0].RequestID != open.Batch.ID || fl.prepareCalls != 1 {
		t.Fatalf("open/groups not processed in order: requests=%+v prepare=%d", ch.requests, fl.prepareCalls)
	}
	fl2 := newFakeLedger(records[:1])
	t.Cleanup(func() {
		if len(fl2.batches) != 1 {
			t.Fatalf("batches=%d", len(fl2.batches))
		}
	})
	for i := 0; i < 2; i++ {
		t.Run("concurrent prepare", func(t *testing.T) {
			t.Parallel()
			_, _ = fl2.PrepareChargeBatch(t.Context(), plan)
		})
	}
}

func TestDTOsAndErrorsDoNotExposeSecretsOrRawBodies(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(AdmissionResult{}), reflect.TypeOf(AutoChargeResult{}), reflect.TypeOf(domain.BillingChargeBatch{}), reflect.TypeOf(domain.BillingChargeAllocation{})} {
		for _, forbidden := range []string{"ServiceToken", "BillingJWT", "RawRequest", "RawResponse", "RawError", "Authorization"} {
			if _, ok := typ.FieldByName(forbidden); ok {
				t.Fatalf("%s exposes %s", typ.Name(), forbidden)
			}
		}
	}
	for _, err := range []error{ErrBillingUnavailable, ErrBillingIdentityUnavailable, ErrChargeReconciliationRequired} {
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "raw") {
			t.Fatalf("bad err %v", err)
		}
	}
}

func rec(id string, provider domain.ProviderType, model string, client, charged, remaining int64, created int64) domain.UsageRecord {
	localRequestID := id
	if !strings.HasPrefix(localRequestID, "llmreq_") {
		localRequestID = "llmreq_" + localRequestID
	}
	createdAt := time.Unix(created, 0)
	status := domain.UsageStatusBillable
	record := domain.UsageRecord{LocalRequestID: localRequestID, UserID: "u", ProviderType: provider, ClientModel: model, BillingModel: string(provider) + ":" + model, ClientAmountCents: client, ChargedAmountCents: charged, RemainingAmountCents: remaining, Currency: "RUB", Status: status, UsageCompleteness: "detailed", Usage: domain.TokenUsage{InputTokens: client / 10, OutputTokens: client / 20}, CreatedAt: createdAt, BillableAt: &createdAt}
	if charged > 0 {
		record.Status = domain.UsageStatusPartiallyCharged
		record.ChargedAt = &createdAt
		record.BillingChargeRequestID = "billchg_" + strings.Repeat("1", 64)
	}
	return record
}

func TestDeterministicOrderingExplicit(t *testing.T) {
	recs := []domain.UsageRecord{rec("b", domain.ProviderOpenAI, "m", 10, 0, 10, 1), rec("a", domain.ProviderOpenAI, "m", 10, 0, 10, 1)}
	groups, _ := BuildChargeGroups("u", "RUB", recs)
	ids := []string{groups[0].records[0].LocalRequestID, groups[0].records[1].LocalRequestID}
	sort.Strings(ids)
	if groups[0].records[0].LocalRequestID != ids[0] {
		t.Fatal("not sorted by id")
	}
}

func TestInvalidCandidatesAreRejectedAndOverflowIsChecked(t *testing.T) {
	badUser := rec("bad-user", domain.ProviderOpenAI, "m", 10, 0, 10, 1)
	badUser.UserID = "other"
	if _, err := BuildChargeGroups("u", "RUB", []domain.UsageRecord{badUser}); !errors.Is(err, ErrInvalidChargePlan) {
		t.Fatalf("bad user err=%v", err)
	}
	badModel := rec("bad-model", domain.ProviderOpenAI, "m", 10, 0, 10, 1)
	badModel.BillingModel = ""
	if _, err := BuildChargeGroups("u", "RUB", []domain.UsageRecord{badModel}); !errors.Is(err, ErrInvalidChargePlan) {
		t.Fatalf("bad model err=%v", err)
	}
	badLifecycle := rec("bad-lifecycle", domain.ProviderOpenAI, "m", 10, 0, 10, 1)
	badLifecycle.BillableAt = nil
	if _, err := BuildChargeGroups("u", "RUB", []domain.UsageRecord{badLifecycle}); !errors.Is(err, domain.ErrUsageRecordCorrupt) {
		t.Fatalf("bad lifecycle err=%v", err)
	}
	overflowA := rec("overflow-a", domain.ProviderOpenAI, "m", 1<<62, 0, 1<<62, 1)
	overflowB := rec("overflow-b", domain.ProviderOpenAI, "m", 1<<62, 0, 1<<62, 2)
	if _, err := BuildChargeGroups("u", "RUB", []domain.UsageRecord{overflowA, overflowB}); !errors.Is(err, domain.ErrFinancialAmountOverflow) {
		t.Fatalf("amount overflow err=%v", err)
	}
	tokA := rec("tok-a", domain.ProviderOpenAI, "m", 10, 0, 10, 1)
	tokB := rec("tok-b", domain.ProviderOpenAI, "m", 10, 0, 10, 2)
	tokA.Usage.InputTokens = 1 << 62
	tokB.Usage.InputTokens = 1 << 62
	if _, err := BuildChargePlan("billing", []domain.UsageRecord{tokA, tokB}, 20, time.Unix(1, 0).UTC()); !errors.Is(err, ErrTokenOverflow) {
		t.Fatalf("token overflow err=%v", err)
	}
}

func TestValidateChargeSnapshotAndFailureMarkReconciliation(t *testing.T) {
	r := rec("snap", domain.ProviderOpenAI, "m", 10, 0, 10, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{r}, 10, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := ports.BillingChargeBatchSnapshot{Batch: plan.Batch, Allocations: plan.Allocations, ExpectedRecords: claimedExpected(plan)}
	if err := ValidateChargeSnapshot(AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing", Currency: "RUB"}, snapshot); err != nil {
		t.Fatalf("valid snapshot err=%v", err)
	}
	snapshot.Allocations = append(snapshot.Allocations, snapshot.Allocations[0])
	if err := ValidateChargeSnapshot(AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing", Currency: "RUB"}, snapshot); !errors.Is(err, ErrInvalidChargePlan) {
		t.Fatalf("duplicate allocation err=%v", err)
	}

	fl := newFakeLedger([]domain.UsageRecord{r})
	fl.markErr = errors.New("store down")
	ch := &fakeCharge{err: errors.New("remote uncertain")}
	svc, _ := NewAutoChargeService(&fakeIdentity{token: "jwt"}, &fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 100}}, ch, fl, testClock{time.Unix(2, 0).UTC()}, AutoChargeConfig{ThresholdCents: 10, MinimumChargeCents: 1})
	_, err = svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if !errors.Is(err, ErrChargeReconciliationRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestRemoteBalanceResultControlsNextGroupAmount(t *testing.T) {
	records := []domain.UsageRecord{rec("anthropic", domain.ProviderAnthropic, "m", 100, 0, 100, 1), rec("openai", domain.ProviderOpenAI, "m", 100, 0, 100, 2)}
	firstRemoteBalance := int64(40)
	ch := &fakeCharge{results: []ports.BillingChargeResult{{BalanceCents: &firstRemoteBalance}, {}}}
	fl := newFakeLedger(records)
	svc, _ := NewAutoChargeService(&fakeIdentity{token: "jwt"}, &fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 200}}, ch, fl, testClock{time.Unix(1, 0).UTC()}, AutoChargeConfig{ThresholdCents: 100, MinimumChargeCents: 1})
	result, err := svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ProcessedBatchIDs) != 2 || len(ch.requests) != 2 {
		t.Fatalf("processed=%+v requests=%+v", result.ProcessedBatchIDs, ch.requests)
	}
	if ch.requests[0].AmountCents != 100 || ch.requests[1].AmountCents != firstRemoteBalance {
		t.Fatalf("amounts=%d/%d", ch.requests[0].AmountCents, ch.requests[1].AmountCents)
	}
}

func TestApplyChargeSuccessRequiresExpectedRecords(t *testing.T) {
	r := rec("expect", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{r}, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	fl := newFakeLedger([]domain.UsageRecord{r})
	snapshot, err := fl.PrepareChargeBatch(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	changed := fl.records[r.LocalRequestID]
	changed.ChargedAmountCents = 1
	changed.RemainingAmountCents = 99
	fl.records[r.LocalRequestID] = changed
	err = fl.ApplyChargeSuccess(t.Context(), ports.UsageChargeSuccess{BatchID: snapshot.Batch.ID, ChargedAt: time.Unix(2, 0).UTC(), Allocations: snapshot.Allocations, ExpectedRecords: snapshot.ExpectedRecords})
	if err == nil {
		t.Fatal("expected mismatch")
	}
}

func claimedExpected(plan ports.UsageChargeBatchPlan) []domain.UsageRecord {
	out := make([]domain.UsageRecord, 0, len(plan.ExpectedRecords))
	for _, record := range plan.ExpectedRecords {
		record.BillingChargeRequestID = plan.Batch.ID
		out = append(out, record)
	}
	return out
}

func TestApplicationRejectsInvalidBillingBalanceResults(t *testing.T) {
	fl := newFakeLedger([]domain.UsageRecord{rec("invalid-balance", domain.ProviderOpenAI, "m", 10, 0, 10, 1)})
	svc, _ := NewAutoChargeService(&fakeIdentity{token: "jwt"}, &fakeBalance{balance: ports.BillingBalance{Currency: "USD", BalanceCents: 100}}, &fakeCharge{}, fl, testClock{time.Unix(1, 0).UTC()}, AutoChargeConfig{ThresholdCents: 10, MinimumChargeCents: 1})
	_, err := svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if !errors.Is(err, ErrBillingUnavailable) {
		t.Fatalf("autocharge err=%v", err)
	}
	admission, _ := NewAdmissionService(
		&fakeIdentity{token: "jwt"},
		&fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: -1}},
		fl,
		AdmissionConfig{},
	)
	_, err = admission.Admit(t.Context(), AdmissionInput{UserID: "u", BillingSubjectUserID: "billing"})
	if !errors.Is(err, ErrBillingUnavailable) {
		t.Fatalf("admission err=%v", err)
	}
}

func TestValidateChargeSnapshotRejectsTokenTotalMismatch(t *testing.T) {
	r := rec("token-mismatch", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{r}, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := ports.BillingChargeBatchSnapshot{Batch: plan.Batch, Allocations: plan.Allocations, ExpectedRecords: claimedExpected(plan)}
	snapshot.Batch.InputTokens++
	if err := ValidateChargeSnapshot(AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing", Currency: "RUB"}, snapshot); !errors.Is(err, ErrInvalidChargePlan) {
		t.Fatalf("err=%v", err)
	}
}

func TestPartialCandidateRequiresSucceededPreviousBatch(t *testing.T) {
	partial := rec("partial-active", domain.ProviderOpenAI, "m", 100, 40, 60, 1)
	fl := newFakeLedger([]domain.UsageRecord{partial})
	candidates, err := fl.LoadChargeCandidates(t.Context(), "u", "RUB")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("active partial candidate returned: %+v", candidates)
	}
	fl.seedSucceededBatchForRecord(partial)
	candidates, err = fl.LoadChargeCandidates(t.Context(), "u", "RUB")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].LocalRequestID != partial.LocalRequestID {
		t.Fatalf("candidates=%+v", candidates)
	}
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{partial}, 60, time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	flNoSucceeded := newFakeLedger([]domain.UsageRecord{partial})
	if _, err := flNoSucceeded.PrepareChargeBatch(t.Context(), plan); err == nil {
		t.Fatal("expected active partial claim rejection")
	}
}

func TestApplyChargeSuccessUsesPersistedImmutableAllocations(t *testing.T) {
	r := rec("persisted-allocation", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{r}, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	fl := newFakeLedger([]domain.UsageRecord{r})
	snapshot, err := fl.PrepareChargeBatch(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	mutatedAllocations := append([]domain.BillingChargeAllocation(nil), snapshot.Allocations...)
	mutatedAllocations[0].ChargedAmountCents = 99
	mutatedExpected := append([]domain.UsageRecord(nil), snapshot.ExpectedRecords...)
	mutatedExpected[0].RemainingAmountCents = 99
	err = fl.ApplyChargeSuccess(t.Context(), ports.UsageChargeSuccess{BatchID: snapshot.Batch.ID, ChargedAt: time.Unix(2, 0).UTC(), Allocations: mutatedAllocations, ExpectedRecords: mutatedExpected})
	if err == nil {
		t.Fatal("expected success command mismatch")
	}
}

func TestNegativeChargeResultBalanceRejectedBeforePersistence(t *testing.T) {
	r := rec("negative-charge-balance", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	negative := int64(-1)
	fl := newFakeLedger([]domain.UsageRecord{r})
	ch := &fakeCharge{result: ports.BillingChargeResult{BalanceCents: &negative}}
	svc, _ := NewAutoChargeService(&fakeIdentity{token: "jwt"}, &fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 100}}, ch, fl, testClock{time.Unix(1, 0).UTC()}, AutoChargeConfig{ThresholdCents: 100, MinimumChargeCents: 1})
	_, err := svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if !errors.Is(err, ErrChargeReconciliationRequired) {
		t.Fatalf("err=%v", err)
	}
	if fl.applyCalls != 0 {
		t.Fatalf("apply called %d times", fl.applyCalls)
	}
}

func TestOpenBatchExpectedRecordMustPassFullLedgerValidation(t *testing.T) {
	r := rec("open-corrupt", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{r}, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := ports.BillingChargeBatchSnapshot{Batch: plan.Batch, Allocations: plan.Allocations, ExpectedRecords: claimedExpected(plan)}
	snapshot.ExpectedRecords[0].BillableAt = nil
	if err := ValidateChargeSnapshot(AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing", Currency: "RUB"}, snapshot); !errors.Is(err, domain.ErrUsageRecordCorrupt) {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenSucceededBatchIsStoreContractViolation(t *testing.T) {
	r := rec("open-succeeded", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{r}, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2, 0).UTC()
	snapshot := ports.BillingChargeBatchSnapshot{Batch: plan.Batch, Allocations: plan.Allocations, ExpectedRecords: claimedExpected(plan)}
	snapshot.Batch.Status = domain.BillingChargeStatusSucceeded
	snapshot.Batch.ChargedAt = &now
	fl := newFakeLedger(nil)
	fl.open = []ports.BillingChargeBatchSnapshot{snapshot}
	svc, _ := NewAutoChargeService(&fakeIdentity{token: "jwt"}, &fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 100}}, &fakeCharge{}, fl, testClock{now}, AutoChargeConfig{ThresholdCents: 1, MinimumChargeCents: 1})
	_, err = svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if !errors.Is(err, ErrBillingStoreContractViolation) {
		t.Fatalf("err=%v", err)
	}
	if fl.prepareCalls != 0 {
		t.Fatalf("prepare called after contract violation")
	}
}

func TestSucceededRetryClearsFailedMetadata(t *testing.T) {
	r := rec("retry-clear", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{r}, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	failedAt := time.Unix(2, 0).UTC()
	snapshot := ports.BillingChargeBatchSnapshot{Batch: plan.Batch, Allocations: plan.Allocations, ExpectedRecords: claimedExpected(plan)}
	snapshot.Batch.Status = domain.BillingChargeStatusFailed
	snapshot.Batch.FailedAt = &failedAt
	snapshot.Batch.BillingErrorCode = "billing_unavailable"
	fl := newFakeLedger([]domain.UsageRecord{snapshot.ExpectedRecords[0]})
	fl.open = []ports.BillingChargeBatchSnapshot{snapshot}
	chargedAt := time.Unix(3, 0).UTC()
	err = fl.ApplyChargeSuccess(t.Context(), ports.UsageChargeSuccess{BatchID: snapshot.Batch.ID, ChargedAt: chargedAt, Allocations: snapshot.Allocations, ExpectedRecords: snapshot.ExpectedRecords})
	if err != nil {
		t.Fatal(err)
	}
	succeeded := fl.batches[snapshot.Batch.ID].Batch
	if succeeded.FailedAt != nil || succeeded.BillingErrorCode != "" || succeeded.ChargedAt == nil {
		t.Fatalf("failed metadata not cleared: %+v", succeeded)
	}
	if err := domain.ValidateBillingChargeBatch(succeeded); err != nil {
		t.Fatalf("invalid succeeded batch: %v", err)
	}
}

func TestRunAggregatesProcessedFinancialResult(t *testing.T) {
	records := []domain.UsageRecord{
		rec("agg-a", domain.ProviderAnthropic, "a", 50, 0, 50, 1),
		rec("agg-b", domain.ProviderOpenAI, "b", 70, 0, 70, 2),
	}
	remoteAfterFirst := int64(80)
	remoteAfterSecond := int64(10)
	fl := newFakeLedger(records)
	ch := &fakeCharge{results: []ports.BillingChargeResult{{BalanceCents: &remoteAfterFirst}, {BalanceCents: &remoteAfterSecond}}}
	svc, _ := NewAutoChargeService(&fakeIdentity{token: "jwt"}, &fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 130}}, ch, fl, testClock{time.Unix(1, 0).UTC()}, AutoChargeConfig{ThresholdCents: 120, MinimumChargeCents: 1})
	result, err := svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ProcessedBatchIDs) != 2 || result.ChargedAmountCents != 120 || result.BillingBalanceCents == nil || *result.BillingBalanceCents != 10 || !result.UsedBillingBalanceCents {
		t.Fatalf("result=%+v", result)
	}
}

func TestInvalidChargeResultRequiresReconciliation(t *testing.T) {
	r := rec("invalid-charge-result", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	negative := int64(-1)
	fl := newFakeLedger([]domain.UsageRecord{r})
	ch := &fakeCharge{result: ports.BillingChargeResult{BalanceCents: &negative}}
	svc, _ := NewAutoChargeService(&fakeIdentity{token: "jwt"}, &fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 100}}, ch, fl, testClock{time.Unix(1, 0).UTC()}, AutoChargeConfig{ThresholdCents: 100, MinimumChargeCents: 1})
	_, err := svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if !errors.Is(err, ErrChargeReconciliationRequired) {
		t.Fatalf("err=%v", err)
	}
	if fl.applyCalls != 0 {
		t.Fatalf("apply called %d times", fl.applyCalls)
	}
}

func TestRunNormalizesStoreValidationErrors(t *testing.T) {
	badCandidate := rec("store-bad-model", domain.ProviderOpenAI, "m", 10, 0, 10, 1)
	badCandidate.BillingModel = "wrong:model"
	fl := newFakeLedger([]domain.UsageRecord{badCandidate})
	svc, _ := NewAutoChargeService(&fakeIdentity{token: "jwt"}, &fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 100}}, &fakeCharge{}, fl, testClock{time.Unix(1, 0).UTC()}, AutoChargeConfig{ThresholdCents: 1, MinimumChargeCents: 1})
	_, err := svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if !errors.Is(err, ErrBillingStoreContractViolation) {
		t.Fatalf("candidate err=%v", err)
	}

	valid := rec("store-open-bad", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{valid}, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := ports.BillingChargeBatchSnapshot{Batch: plan.Batch, Allocations: plan.Allocations, ExpectedRecords: claimedExpected(plan)}
	snapshot.ExpectedRecords[0].BillableAt = nil
	fl = newFakeLedger(nil)
	fl.open = []ports.BillingChargeBatchSnapshot{snapshot}
	svc, _ = NewAutoChargeService(&fakeIdentity{token: "jwt"}, &fakeBalance{balance: ports.BillingBalance{Currency: "RUB", BalanceCents: 100}}, &fakeCharge{}, fl, testClock{time.Unix(1, 0).UTC()}, AutoChargeConfig{ThresholdCents: 1, MinimumChargeCents: 1})
	_, err = svc.Run(t.Context(), AutoChargeInput{UserID: "u", BillingSubjectUserID: "billing"})
	if !errors.Is(err, ErrBillingStoreContractViolation) {
		t.Fatalf("open err=%v", err)
	}
}

func TestChargeBatchStateMachineIsConditionalAndIdempotent(t *testing.T) {
	r := rec("state-machine", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{r}, 100, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	fl := newFakeLedger([]domain.UsageRecord{r})
	snapshot, err := fl.PrepareChargeBatch(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	chargedAt := time.Unix(2, 0).UTC()
	success := ports.UsageChargeSuccess{BatchID: snapshot.Batch.ID, ChargedAt: chargedAt, Allocations: snapshot.Allocations, ExpectedRecords: snapshot.ExpectedRecords}
	if err := fl.ApplyChargeSuccess(t.Context(), success); err != nil {
		t.Fatal(err)
	}
	record := fl.records[r.LocalRequestID]
	if err := fl.ApplyChargeSuccess(t.Context(), success); err != nil {
		t.Fatal(err)
	}
	if fl.records[r.LocalRequestID] != record {
		t.Fatal("idempotent success reapplied allocations")
	}
	if err := fl.MarkChargeBatchFailed(t.Context(), snapshot.Batch.ID, domain.BillingChargeStatusPending, "billing_unavailable", time.Unix(3, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if got := fl.batches[snapshot.Batch.ID].Batch.Status; got != domain.BillingChargeStatusSucceeded {
		t.Fatalf("succeeded overwritten as %s", got)
	}
}
