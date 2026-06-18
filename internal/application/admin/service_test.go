package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type fixedClock struct{ value time.Time }

func (c fixedClock) Now() time.Time { return c.value }

type fakeUsers struct {
	ports.AdminUserRepository
	mu   sync.Mutex
	user *domain.User
}

func (f *fakeUsers) FindByID(context.Context, string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.user == nil {
		return nil, ports.ErrNotFound
	}
	copied := *f.user
	return &copied, nil
}

type fakeKeys struct {
	ports.AdminAPIKeyRepository
	mu        sync.Mutex
	created   domain.APIKeyRecord
	audit     domain.AuditContext
	createErr error
}

func (f *fakeKeys) CreateAPIKeyWithAudit(_ context.Context, record domain.APIKeyRecord, audit domain.AuditContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.created = record
	f.audit = audit
	return nil
}

type fakeResellers struct {
	ports.ResellerRepository
	mu       sync.Mutex
	reseller *domain.Reseller
	items    []domain.Reseller
	audit    domain.AuditContext
}

func (f *fakeResellers) FindResellerByID(context.Context, string) (*domain.Reseller, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reseller == nil {
		return nil, ports.ErrNotFound
	}
	copied := *f.reseller
	return &copied, nil
}
func (f *fakeResellers) ListResellers(context.Context, ports.ResellerListFilter) (ports.Page[domain.Reseller], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return ports.Page[domain.Reseller]{Items: append([]domain.Reseller(nil), f.items...), Total: int64(len(f.items))}, nil
}
func (f *fakeResellers) CompareAndSwapResellerWithAudit(_ context.Context, expected, next domain.Reseller, audit domain.AuditContext) (domain.Reseller, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reseller == nil || *f.reseller != expected {
		return domain.Reseller{}, errors.New("cas")
	}
	copied := next
	f.reseller = &copied
	f.audit = audit
	return next, nil
}

type fakeRoutes struct {
	ports.AdminRouteRepository
	mu          sync.Mutex
	createCalls int
}

func (f *fakeRoutes) CreateRouteWithAudit(context.Context, domain.Route, domain.AuditContext) (domain.Route, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	return domain.Route{}, nil
}

type fakePrices struct {
	ports.AdminRoutePriceRepository
}

type fakePriceValidator struct {
	err error
}

func (f *fakePriceValidator) ValidateRoutePrice(
	domain.RoutePrice,
) error {
	return f.err
}

type fakeUsagePolicy struct {
	recordErr     error
	transitionErr error
}

func (f *fakeUsagePolicy) ValidateRecord(
	domain.UsageRecord,
) error {
	return f.recordErr
}

func (f *fakeUsagePolicy) ValidateTransition(
	domain.UsageStatus,
	domain.UsageStatus,
) error {
	return f.transitionErr
}

type fakeLedger struct {
	ports.AdminUsageLedger
	mu           sync.Mutex
	record       *domain.UsageRecord
	resolved     domain.UsageRecord
	audit        domain.AuditContext
	resolveCalls int
}

func (f *fakeLedger) FindByLocalRequestID(context.Context, string) (*domain.UsageRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.record == nil {
		return nil, ports.ErrNotFound
	}
	copied := *f.record
	return &copied, nil
}
func (f *fakeLedger) ResolvePricingFailedWithAudit(_ context.Context, expected, next domain.UsageRecord, audit domain.AuditContext) (ports.UsageTransitionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolveCalls++
	if f.record == nil || *f.record != expected {
		return ports.UsageTransitionResult{Applied: false}, nil
	}
	copied := next
	f.record = &copied
	f.resolved = next
	f.audit = audit
	return ports.UsageTransitionResult{Applied: true}, nil
}

type fakeAuditStore struct{ ports.AdminAuditStore }
type fakeSecrets struct {
	mu      sync.Mutex
	names   []string
	present bool
}

func (f *fakeSecrets) Exists(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names = append(f.names, name)
	return f.present, nil
}

type fakeAdapterSupport struct{}

func (*fakeAdapterSupport) SupportsForwardingAdapter(
	domain.APIFamily,
	domain.ProviderType,
	domain.EndpointKind,
) bool {
	return true
}

type fakeGenerator struct {
	raw string
	err error
}

func (f *fakeGenerator) GenerateAPIKey() (string, error) { return f.raw, f.err }

type fakeRetrier struct{}

func (*fakeRetrier) RetryFailedBatch(context.Context, string, domain.AuditContext) (domain.BillingChargeBatch, error) {
	return domain.BillingChargeBatch{}, nil
}

