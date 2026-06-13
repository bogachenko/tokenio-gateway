package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type stage10MajorUserRepoFake struct {
	mu   sync.Mutex
	user *domain.User
}

func (f *stage10MajorUserRepoFake) FindByID(context.Context, string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.user == nil {
		return nil, ports.ErrNotFound
	}
	copy := *f.user
	return &copy, nil
}
func (f *stage10MajorUserRepoFake) ListUsers(context.Context, ports.UserListFilter) (ports.Page[domain.User], error) {
	return ports.Page[domain.User]{}, nil
}
func (f *stage10MajorUserRepoFake) CreateUserWithAudit(context.Context, domain.User, domain.AuditContext) (domain.User, error) {
	return domain.User{}, nil
}
func (f *stage10MajorUserRepoFake) CompareAndSwapUserWithAudit(context.Context, domain.User, domain.User, domain.AuditContext) (domain.User, error) {
	return domain.User{}, nil
}

type stage10MajorAPIKeyRepoFake struct {
	mu          sync.Mutex
	createErr   error
	createCalls int
	record      domain.APIKeyRecord
	audit       domain.AuditContext
}

func (f *stage10MajorAPIKeyRepoFake) FindByHash(context.Context, string) (*domain.APIKeyRecord, error) {
	return nil, ports.ErrNotFound
}
func (f *stage10MajorAPIKeyRepoFake) FindAPIKeyByID(context.Context, string) (*domain.APIKeyRecord, error) {
	return nil, ports.ErrNotFound
}
func (f *stage10MajorAPIKeyRepoFake) ListAPIKeys(context.Context, ports.APIKeyListFilter) (ports.Page[domain.APIKeyRecord], error) {
	return ports.Page[domain.APIKeyRecord]{}, nil
}
func (f *stage10MajorAPIKeyRepoFake) CreateAPIKeyWithAudit(_ context.Context, record domain.APIKeyRecord, audit domain.AuditContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.record = record
	f.audit = audit
	return f.createErr
}
func (f *stage10MajorAPIKeyRepoFake) CompareAndSwapAPIKeyWithAudit(context.Context, domain.APIKeyRecord, domain.APIKeyRecord, domain.AuditContext) (domain.APIKeyRecord, error) {
	return domain.APIKeyRecord{}, nil
}

type stage10MajorFixedKeyGenerator struct {
	mu  sync.Mutex
	raw string
	err error
}

func (f *stage10MajorFixedKeyGenerator) GenerateAPIKey() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.raw, f.err
}

type stage10MajorFixedHasher struct {
	mu   sync.Mutex
	hash string
	raws []string
}

func (f *stage10MajorFixedHasher) Hash(raw string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.raws = append(f.raws, raw)
	return f.hash
}

type stage10MajorAdminClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *stage10MajorAdminClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func TestCreateAPIKeyReturnsRawOnlyAfterAtomicCommit(t *testing.T) {
	at := time.Unix(100, 123).UTC()
	expires := at.Add(time.Hour)
	raw := "sk_live_" + strings.Repeat("A", 43)
	hash := strings.Repeat("b", 64)
	users := &stage10MajorUserRepoFake{user: &domain.User{ID: "usr_1", Enabled: true}}
	keys := &stage10MajorAPIKeyRepoFake{}
	hasher := &stage10MajorFixedHasher{hash: hash}
	service := &Service{deps: Dependencies{
		Users:        users,
		APIKeys:      keys,
		KeyGenerator: &stage10MajorFixedKeyGenerator{raw: raw},
		Hasher:       hasher,
		Clock:        &stage10MajorAdminClock{now: at},
	}}

	result, err := service.CreateAPIKey(context.Background(), CommandContext{RequestID: "admreq_1", AdminSubject: "admin_token"}, CreateAPIKeyInput{
		UserID:    "usr_1",
		Name:      "Laptop",
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.APIKey != raw {
		t.Fatalf("returned key = %q", result.APIKey)
	}

	keys.mu.Lock()
	defer keys.mu.Unlock()
	if keys.createCalls != 1 {
		t.Fatalf("create calls = %d", keys.createCalls)
	}
	if keys.record.KeyHash != hash || keys.record.KeyPrefix == raw || strings.Contains(keys.record.KeyPrefix, raw) {
		t.Fatalf("persisted record = %+v", keys.record)
	}
	if keys.record.ExpiresAt == nil || !keys.record.ExpiresAt.Equal(expires) {
		t.Fatalf("expires_at = %v", keys.record.ExpiresAt)
	}
	if _, ok := keys.audit.AfterState["key_hash"]; ok {
		t.Fatal("key_hash leaked into audit")
	}
	auditJSON, err := json.Marshal(keys.audit)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(auditJSON), raw) || strings.Contains(string(auditJSON), hash) {
		t.Fatalf("secret leaked into audit: %s", auditJSON)
	}
}

func TestCreateAPIKeyPersistenceFailureDoesNotReturnRawKey(t *testing.T) {
	raw := "sk_live_" + strings.Repeat("A", 43)
	keys := &stage10MajorAPIKeyRepoFake{createErr: errors.New("store down")}
	service := &Service{deps: Dependencies{
		Users:        &stage10MajorUserRepoFake{user: &domain.User{ID: "usr_1", Enabled: true}},
		APIKeys:      keys,
		KeyGenerator: &stage10MajorFixedKeyGenerator{raw: raw},
		Hasher:       &stage10MajorFixedHasher{hash: strings.Repeat("b", 64)},
		Clock:        &stage10MajorAdminClock{now: time.Unix(100, 0).UTC()},
	}}
	result, err := service.CreateAPIKey(context.Background(), CommandContext{RequestID: "admreq_1", AdminSubject: "admin_token"}, CreateAPIKeyInput{UserID: "usr_1", Name: "Laptop"})
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if result.APIKey != "" {
		t.Fatalf("raw key returned on failed commit: %q", result.APIKey)
	}
}
