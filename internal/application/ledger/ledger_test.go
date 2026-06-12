package ledger

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type fixedClock struct {
	mu    sync.Mutex
	now   time.Time
	calls int
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.now
}

func (c *fixedClock) set(now time.Time) { c.mu.Lock(); c.now = now; c.mu.Unlock() }
func (c *fixedClock) count() int        { c.mu.Lock(); defer c.mu.Unlock(); return c.calls }

type fakeLedger struct {
	mu             sync.Mutex
	records        map[string]domain.UsageRecord
	casCalls       int
	forceCASResult *ports.UsageTransitionResult
}

func newFakeLedger() *fakeLedger { return &fakeLedger{records: map[string]domain.UsageRecord{}} }

func (f *fakeLedger) CreateReserved(ctx context.Context, record domain.UsageRecord) (ports.UsageReserveResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.records {
		if existing.UserID == record.UserID && existing.Status == domain.UsageStatusPricingFailed {
			return ports.UsageReserveResult{Outcome: ports.UsageReserveOutcomeUnresolvedUsage}, nil
		}
	}
	if existing, ok := f.records[record.LocalRequestID]; ok {
		copyValue := copyRecord(existing)
		return ports.UsageReserveResult{Outcome: ports.UsageReserveOutcomeLocalRequestExists, Existing: &copyValue}, nil
	}
	if record.IdempotencyKey != "" {
		for _, existing := range f.records {
			if existing.UserID == record.UserID && existing.EndpointKind == record.EndpointKind && existing.IdempotencyKey == record.IdempotencyKey {
				copyValue := copyRecord(existing)
				return ports.UsageReserveResult{Outcome: ports.UsageReserveOutcomeIdempotencyExists, Existing: &copyValue}, nil
			}
		}
	}
	f.records[record.LocalRequestID] = copyRecord(record)
	return ports.UsageReserveResult{Outcome: ports.UsageReserveOutcomeCreated}, nil
}

func (f *fakeLedger) FindByLocalRequestID(ctx context.Context, localRequestID string) (*domain.UsageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.records[localRequestID]
	if !ok {
		return nil, ports.ErrNotFound
	}
	copyValue := copyRecord(record)
	return &copyValue, nil
}

func (f *fakeLedger) CompareAndSwap(ctx context.Context, localRequestID string, expectedStatus domain.UsageStatus, next domain.UsageRecord) (ports.UsageTransitionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.casCalls++
	if f.forceCASResult != nil {
		return *f.forceCASResult, nil
	}
	current, ok := f.records[localRequestID]
	if !ok {
		return ports.UsageTransitionResult{}, ports.ErrNotFound
	}
	if current.Status != expectedStatus {
		copyValue := copyRecord(current)
		return ports.UsageTransitionResult{Applied: false, Current: &copyValue}, nil
	}
	f.records[localRequestID] = copyRecord(next)
	return ports.UsageTransitionResult{Applied: true}, nil
}

func (f *fakeLedger) LoadExposure(ctx context.Context, userID string, currency string) (ports.UsageExposureSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := ports.UsageExposureSnapshot{Currency: currency}
	for _, r := range f.records {
		if r.UserID != userID || r.Currency != currency {
			continue
		}
		switch r.Status {
		case domain.UsageStatusReserved:
			s.ReservedEstimatedAmountCents += r.EstimatedClientAmountCents
		case domain.UsageStatusBillable:
			s.BillableRemainingAmountCents += r.RemainingAmountCents
		case domain.UsageStatusPartiallyCharged:
			s.PartiallyChargedRemainingAmountCents += r.RemainingAmountCents
		case domain.UsageStatusPricingFailed:
			s.PricingFailedCount++
		}
	}
	return s, nil
}

func (f *fakeLedger) LoadOpenChargeBatches(ctx context.Context, userID string, billingSubjectUserID string, currency string) ([]ports.BillingChargeBatchSnapshot, error) {
	return nil, nil
}

func (f *fakeLedger) LoadChargeCandidates(ctx context.Context, userID string, currency string) ([]domain.UsageRecord, error) {
	return nil, nil
}

func (f *fakeLedger) PrepareChargeBatch(ctx context.Context, plan ports.UsageChargeBatchPlan) (ports.BillingChargeBatchSnapshot, error) {
	return ports.BillingChargeBatchSnapshot{}, nil
}

func (f *fakeLedger) MarkChargeBatchFailed(ctx context.Context, batchID string, expectedStatus domain.BillingChargeStatus, billingErrorCode string, failedAt time.Time) error {
	return nil
}

func (f *fakeLedger) ApplyChargeSuccess(ctx context.Context, success ports.UsageChargeSuccess) error {
	return nil
}

func (f *fakeLedger) put(record domain.UsageRecord) {
	f.mu.Lock()
	f.records[record.LocalRequestID] = copyRecord(record)
	f.mu.Unlock()
}
func (f *fakeLedger) len() int      { f.mu.Lock(); defer f.mu.Unlock(); return len(f.records) }
func (f *fakeLedger) casCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.casCalls }

