package admin

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type stage10MajorResellerRepoFake struct {
	mu          sync.Mutex
	current     *domain.Reseller
	createCalls int
	casCalls    int
}

func (f *stage10MajorResellerRepoFake) FindResellerByID(context.Context, string) (*domain.Reseller, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.current == nil {
		return nil, ports.ErrNotFound
	}
	copy := *f.current
	return &copy, nil
}
func (f *stage10MajorResellerRepoFake) ListResellers(context.Context, ports.ResellerListFilter) (ports.Page[domain.Reseller], error) {
	return ports.Page[domain.Reseller]{}, nil
}
func (f *stage10MajorResellerRepoFake) CreateResellerWithAudit(_ context.Context, reseller domain.Reseller, _ domain.AuditContext) (domain.Reseller, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.current = &reseller
	return reseller, nil
}
func (f *stage10MajorResellerRepoFake) CompareAndSwapResellerWithAudit(_ context.Context, _ domain.Reseller, next domain.Reseller, _ domain.AuditContext) (domain.Reseller, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.casCalls++
	f.current = &next
	return next, nil
}

type stage10MajorSecretPresenceFake struct {
	mu      sync.Mutex
	present bool
	err     error
	calls   []string
}

func (f *stage10MajorSecretPresenceFake) Exists(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	return f.present, f.err
}

func stage10MajorValidReseller() domain.Reseller {
	return domain.Reseller{
		ID:                  "reseller_1",
		Name:                "Primary",
		ProviderType:        domain.ProviderOpenAI,
		BaseURL:             "https://provider.example",
		APIKeyEnv:           "PROVIDER_KEY",
		Enabled:             true,
		BalanceCents:        1000,
		ReservedCents:       0,
		MinimumBalanceCents: 100,
	}
}

func TestCreateResellerChecksSecretPresenceBeforeCommit(t *testing.T) {
	repo := &stage10MajorResellerRepoFake{}
	secrets := &stage10MajorSecretPresenceFake{err: errors.New("diagnostic unavailable")}
	service := &Service{deps: Dependencies{
		Resellers: repo,
		Secrets:   secrets,
		Clock:     &stage10MajorAdminClock{now: time.Unix(100, 0).UTC()},
	}}
	_, err := service.CreateReseller(context.Background(), CommandContext{RequestID: "admreq_1", AdminSubject: "admin_token"}, CreateResellerInput{Reseller: stage10MajorValidReseller()})
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("error = %v", err)
	}
	repo.mu.Lock()
	createCalls := repo.createCalls
	repo.mu.Unlock()
	if createCalls != 0 {
		t.Fatalf("mutation committed before secret diagnostic: %d", createCalls)
	}

	secrets.mu.Lock()
	secrets.err = nil
	secrets.present = true
	secrets.calls = nil
	secrets.mu.Unlock()
	view, err := service.CreateReseller(context.Background(), CommandContext{RequestID: "admreq_2", AdminSubject: "admin_token"}, CreateResellerInput{Reseller: stage10MajorValidReseller()})
	if err != nil {
		t.Fatal(err)
	}
	if !view.APIKeyEnvPresent {
		t.Fatal("secret presence boolean was not returned")
	}
	secrets.mu.Lock()
	calls := append([]string(nil), secrets.calls...)
	secrets.mu.Unlock()
	if len(calls) != 1 || calls[0] != "PROVIDER_KEY" {
		t.Fatalf("secret diagnostic calls = %v", calls)
	}
}

func TestUpdateResellerSecretFailureCannotFollowCommittedMutation(t *testing.T) {
	current := stage10MajorValidReseller()
	current.CreatedAt = time.Unix(1, 0).UTC()
	current.UpdatedAt = current.CreatedAt
	repo := &stage10MajorResellerRepoFake{current: &current}
	secrets := &stage10MajorSecretPresenceFake{err: errors.New("diagnostic unavailable")}
	service := &Service{deps: Dependencies{
		Resellers: repo,
		Secrets:   secrets,
		Clock:     &stage10MajorAdminClock{now: time.Unix(100, 0).UTC()},
	}}
	name := "Updated"
	_, err := service.UpdateReseller(context.Background(), CommandContext{RequestID: "admreq_1", AdminSubject: "admin_token"}, UpdateResellerInput{ID: current.ID, Name: &name})
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("error = %v", err)
	}
	repo.mu.Lock()
	casCalls := repo.casCalls
	repo.mu.Unlock()
	if casCalls != 0 {
		t.Fatalf("mutation committed before secret diagnostic: %d", casCalls)
	}
}

func TestBalanceOverflowRejectedBeforePersistence(t *testing.T) {
	current := stage10MajorValidReseller()
	current.BalanceCents = math.MinInt64
	current.ReservedCents = 1
	current.MinimumBalanceCents = 0
	current.CreatedAt = time.Unix(1, 0).UTC()
	current.UpdatedAt = current.CreatedAt
	repo := &stage10MajorResellerRepoFake{current: &current}
	service := &Service{deps: Dependencies{
		Resellers: repo,
		Clock:     &stage10MajorAdminClock{now: time.Unix(100, 0).UTC()},
	}}
	_, err := service.AdjustResellerBalance(context.Background(), CommandContext{RequestID: "admreq_1", AdminSubject: "admin_token"}, current.ID, 0, "reconcile")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("adjust error = %v", err)
	}
	repo.mu.Lock()
	casCalls := repo.casCalls
	repo.mu.Unlock()
	if casCalls != 0 {
		t.Fatalf("overflowing balance persisted: %d", casCalls)
	}
}