func newServiceForTest(t *testing.T, users *fakeUsers, keys *fakeKeys, resellers *fakeResellers, routes *fakeRoutes, ledgerStore *fakeLedger, secrets *fakeSecrets, generator *fakeGenerator) *Service {
	t.Helper()
	hasher, err := auth.NewAPIKeyHasher("hmac-secret")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Dependencies{Users: users, APIKeys: keys, RouteEvents: &routeEventStoreFake{},
		Provisionings: &fakeAdminProvisioningRepository{}, Resellers: resellers, Routes: routes, Prices: &fakePrices{}, PriceValidator: &fakePriceValidator{}, UsagePolicy: &fakeUsagePolicy{}, Ledger: ledgerStore, Audit: &fakeAuditStore{}, Secrets: secrets, AdapterSupport: &fakeAdapterSupport{}, KeyGenerator: generator, Hasher: hasher, Clock: fixedClock{value: time.Unix(100, 0).UTC()}, BatchRetrier: &fakeRetrier{}})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func validRawKey() string {
	return "sk_live_" + base64.RawURLEncoding.EncodeToString(bytesOf(32, 0x42))
}
func bytesOf(n int, value byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = value
	}
	return out
}
func command() CommandContext {
	return CommandContext{RequestID: "admreq_test", AdminSubject: "admin_token"}
}

func TestCreateAPIKeyPersistsOnlyHMACAndReturnsRawAfterCommit(t *testing.T) {
	users := &fakeUsers{user: &domain.User{ID: "usr_1", Enabled: true}}
	keys := &fakeKeys{}
	service := newServiceForTest(t, users, keys, &fakeResellers{}, &fakeRoutes{}, &fakeLedger{}, &fakeSecrets{}, &fakeGenerator{raw: validRawKey()})
	result, err := service.CreateAPIKey(nil, command(), CreateAPIKeyInput{UserID: "usr_1", Name: "Laptop key"})
	if err != nil {
		t.Fatal(err)
	}
	if result.APIKey != validRawKey() {
		t.Fatalf("raw=%q", result.APIKey)
	}
	keys.mu.Lock()
	persisted, audit := keys.created, keys.audit
	keys.mu.Unlock()
	if persisted.KeyHash == "" || persisted.KeyHash == result.APIKey || len(persisted.KeyHash) != 64 {
		t.Fatalf("persisted hash=%q", persisted.KeyHash)
	}
	if audit.Action != domain.AuditActionAPIKeyCreate {
		t.Fatalf("action=%q", audit.Action)
	}
	encoded, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, result.APIKey) || strings.Contains(text, persisted.KeyHash) || strings.Contains(text, "key_hash") {
		t.Fatalf("secret in audit: %s", text)
	}
}