func newServiceForTest(t *testing.T) (*Service, *fakeLedger, *fixedClock) {
	t.Helper()
	store := newFakeLedger()
	clock := &fixedClock{now: time.Date(2026, 6, 12, 9, 0, 0, 0, time.FixedZone("MSK", 3*60*60))}
	svc, err := NewService(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, clock
}

func validReserveInput(id string) ReserveInput {
	return ReserveInput{LocalRequestID: id, UserID: "user_1", APIKeyID: "key_1", APIFamily: domain.APIFamilyOpenAICompatible, EndpointKind: domain.EndpointChat, ClientModel: "gpt-4o", BillingModel: "gpt-4o", SelectedRouteID: "route_1", SelectedResellerID: "reseller_1", ProviderType: domain.ProviderOpenAI, ProviderModel: "gpt-4o", EstimatedUsage: domain.TokenUsage{InputTokens: 10, OutputTokens: 2}, EstimatedClientAmountCents: 50, EstimatedUpstreamCostCents: 30, Currency: "RUB"}
}

func reserveRecord(id string) domain.UsageRecord {
	now := time.Date(2026, 6, 12, 6, 0, 0, 0, time.UTC)
	input := validReserveInput(id)
	return domain.UsageRecord{LocalRequestID: input.LocalRequestID, UserID: input.UserID, APIKeyID: input.APIKeyID, APIFamily: input.APIFamily, EndpointKind: input.EndpointKind, ClientModel: input.ClientModel, BillingModel: input.BillingModel, SelectedRouteID: input.SelectedRouteID, SelectedResellerID: input.SelectedResellerID, ProviderType: input.ProviderType, ProviderModel: input.ProviderModel, EstimatedUsage: input.EstimatedUsage, EstimatedClientAmountCents: input.EstimatedClientAmountCents, EstimatedUpstreamCostCents: input.EstimatedUpstreamCostCents, Currency: "RUB", UsageCompleteness: string(pricing.UsageCompletenessMissing), Status: domain.UsageStatusReserved, CreatedAt: now, ReservedAt: &now, UpdatedAt: now}
}

func billableRecord(id string) domain.UsageRecord {
	r := reserveRecord(id)
	now := time.Date(2026, 6, 12, 6, 0, 0, 0, time.UTC)
	r.Status = domain.UsageStatusBillable
	r.Usage = domain.TokenUsage{InputTokens: 1}
	r.UsageCompleteness = string(pricing.UsageCompletenessDetailed)
	r.ClientAmountCents = 10
	r.RemainingAmountCents = 10
	r.BillableAt = &now
	return r
}

func chargedRecord(id string) domain.UsageRecord {
	r := billableRecord(id)
	now := time.Date(2026, 6, 12, 6, 0, 0, 0, time.UTC)
	r.Status = domain.UsageStatusCharged
	r.ChargedAmountCents = r.ClientAmountCents
	r.RemainingAmountCents = 0
	r.ChargedAt = &now
	r.BillingChargeRequestID = "billchg_" + strings.Repeat("a", 64)
	return r
}

func failedRecord(id string) domain.UsageRecord {
	r := reserveRecord(id)
	now := time.Date(2026, 6, 12, 6, 0, 0, 0, time.UTC)
	r.Status = domain.UsageStatusFailed
	r.FailureReason = "internal_failure"
	r.FailedAt = &now
	return r
}

func TestStateMachineAllowedForbiddenAndUnknown(t *testing.T) {
	allowed := [][2]domain.UsageStatus{{domain.UsageStatusReserved, domain.UsageStatusReleased}, {domain.UsageStatusReserved, domain.UsageStatusBillable}, {domain.UsageStatusReserved, domain.UsageStatusFailed}, {domain.UsageStatusReserved, domain.UsageStatusPricingFailed}, {domain.UsageStatusBillable, domain.UsageStatusCharged}, {domain.UsageStatusBillable, domain.UsageStatusPartiallyCharged}, {domain.UsageStatusBillable, domain.UsageStatusFailed}, {domain.UsageStatusPartiallyCharged, domain.UsageStatusCharged}, {domain.UsageStatusPartiallyCharged, domain.UsageStatusPartiallyCharged}, {domain.UsageStatusPartiallyCharged, domain.UsageStatusFailed}, {domain.UsageStatusPricingFailed, domain.UsageStatusBillable}, {domain.UsageStatusPricingFailed, domain.UsageStatusCharged}, {domain.UsageStatusPricingFailed, domain.UsageStatusFailed}}
	for _, pair := range allowed {
		if !CanTransition(pair[0], pair[1]) || ValidateTransition(pair[0], pair[1]) != nil {
			t.Fatalf("allowed transition rejected: %s -> %s", pair[0], pair[1])
		}
	}
	forbidden := [][2]domain.UsageStatus{{domain.UsageStatusReleased, domain.UsageStatusBillable}, {domain.UsageStatusReleased, domain.UsageStatusReserved}, {domain.UsageStatusCharged, domain.UsageStatusBillable}, {domain.UsageStatusCharged, domain.UsageStatusReserved}, {domain.UsageStatusFailed, domain.UsageStatusCharged}, {domain.UsageStatusFailed, domain.UsageStatusReserved}, {domain.UsageStatusBillable, domain.UsageStatusReserved}, {domain.UsageStatusPricingFailed, domain.UsageStatusReserved}}
	for _, pair := range forbidden {
		if CanTransition(pair[0], pair[1]) || !errors.Is(ValidateTransition(pair[0], pair[1]), ErrInvalidStateTransition) {
			t.Fatalf("forbidden transition accepted: %s -> %s", pair[0], pair[1])
		}
	}
	if !errors.Is(ValidateTransition(domain.UsageStatus("weird"), domain.UsageStatusReserved), ErrInvalidUsageStatus) {
		t.Fatal("unknown from not invalid")
	}
	if !errors.Is(ValidateTransition(domain.UsageStatusReserved, domain.UsageStatus("weird")), ErrInvalidUsageStatus) {
		t.Fatal("unknown to not invalid")
	}
}

func TestReserveValidationAndIdempotencyKey(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ReserveInput)
		want   error
	}{
		{"valid reserve created", func(*ReserveInput) {}, nil}, {"local request without llmreq_ rejected", func(i *ReserveInput) { i.LocalRequestID = "x" }, ErrInvalidLedgerInput}, {"llmreq_ without suffix rejected", func(i *ReserveInput) { i.LocalRequestID = "llmreq_" }, ErrInvalidLedgerInput}, {"blank user ID rejected", func(i *ReserveInput) { i.UserID = " " }, ErrInvalidLedgerInput}, {"blank API key ID rejected", func(i *ReserveInput) { i.APIKeyID = " " }, ErrInvalidLedgerInput}, {"empty API family rejected", func(i *ReserveInput) { i.APIFamily = "" }, ErrInvalidLedgerInput}, {"empty endpoint kind rejected", func(i *ReserveInput) { i.EndpointKind = "" }, ErrInvalidLedgerInput}, {"blank client model rejected", func(i *ReserveInput) { i.ClientModel = " " }, ErrInvalidLedgerInput}, {"blank billing model rejected", func(i *ReserveInput) { i.BillingModel = " " }, ErrInvalidLedgerInput}, {"blank route ID rejected", func(i *ReserveInput) { i.SelectedRouteID = " " }, ErrInvalidLedgerInput}, {"blank reseller ID rejected", func(i *ReserveInput) { i.SelectedResellerID = " " }, ErrInvalidLedgerInput}, {"empty provider type rejected", func(i *ReserveInput) { i.ProviderType = "" }, ErrInvalidLedgerInput}, {"blank provider model rejected", func(i *ReserveInput) { i.ProviderModel = " " }, ErrInvalidLedgerInput}, {"negative estimated client amount rejected", func(i *ReserveInput) { i.EstimatedClientAmountCents = -1 }, ErrInvalidLedgerInput}, {"negative estimated upstream cost rejected", func(i *ReserveInput) { i.EstimatedUpstreamCostCents = -1 }, ErrInvalidLedgerInput}, {"negative estimated usage rejected", func(i *ReserveInput) { i.EstimatedUsage.InputTokens = -1 }, ErrInvalidLedgerInput}, {"currency other than RUB rejected", func(i *ReserveInput) { i.Currency = "USD" }, ErrInvalidLedgerInput}, {"zero estimated amount accepted", func(i *ReserveInput) { i.EstimatedClientAmountCents = 0 }, nil}, {"zero estimated usage accepted", func(i *ReserveInput) { i.EstimatedUsage = domain.TokenUsage{} }, nil},
	}
	for n, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := newServiceForTest(t)
			input := validReserveInput("llmreq_val_" + string(rune('a'+n)))
			tc.mutate(&input)
			res, err := svc.Reserve(t.Context(), input)
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if tc.want == nil {
				if err != nil {
					t.Fatal(err)
				}
				if res.Disposition != ReserveDispositionCreated {
					t.Fatal(res.Disposition)
				}
			}
		})
	}

	for _, tc := range []struct {
		name    string
		key     *string
		wantErr bool
		want    string
	}{{"nil key accepted as absent", nil, false, ""}, {"exact key preserved", ptr("Key-1"), false, "Key-1"}, {"empty provided key rejected", ptr(""), true, ""}, {"whitespace-only provided key rejected", ptr("  \t"), true, ""}, {"leading/trailing spaces in nonblank key preserved", ptr(" key "), false, " key "}, {"different case preserved", ptr("AbC"), false, "AbC"}, {"key not hashed", ptr("plain-key"), false, "plain-key"}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _ := newServiceForTest(t)
			input := validReserveInput("llmreq_key_" + strings.ReplaceAll(tc.name, " ", "_"))
			input.IdempotencyKey = tc.key
			res, err := svc.Reserve(t.Context(), input)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidLedgerInput) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Record.IdempotencyKey != tc.want {
				t.Fatalf("key=%q want %q", res.Record.IdempotencyKey, tc.want)
			}
		})
	}
}

func TestLocalRequestAndClientIdempotency(t *testing.T) {
	svc, store, _ := newServiceForTest(t)
	input := validReserveInput("llmreq_same")
	first, err := svc.Reserve(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Reserve(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if second.Disposition != ReserveDispositionAlreadyReserved || !SameReservation(first.Record, second.Record) || store.len() != 1 {
		t.Fatal("same local idempotency failed")
	}
	for _, mutate := range []func(*ReserveInput){func(i *ReserveInput) { i.UserID = "other" }, func(i *ReserveInput) { i.EstimatedClientAmountCents++ }, func(i *ReserveInput) { i.SelectedRouteID = "route_2" }} {
		changed := input
		mutate(&changed)
		if _, err := svc.Reserve(t.Context(), changed); !errors.Is(err, ErrLocalRequestConflict) {
			t.Fatalf("want local conflict got %v", err)
		}
	}
	r := billableRecord(first.Record.LocalRequestID)
	store.put(r)
	if _, err := svc.Reserve(t.Context(), input); !errors.Is(err, ErrLocalRequestConflict) {
		t.Fatalf("billable local err=%v", err)
	}

	key := "idem"
	in1 := validReserveInput("llmreq_idem_1")
	in1.IdempotencyKey = &key
	if _, err := svc.Reserve(t.Context(), in1); err != nil {
		t.Fatal(err)
	}
	in2 := validReserveInput("llmreq_idem_2")
	in2.IdempotencyKey = &key
	if _, err := svc.Reserve(t.Context(), in2); !errors.Is(err, ErrRequestInProgress) {
		t.Fatalf("same scope err=%v", err)
	}
	in3 := validReserveInput("llmreq_idem_3")
	in3.UserID = "user_2"
	in3.IdempotencyKey = &key
	if _, err := svc.Reserve(t.Context(), in3); err != nil {
		t.Fatal(err)
	}
	in4 := validReserveInput("llmreq_idem_4")
	in4.EndpointKind = domain.EndpointEmbeddings
	in4.IdempotencyKey = &key
	if _, err := svc.Reserve(t.Context(), in4); err != nil {
		t.Fatal(err)
	}
	in5 := validReserveInput("llmreq_idem_5")
	in6 := validReserveInput("llmreq_idem_6")
	if _, err := svc.Reserve(t.Context(), in5); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reserve(t.Context(), in6); err != nil {
		t.Fatal(err)
	}
}

func TestIdempotencyExistingStatusMappingAndConcurrency(t *testing.T) {
	for status, want := range map[domain.UsageStatus]error{domain.UsageStatusReserved: ErrRequestInProgress, domain.UsageStatusBillable: ErrIdempotencyReplayNotAvailable, domain.UsageStatusPartiallyCharged: ErrIdempotencyReplayNotAvailable, domain.UsageStatusCharged: ErrIdempotencyReplayNotAvailable, domain.UsageStatusReleased: ErrIdempotencyKeyReused, domain.UsageStatusFailed: ErrIdempotencyKeyReused, domain.UsageStatusPricingFailed: ErrUnresolvedUsage, domain.UsageStatus("mystery"): ErrRecordCorrupt} {
		t.Run(string(status), func(t *testing.T) {
			svc, store, _ := newServiceForTest(t)
			key := "idem_status"
			r := reserveRecord("llmreq_existing")
			r.IdempotencyKey = key
			r.Status = status
			store.put(r)
			input := validReserveInput("llmreq_new")
			input.IdempotencyKey = &key
			_, err := svc.Reserve(t.Context(), input)
			if !errors.Is(err, want) {
				t.Fatalf("err=%v want %v", err, want)
			}
		})
	}

	svc, store, _ := newServiceForTest(t)
	key := "race"
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, id := range []string{"llmreq_race_1", "llmreq_race_2"} {
		go func(id string) {
			<-start
			input := validReserveInput(id)
			input.IdempotencyKey = &key
			_, err := svc.Reserve(context.WithValue(t.Context(), struct{}{}, id), input)
			errs <- err
		}(id)
	}
	close(start)
	e1, e2 := <-errs, <-errs
	created, conflict := 0, 0
	for _, err := range []error{e1, e2} {
		if err == nil {
			created++
		} else if errors.Is(err, ErrRequestInProgress) {
			conflict++
		} else {
			t.Fatalf("unexpected err %v", err)
		}
	}
	if created != 1 || conflict != 1 || store.len() != 1 {
		t.Fatalf("created=%d conflict=%d len=%d", created, conflict, store.len())
	}
}

func TestUnresolvedUsageBlocksReserveAndPending(t *testing.T) {
	svc, store, _ := newServiceForTest(t)
	pf := reserveRecord("llmreq_pf")
	pf.Status = domain.UsageStatusPricingFailed
	pf.FailureReason = "usage_resolution_failed"
	store.put(pf)
	if _, err := svc.Reserve(t.Context(), validReserveInput("llmreq_no_key")); !errors.Is(err, ErrUnresolvedUsage) {
		t.Fatalf("no key err=%v", err)
	}
	key := "new"
	withKey := validReserveInput("llmreq_with_key")
	withKey.IdempotencyKey = &key
	if _, err := svc.Reserve(t.Context(), withKey); !errors.Is(err, ErrUnresolvedUsage) {
		t.Fatalf("with key err=%v", err)
	}
	other := validReserveInput("llmreq_other_user")
	other.UserID = "other"
	if _, err := svc.Reserve(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	if _, err := PendingAmountForRecord(pf); !errors.Is(err, ErrUnresolvedUsage) {
		t.Fatalf("pricing_failed pending err=%v", err)
	}
	before := store.len()
	blocked := validReserveInput("llmreq_blocked_created")
	_, _ = svc.Reserve(t.Context(), blocked)
	if store.len() != before {
		t.Fatal("unresolved outcome created record")
	}
}

func TestReleaseFailBillablePricingFailed(t *testing.T) {
	t.Run("release", func(t *testing.T) {
		svc, store, clock := newServiceForTest(t)
		r := reserveRecord("llmreq_rel")
		store.put(r)
		out, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: r.LocalRequestID, FailureReason: "upstream_not_sent"})
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != domain.UsageStatusReleased || out.FailureReason != "upstream_not_sent" || out.ReleasedAt == nil || out.ReleasedAt.Location() != time.UTC || out.UpdatedAt.Location() != time.UTC || out.EstimatedClientAmountCents != r.EstimatedClientAmountCents || out.ClientAmountCents != 0 {
			t.Fatal("bad release")
		}
		clock.set(clock.now.Add(time.Hour))
		again, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: r.LocalRequestID, FailureReason: "upstream_not_sent"})
		if err != nil {
			t.Fatal(err)
		}
		if !again.ReleasedAt.Equal(*out.ReleasedAt) || !again.UpdatedAt.Equal(out.UpdatedAt) {
			t.Fatal("timestamp changed")
		}
		if _, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: r.LocalRequestID, FailureReason: "other"}); !errors.Is(err, ErrLedgerStateConflict) {
			t.Fatalf("diff reason err=%v", err)
		}
		billable := billableRecord("llmreq_rel_billable")
		store.put(billable)
		if _, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: billable.LocalRequestID, FailureReason: "upstream_not_sent"}); !errors.Is(err, ErrInvalidStateTransition) {
			t.Fatalf("billable release err=%v", err)
		}
		charged := chargedRecord("llmreq_rel_charged")
		store.put(charged)
		if _, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: charged.LocalRequestID, FailureReason: "upstream_not_sent"}); !errors.Is(err, ErrInvalidStateTransition) {
			t.Fatalf("charged release err=%v", err)
		}
		if _, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: "llmreq_raw", FailureReason: "provider said: raw-provider-response"}); !errors.Is(err, ErrInvalidLedgerInput) {
			t.Fatalf("raw reason err=%v", err)
		}
	})
	t.Run("fail", func(t *testing.T) {
		svc, store, _ := newServiceForTest(t)
		r := reserveRecord("llmreq_fail")
		store.put(r)
		out, err := svc.Fail(t.Context(), FailInput{LocalRequestID: r.LocalRequestID, FailureReason: "internal_failure"})
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != domain.UsageStatusFailed || out.FailedAt == nil {
			t.Fatal("bad fail")
		}
		if _, err := svc.Fail(t.Context(), FailInput{LocalRequestID: r.LocalRequestID, FailureReason: "internal_failure"}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Fail(t.Context(), FailInput{LocalRequestID: r.LocalRequestID, FailureReason: "validation_failed"}); !errors.Is(err, ErrLedgerStateConflict) {
			t.Fatalf("diff fail err=%v", err)
		}
		bill := billableRecord("llmreq_fail_bill")
		store.put(bill)
		if _, err := svc.Fail(t.Context(), FailInput{LocalRequestID: bill.LocalRequestID, FailureReason: "internal_failure"}); !errors.Is(err, ErrInvalidStateTransition) {
			t.Fatalf("billable fail err=%v", err)
		}
	})
	t.Run("billable", func(t *testing.T) {
		svc, store, _ := newServiceForTest(t)
		r := reserveRecord("llmreq_bill")
		store.put(r)
		input := CommitBillableInput{LocalRequestID: r.LocalRequestID, Usage: domain.TokenUsage{InputTokens: 11, OutputTokens: 3}, UsageCompleteness: pricing.UsageCompletenessDetailed, ClientAmountCents: 77, ActualUpstreamCostCents: 55, ProviderRequestID: "req_provider", ProviderResponseModel: "provider_model"}
		out, err := svc.CommitBillable(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != domain.UsageStatusBillable || out.Usage != input.Usage || out.UsageCompleteness != string(input.UsageCompleteness) || out.ProviderRequestID != input.ProviderRequestID || out.ProviderResponseModel != input.ProviderResponseModel || out.ActualUpstreamCostCents != 55 || out.ClientAmountCents != 77 || out.ChargedAmountCents != 0 || out.RemainingAmountCents != 77 || out.BillableAt == nil || !SameReservation(r, out) {
			t.Fatal("bad billable")
		}
		if _, err := svc.CommitBillable(t.Context(), input); err != nil {
			t.Fatal(err)
		}
		changed := input
		changed.ClientAmountCents = 78
		if _, err := svc.CommitBillable(t.Context(), changed); !errors.Is(err, ErrLedgerStateConflict) {
			t.Fatalf("changed amount err=%v", err)
		}
		changed = input
		changed.Usage.OutputTokens++
		if _, err := svc.CommitBillable(t.Context(), changed); !errors.Is(err, ErrLedgerStateConflict) {
			t.Fatalf("changed usage err=%v", err)
		}
		zero := reserveRecord("llmreq_zero")
		store.put(zero)
		input.LocalRequestID = zero.LocalRequestID
		input.ClientAmountCents = 0
		if out, err := svc.CommitBillable(t.Context(), input); err != nil || out.RemainingAmountCents != 0 {
			t.Fatalf("zero err=%v out=%+v", err, out)
		}
	})
	t.Run("pricing failed", func(t *testing.T) {
		svc, store, _ := newServiceForTest(t)
		r := reserveRecord("llmreq_pricefail")
		store.put(r)
		input := MarkPricingFailedInput{LocalRequestID: r.LocalRequestID, Usage: domain.TokenUsage{InputTokens: 9}, UsageCompleteness: pricing.UsageCompletenessFailed, ProviderRequestID: "req", ProviderResponseModel: "model", FailureReason: "usage_resolution_failed"}
		out, err := svc.MarkPricingFailed(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != domain.UsageStatusPricingFailed || out.Usage != input.Usage || out.ProviderRequestID != "req" || out.FailureReason != input.FailureReason || out.ClientAmountCents != 0 || out.RemainingAmountCents != 0 || !SameReservation(r, out) {
			t.Fatal("bad pricing failed")
		}
		if _, err := svc.MarkPricingFailed(t.Context(), input); err != nil {
			t.Fatal(err)
		}
		input.FailureReason = "internal_failure"
		if _, err := svc.MarkPricingFailed(t.Context(), input); !errors.Is(err, ErrLedgerStateConflict) {
			t.Fatalf("diff pf err=%v", err)
		}
		if _, err := svc.Reserve(t.Context(), validReserveInput("llmreq_after_pf")); !errors.Is(err, ErrUnresolvedUsage) {
			t.Fatalf("future reserve err=%v", err)
		}
	})
}

func TestCompletenessAndValidationFailures(t *testing.T) {
	for _, c := range []pricing.UsageCompleteness{pricing.UsageCompletenessDetailed, pricing.UsageCompletenessAggregate, pricing.UsageCompletenessEstimated} {
		svc, store, _ := newServiceForTest(t)
		r := reserveRecord("llmreq_comp_" + string(c))
		store.put(r)
		if _, err := svc.CommitBillable(t.Context(), CommitBillableInput{LocalRequestID: r.LocalRequestID, UsageCompleteness: c}); err != nil {
			t.Fatalf("%s err=%v", c, err)
		}
	}
	for _, c := range []pricing.UsageCompleteness{pricing.UsageCompletenessMissing, pricing.UsageCompletenessFailed, pricing.UsageCompleteness("unknown")} {
		svc, _, _ := newServiceForTest(t)
		if _, err := svc.CommitBillable(t.Context(), CommitBillableInput{LocalRequestID: "llmreq_bad", UsageCompleteness: c}); !errors.Is(err, ErrInvalidLedgerInput) {
			t.Fatalf("%s err=%v", c, err)
		}
	}
	for _, c := range []pricing.UsageCompleteness{pricing.UsageCompletenessDetailed, pricing.UsageCompletenessAggregate, pricing.UsageCompletenessEstimated, pricing.UsageCompletenessMissing, pricing.UsageCompletenessFailed} {
		svc, store, _ := newServiceForTest(t)
		r := reserveRecord("llmreq_pfcomp_" + string(c))
		store.put(r)
		if _, err := svc.MarkPricingFailed(t.Context(), MarkPricingFailedInput{LocalRequestID: r.LocalRequestID, UsageCompleteness: c, FailureReason: "internal_failure"}); err != nil {
			t.Fatalf("pf %s err=%v", c, err)
		}
	}
	svc, _, _ := newServiceForTest(t)
	if _, err := svc.CommitBillable(t.Context(), CommitBillableInput{LocalRequestID: "llmreq_bad_usage", Usage: domain.TokenUsage{InputTokens: -1}, UsageCompleteness: pricing.UsageCompletenessDetailed}); !errors.Is(err, ErrInvalidLedgerInput) {
		t.Fatalf("negative usage err=%v", err)
	}
	if _, err := svc.CommitBillable(t.Context(), CommitBillableInput{LocalRequestID: "llmreq_bad_amount", UsageCompleteness: pricing.UsageCompletenessDetailed, ClientAmountCents: -1}); !errors.Is(err, ErrInvalidLedgerInput) {
		t.Fatalf("negative amount err=%v", err)
	}
}

func TestCASConflictSemantics(t *testing.T) {
	svc, store, _ := newServiceForTest(t)
	r := reserveRecord("llmreq_cas")
	store.put(r)
	out, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: r.LocalRequestID, FailureReason: "upstream_not_sent"})
	if err != nil || out.Status != domain.UsageStatusReleased || store.casCount() != 1 {
		t.Fatalf("applied err=%v calls=%d", err, store.casCount())
	}
	svc, store, _ = newServiceForTest(t)
	r = reserveRecord("llmreq_cas_same")
	store.put(r)
	desired := r
	desired.Status = domain.UsageStatusReleased
	desired.FailureReason = "upstream_not_sent"
	nt := time.Date(2026, 6, 12, 6, 0, 0, 0, time.UTC)
	desired.ReleasedAt = &nt
	desired.UpdatedAt = nt
	store.forceCASResult = &ports.UsageTransitionResult{Applied: false, Current: &desired}
	if _, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: r.LocalRequestID, FailureReason: "upstream_not_sent"}); err != nil || store.casCount() != 1 {
		t.Fatalf("same lost err=%v calls=%d", err, store.casCount())
	}
	svc, store, _ = newServiceForTest(t)
	r = reserveRecord("llmreq_cas_diff")
	store.put(r)
	diff := failedRecord(r.LocalRequestID)
	store.forceCASResult = &ports.UsageTransitionResult{Applied: false, Current: &diff}
	if _, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: r.LocalRequestID, FailureReason: "upstream_not_sent"}); !errors.Is(err, ErrLedgerStateConflict) || store.casCount() != 1 {
		t.Fatalf("diff err=%v calls=%d", err, store.casCount())
	}
	svc, store, _ = newServiceForTest(t)
	r = reserveRecord("llmreq_cas_owned")
	store.put(r)
	diffOwned := desired
	diffOwned.FailureReason = "other"
	store.forceCASResult = &ports.UsageTransitionResult{Applied: false, Current: &diffOwned}
	if _, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: r.LocalRequestID, FailureReason: "upstream_not_sent"}); !errors.Is(err, ErrLedgerStateConflict) || store.casCount() != 1 {
		t.Fatalf("owned err=%v calls=%d", err, store.casCount())
	}
}