func TestCreateAPIKeyDoesNotReturnRawWhenAtomicPersistenceFails(t *testing.T) {
	users := &fakeUsers{user: &domain.User{ID: "usr_1", Enabled: true}}
	keys := &fakeKeys{createErr: errors.New("store down")}
	service := newServiceForTest(t, users, keys, &fakeResellers{}, &fakeRoutes{}, &fakeLedger{}, &fakeSecrets{}, &fakeGenerator{raw: validRawKey()})
	result, err := service.CreateAPIKey(nil, command(), CreateAPIKeyInput{UserID: "usr_1", Name: "Laptop key"})
	if !errors.Is(err, ErrStoreUnavailable) || result.APIKey != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestSecretPresenceUsesExactEnvironmentNameAndReturnsOnlyBoolean(t *testing.T) {
	reseller := domain.Reseller{ID: "r1", APIKeyEnv: " EXACT_ENV ", CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}
	secrets := &fakeSecrets{present: true}
	service := newServiceForTest(t, &fakeUsers{}, &fakeKeys{}, &fakeResellers{items: []domain.Reseller{reseller}}, &fakeRoutes{}, &fakeLedger{}, secrets, &fakeGenerator{})
	result, err := service.ListResellers(nil, ResellerListInput{})
	if err != nil {
		t.Fatal(err)
	}
	secrets.mu.Lock()
	names := append([]string(nil), secrets.names...)
	secrets.mu.Unlock()
	if len(names) != 1 || names[0] != " EXACT_ENV " {
		t.Fatalf("names=%q", names)
	}
	if len(result.Data) != 1 || !result.Data[0].APIKeyEnvPresent {
		t.Fatalf("result=%+v", result)
	}
}

func TestRouteProviderMustMatchCanonicalReseller(t *testing.T) {
	reseller := domain.Reseller{ID: "r1", ProviderType: domain.ProviderOpenAI}
	routes := &fakeRoutes{}
	service := newServiceForTest(t, &fakeUsers{}, &fakeKeys{}, &fakeResellers{reseller: &reseller}, routes, &fakeLedger{}, &fakeSecrets{}, &fakeGenerator{})
	_, err := service.CreateRoute(nil, command(), domain.Route{ID: "route_1", ResellerID: "r1", ProviderType: domain.ProviderAnthropic})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
	routes.mu.Lock()
	calls := routes.createCalls
	routes.mu.Unlock()
	if calls != 0 {
		t.Fatalf("create calls=%d", calls)
	}
}

func TestResellerBalanceAdjustmentRejectsInt64OverflowBeforeMutation(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	reseller := domain.Reseller{ID: "r1", Name: "r", ProviderType: domain.ProviderOpenAI, BaseURL: "https://example", APIKeyEnv: "ENV", Enabled: true, BalanceCents: math.MaxInt64, CreatedAt: now, UpdatedAt: now}
	store := &fakeResellers{reseller: &reseller}
	service := newServiceForTest(t, &fakeUsers{}, &fakeKeys{}, store, &fakeRoutes{}, &fakeLedger{}, &fakeSecrets{}, &fakeGenerator{})
	_, err := service.AdjustResellerBalance(nil, command(), "r1", 1, "top up")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
	store.mu.Lock()
	got := store.reseller.BalanceCents
	store.mu.Unlock()
	if got != math.MaxInt64 {
		t.Fatalf("balance changed=%d", got)
	}
}

func TestPricingFailedResolutionUsesSingleAtomicCASAndExactAuditAction(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	current := domain.UsageRecord{LocalRequestID: "llmreq_1", UserID: "usr_1", ProviderType: domain.ProviderOpenAI, ClientModel: "model-a", BillingModel: "openai:model-a", Currency: "RUB", UsageCompleteness: "failed", Status: domain.UsageStatusPricingFailed, FailureReason: "pricing_failed", CreatedAt: now, UpdatedAt: now, FailedAt: &now}
	ledgerStore := &fakeLedger{record: &current}
	service := newServiceForTest(t, &fakeUsers{}, &fakeKeys{}, &fakeResellers{}, &fakeRoutes{}, ledgerStore, &fakeSecrets{}, &fakeGenerator{})
	batchID := "billchg_" + strings.Repeat("a", 64)
	resolved, err := service.ResolveUsageCharged(nil, command(), ResolveChargedInput{LocalRequestID: "llmreq_1", ChargedAmountCents: 123, BillingChargeRequestID: batchID, Reason: "external charge confirmed"})
	if err != nil {
		t.Fatal(err)
	}
	ledgerStore.mu.Lock()
	calls, audit := ledgerStore.resolveCalls, ledgerStore.audit
	ledgerStore.mu.Unlock()
	if calls != 1 || resolved.Status != domain.UsageStatusCharged || resolved.BillingChargeRequestID != batchID {
		t.Fatalf("calls=%d resolved=%+v", calls, resolved)
	}
	if audit.Action != domain.AuditActionUsageResolveCharged || audit.RequestID != command().RequestID {
		t.Fatalf("audit=%+v", audit)
	}
}

func TestManualResolutionRequiresReason(t *testing.T) {
	service := newServiceForTest(t, &fakeUsers{}, &fakeKeys{}, &fakeResellers{}, &fakeRoutes{}, &fakeLedger{}, &fakeSecrets{}, &fakeGenerator{})
	_, err := service.ResolveUsageFailed(nil, command(), ResolveFailedInput{LocalRequestID: "llmreq_1"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
}

func TestRequiredManualReasonsArePersistedInAuditContext(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	reseller := domain.Reseller{
		ID: "r1", Name: "r", ProviderType: domain.ProviderOpenAI,
		BaseURL: "https://example", APIKeyEnv: "ENV", Enabled: true,
		BalanceCents: 100, CreatedAt: now, UpdatedAt: now,
	}
	resellers := &fakeResellers{reseller: &reseller}
	service := newServiceForTest(t, &fakeUsers{}, &fakeKeys{}, resellers, &fakeRoutes{}, &fakeLedger{}, &fakeSecrets{}, &fakeGenerator{})
	const balanceReason = "manual reconciliation"
	if _, err := service.SetResellerBalance(context.Background(), command(), "r1", 200, balanceReason); err != nil {
		t.Fatal(err)
	}
	resellers.mu.Lock()
	balanceAudit := resellers.audit
	resellers.mu.Unlock()
	if balanceAudit.Reason != balanceReason {
		t.Fatalf("balance audit reason=%q", balanceAudit.Reason)
	}

	current := domain.UsageRecord{
		LocalRequestID: "llmreq_1", UserID: "usr_1",
		ProviderType: domain.ProviderOpenAI, ClientModel: "model-a",
		BillingModel: "openai:model-a", Currency: "RUB",
		UsageCompleteness: "failed", Status: domain.UsageStatusPricingFailed,
		FailureReason: "pricing_failed", CreatedAt: now, UpdatedAt: now,
		FailedAt: &now,
	}
	ledgerStore := &fakeLedger{record: &current}
	service = newServiceForTest(t, &fakeUsers{}, &fakeKeys{}, &fakeResellers{}, &fakeRoutes{}, ledgerStore, &fakeSecrets{}, &fakeGenerator{})
	const usageReason = "manual write-off"
	if _, err := service.ResolveUsageFailed(context.Background(), command(), ResolveFailedInput{LocalRequestID: "llmreq_1", Reason: usageReason}); err != nil {
		t.Fatal(err)
	}
	ledgerStore.mu.Lock()
	usageAudit := ledgerStore.audit
	ledgerStore.mu.Unlock()
	if usageAudit.Reason != usageReason {
		t.Fatalf("usage audit reason=%q", usageAudit.Reason)
	}
}