func TestRecordValidationPendingExposureBalance(t *testing.T) {
	now := time.Date(2026, 6, 12, 6, 0, 0, 0, time.UTC)
	valid := []domain.UsageRecord{reserveRecord("llmreq_v_res"), func() domain.UsageRecord {
		r := reserveRecord("llmreq_v_rel")
		r.Status = domain.UsageStatusReleased
		r.FailureReason = "upstream_not_sent"
		r.ReleasedAt = &now
		return r
	}(), func() domain.UsageRecord {
		r := reserveRecord("llmreq_v_bill")
		r.Status = domain.UsageStatusBillable
		r.Usage = domain.TokenUsage{InputTokens: 1}
		r.UsageCompleteness = string(pricing.UsageCompletenessDetailed)
		r.ClientAmountCents = 10
		r.RemainingAmountCents = 10
		r.BillableAt = &now
		return r
	}(), func() domain.UsageRecord {
		r := reserveRecord("llmreq_v_part")
		r.Status = domain.UsageStatusPartiallyCharged
		r.Usage = domain.TokenUsage{InputTokens: 1}
		r.UsageCompleteness = string(pricing.UsageCompletenessDetailed)
		r.BillableAt = &now
		r.ClientAmountCents = 10
		r.ChargedAmountCents = 4
		r.RemainingAmountCents = 6
		r.ChargedAt = &now
		r.BillingChargeRequestID = "billchg_" + strings.Repeat("c", 64)
		return r
	}(), func() domain.UsageRecord {
		r := reserveRecord("llmreq_v_charged")
		r.Status = domain.UsageStatusCharged
		r.Usage = domain.TokenUsage{InputTokens: 1}
		r.UsageCompleteness = string(pricing.UsageCompletenessDetailed)
		r.BillableAt = &now
		r.ClientAmountCents = 10
		r.ChargedAmountCents = 10
		r.RemainingAmountCents = 0
		r.ChargedAt = &now
		r.BillingChargeRequestID = "billchg_" + strings.Repeat("b", 64)
		return r
	}(), func() domain.UsageRecord {
		r := reserveRecord("llmreq_v_failed")
		r.Status = domain.UsageStatusFailed
		r.FailureReason = "internal_failure"
		r.FailedAt = &now
		return r
	}(), func() domain.UsageRecord {
		r := reserveRecord("llmreq_v_pf")
		r.Status = domain.UsageStatusPricingFailed
		r.FailureReason = "usage_resolution_failed"
		return r
	}()}
	for _, r := range valid {
		if err := ValidateRecord(r); err != nil {
			t.Fatalf("valid %s err=%v", r.Status, err)
		}
	}
	bad := []domain.UsageRecord{func() domain.UsageRecord { r := reserveRecord("llmreq_b1"); r.ClientAmountCents = 1; return r }(), func() domain.UsageRecord { r := valid[1]; r.ReleasedAt = nil; return r }(), func() domain.UsageRecord { r := valid[2]; r.BillableAt = nil; return r }(), func() domain.UsageRecord { r := valid[2]; r.RemainingAmountCents = 9; return r }(), func() domain.UsageRecord { r := valid[3]; r.ChargedAmountCents = 0; return r }(), func() domain.UsageRecord { r := valid[3]; r.ChargedAmountCents = 10; return r }(), func() domain.UsageRecord { r := valid[3]; r.RemainingAmountCents = 5; return r }(), func() domain.UsageRecord { r := valid[4]; r.RemainingAmountCents = 1; return r }(), func() domain.UsageRecord { r := valid[4]; r.ChargedAt = nil; return r }(), func() domain.UsageRecord { r := valid[6]; r.ClientAmountCents = 1; return r }()}
	for _, r := range bad {
		if err := ValidateRecord(r); err == nil {
			t.Fatalf("bad %s accepted", r.Status)
		}
	}
	if !errors.Is(ValidateTransition(domain.UsageStatusReleased, domain.UsageStatusBillable), ErrInvalidStateTransition) {
		t.Fatal("terminal transition accepted")
	}

	pendingCases := map[domain.UsageStatus]struct {
		amount int64
		err    error
	}{domain.UsageStatusReserved: {50, nil}, domain.UsageStatusBillable: {10, nil}, domain.UsageStatusPartiallyCharged: {6, nil}, domain.UsageStatusReleased: {0, nil}, domain.UsageStatusCharged: {0, nil}, domain.UsageStatusFailed: {0, nil}, domain.UsageStatusPricingFailed: {0, ErrUnresolvedUsage}, domain.UsageStatus("bad"): {0, ErrRecordCorrupt}}
	for st, tc := range pendingCases {
		r := reserveRecord("llmreq_p_" + string(st))
		r.Status = st
		r.ClientAmountCents = 10
		r.RemainingAmountCents = tc.amount
		if st == domain.UsageStatusReserved {
			r.EstimatedClientAmountCents = tc.amount
		}
		got, err := PendingAmountForRecord(r)
		if tc.err != nil && !errors.Is(err, tc.err) {
			t.Fatalf("%s err=%v", st, err)
		}
		if tc.err == nil && (err != nil || got != tc.amount) {
			t.Fatalf("%s got=%d err=%v", st, got, err)
		}
	}

	store := newFakeLedger()
	store.put(reserveRecord("llmreq_load_exp"))
	svc, err := NewService(store, &fixedClock{now: now})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.LoadExposure(t.Context(), "user_1", "RUB")
	if err != nil || loaded.PendingAmountCents != 50 {
		t.Fatalf("loaded exposure=%+v err=%v", loaded, err)
	}

	exp, err := CalculateExposure(ports.UsageExposureSnapshot{Currency: "RUB", ReservedEstimatedAmountCents: 1, BillableRemainingAmountCents: 2, PartiallyChargedRemainingAmountCents: 3, PricingFailedCount: 1})
	if err != nil || exp.PendingAmountCents != 6 || !exp.HasUnresolvedUsage {
		t.Fatalf("exp=%+v err=%v", exp, err)
	}
	if _, err := CalculateExposure(ports.UsageExposureSnapshot{Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	if _, err := CalculateExposure(ports.UsageExposureSnapshot{Currency: "RUB", ReservedEstimatedAmountCents: -1}); !errors.Is(err, ErrInvalidLedgerInput) {
		t.Fatalf("negative exp err=%v", err)
	}
	if _, err := CalculateExposure(ports.UsageExposureSnapshot{Currency: "RUB", ReservedEstimatedAmountCents: math.MaxInt64, BillableRemainingAmountCents: 1}); !errors.Is(err, ErrAmountOverflow) {
		t.Fatalf("overflow exp err=%v", err)
	}
	if _, err := CalculateExposure(ports.UsageExposureSnapshot{Currency: "USD"}); !errors.Is(err, ErrInvalidLedgerInput) {
		t.Fatalf("currency exp err=%v", err)
	}

	bal, err := EvaluateBalance(BalanceInput{RemoteBalanceCents: 100, RequiredReserveCents: 80, Exposure: Exposure{Currency: "RUB", PendingAmountCents: 20}})
	if err != nil || !bal.Allowed || bal.EffectiveBalanceCents != 80 {
		t.Fatalf("bal=%+v err=%v", bal, err)
	}
	if _, err := EvaluateBalance(BalanceInput{RemoteBalanceCents: 100, RequiredReserveCents: 81, Exposure: Exposure{Currency: "RUB", PendingAmountCents: 20}}); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("insufficient err=%v", err)
	}
	for _, input := range []BalanceInput{{RemoteBalanceCents: 0, RequiredReserveCents: 0, Exposure: Exposure{Currency: "RUB"}}, {RemoteBalanceCents: 10, RequiredReserveCents: 0, Exposure: Exposure{Currency: "RUB", PendingAmountCents: 20}}} {
		_, _ = EvaluateBalance(input)
	}
	if _, err := EvaluateBalance(BalanceInput{RemoteBalanceCents: 1, Exposure: Exposure{Currency: "RUB", HasUnresolvedUsage: true}}); !errors.Is(err, ErrUnresolvedUsage) {
		t.Fatalf("unresolved bal err=%v", err)
	}
	if _, err := EvaluateBalance(BalanceInput{RemoteBalanceCents: -1, Exposure: Exposure{Currency: "RUB"}}); !errors.Is(err, ErrInvalidLedgerInput) {
		t.Fatalf("negative remote err=%v", err)
	}
	if _, err := EvaluateBalance(BalanceInput{RemoteBalanceCents: 1, RequiredReserveCents: -1, Exposure: Exposure{Currency: "RUB"}}); !errors.Is(err, ErrInvalidLedgerInput) {
		t.Fatalf("negative required err=%v", err)
	}
	if _, err := EvaluateBalance(BalanceInput{RemoteBalanceCents: math.MinInt64, Exposure: Exposure{Currency: "RUB", PendingAmountCents: 1}}); !errors.Is(err, ErrInvalidLedgerInput) {
		t.Fatalf("overflow/negative input err=%v", err)
	}
	input := BalanceInput{RemoteBalanceCents: 100, RequiredReserveCents: 1, Exposure: Exposure{Currency: "RUB", PendingAmountCents: 2}}
	original := input
	_, _ = EvaluateBalance(input)
	if !reflect.DeepEqual(input, original) {
		t.Fatal("input mutated")
	}
}

func TestPersistenceBoundaryValidatesRecords(t *testing.T) {
	svc, store, _ := newServiceForTest(t)
	corrupt := reserveRecord("llmreq_corrupt_loaded")
	corrupt.ClientAmountCents = 1
	store.put(corrupt)
	if _, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: corrupt.LocalRequestID, FailureReason: "upstream_not_sent"}); !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("corrupt loaded err=%v", err)
	}
	if store.casCount() != 0 {
		t.Fatalf("CAS called for corrupt loaded record: %d", store.casCount())
	}

	svc, store, _ = newServiceForTest(t)
	reserved := reserveRecord("llmreq_corrupt_cas_current")
	store.put(reserved)
	desired := reserved
	desired.Status = domain.UsageStatusReleased
	desired.FailureReason = "upstream_not_sent"
	now := time.Date(2026, 6, 12, 6, 0, 0, 0, time.UTC)
	desired.ReleasedAt = &now
	desired.UpdatedAt = now
	corruptCurrent := desired
	corruptCurrent.ClientAmountCents = 1
	store.forceCASResult = &ports.UsageTransitionResult{Applied: false, Current: &corruptCurrent}
	if _, err := svc.Release(t.Context(), ReleaseInput{LocalRequestID: reserved.LocalRequestID, FailureReason: "upstream_not_sent"}); !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("corrupt CAS current err=%v", err)
	}
	if store.casCount() != 1 {
		t.Fatalf("CAS calls=%d, want 1", store.casCount())
	}

	svc, store, _ = newServiceForTest(t)
	input := validReserveInput("llmreq_constructed_validated")
	if _, err := svc.Reserve(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	persisted, err := store.FindByLocalRequestID(t.Context(), input.LocalRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRecord(*persisted); err != nil {
		t.Fatalf("constructed reserved record was not valid: %v", err)
	}
}

func TestSafetyNoPayloadsOrSecrets(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(ReserveInput{}), reflect.TypeOf(ReserveResult{}), reflect.TypeOf(ReleaseInput{}), reflect.TypeOf(FailInput{}), reflect.TypeOf(CommitBillableInput{}), reflect.TypeOf(MarkPricingFailedInput{}), reflect.TypeOf(Exposure{}), reflect.TypeOf(BalanceInput{}), reflect.TypeOf(BalanceResult{}), reflect.TypeOf(domain.UsageRecord{})} {
		forbiddenFieldFragments := []string{"RequestBody", "ResponseBody", "RawRequest", "RawResponse", "Authorization", "RawAPIKey", "UserAPIKey", "ResellerAPIKey", "BillingJWT", "ServiceToken", "ProviderErrorBody"}
		for i := 0; i < typ.NumField(); i++ {
			for _, fragment := range forbiddenFieldFragments {
				if strings.Contains(typ.Field(i).Name, fragment) {
					t.Fatalf("%s has forbidden field %s", typ.Name(), typ.Field(i).Name)
				}
			}
		}
	}
	secrets := []string{"sk_user_secret", "rk_reseller_secret", "billing-jwt-secret", "raw-provider-response", "raw-request-prompt"}
	values := []any{ReserveResult{}, Exposure{}, BalanceResult{}, domain.UsageRecord{}, ErrInvalidLedgerInput, ErrUsageStoreUnavailable}
	for _, v := range values {
		text := stringify(v)
		for _, secret := range secrets {
			if strings.Contains(text, secret) {
				t.Fatalf("leaked %s in %T", secret, v)
			}
		}
	}
}

func ptr(s string) *string { return &s }
func stringify(v any) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(reflect.ValueOf(v).String(), "\n", " "), "\t", " "))
}

func TestValidateChargeBatchCanonicalModelBalanceAndUTC(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	chargedAt := time.Unix(2, 0).UTC()
	balance := int64(0)
	batch := domain.BillingChargeBatch{
		ID:                          "billchg_" + strings.Repeat("a", 64),
		UserID:                      "u",
		BillingSubjectUserID:        "billing",
		ProviderType:                domain.ProviderOpenAI,
		ClientModel:                 "gpt",
		BillingModel:                "openai:gpt",
		InputTokens:                 1,
		OutputTokens:                2,
		AmountCents:                 3,
		Currency:                    "RUB",
		Status:                      domain.BillingChargeStatusSucceeded,
		BillingResponseBalanceCents: &balance,
		CreatedAt:                   now,
		ChargedAt:                   &chargedAt,
		UpdatedAt:                   chargedAt,
	}
	if err := ValidateChargeBatch(batch); err != nil {
		t.Fatalf("valid batch err=%v", err)
	}
	badModel := batch
	badModel.BillingModel = "wrong:gpt"
	if err := ValidateChargeBatch(badModel); err == nil {
		t.Fatal("bad billing model accepted")
	}
	badBalance := batch
	negative := int64(-1)
	badBalance.BillingResponseBalanceCents = &negative
	if err := ValidateChargeBatch(badBalance); err == nil {
		t.Fatal("negative response balance accepted")
	}
	badTime := batch
	local := time.Unix(1, 0)
	badTime.CreatedAt = local
	if err := ValidateChargeBatch(badTime); err == nil {
		t.Fatal("non-UTC timestamp accepted")
	}
}
