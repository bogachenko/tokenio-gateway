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

type stage10V5UserRepoFake struct {
	mu   sync.Mutex
	user *domain.User
}

func (f *stage10V5UserRepoFake) FindByID(context.Context, string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.user == nil {
		return nil, ports.ErrNotFound
	}
	copy := *f.user
	return &copy, nil
}
func (f *stage10V5UserRepoFake) ListUsers(context.Context, ports.UserListFilter) (ports.Page[domain.User], error) {
	return ports.Page[domain.User]{}, nil
}
func (f *stage10V5UserRepoFake) CreateUserWithAudit(context.Context, domain.User, domain.AuditContext) (domain.User, error) {
	return domain.User{}, nil
}
func (f *stage10V5UserRepoFake) CompareAndSwapUserWithAudit(context.Context, domain.User, domain.User, domain.AuditContext) (domain.User, error) {
	return domain.User{}, nil
}

type stage10V5APIKeyRepoFake struct {
	mu          sync.Mutex
	createErr   error
	createCalls int
	record      domain.APIKeyRecord
	audit       domain.AuditContext
}

func (f *stage10V5APIKeyRepoFake) FindByHash(context.Context, string) (*domain.APIKeyRecord, error) {
	return nil, ports.ErrNotFound
}
func (f *stage10V5APIKeyRepoFake) FindAPIKeyByID(context.Context, string) (*domain.APIKeyRecord, error) {
	return nil, ports.ErrNotFound
}
func (f *stage10V5APIKeyRepoFake) ListAPIKeys(context.Context, ports.APIKeyListFilter) (ports.Page[domain.APIKeyRecord], error) {
	return ports.Page[domain.APIKeyRecord]{}, nil
}
func (f *stage10V5APIKeyRepoFake) CreateAPIKeyWithAudit(_ context.Context, record domain.APIKeyRecord, audit domain.AuditContext) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.record = record
	f.audit = audit
	return f.createErr
}
func (f *stage10V5APIKeyRepoFake) CompareAndSwapAPIKeyWithAudit(context.Context, domain.APIKeyRecord, domain.APIKeyRecord, domain.AuditContext) (domain.APIKeyRecord, error) {
	return domain.APIKeyRecord{}, nil
}

type stage10V5FixedKeyGenerator struct {
	mu  sync.Mutex
	raw string
	err error
}

func (f *stage10V5FixedKeyGenerator) GenerateAPIKey() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.raw, f.err
}

type stage10V5FixedHasher struct {
	mu   sync.Mutex
	hash string
	raws []string
}

func (f *stage10V5FixedHasher) Hash(raw string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.raws = append(f.raws, raw)
	return f.hash
}

type stage10V5AdminClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *stage10V5AdminClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func TestStage10V5CreateAPIKeyReturnsRawOnlyAfterAtomicCommit(t *testing.T) {
	at := time.Unix(100, 123).UTC()
	expires := at.Add(time.Hour)
	raw := "sk_live_" + strings.Repeat("A", 43)
	hash := strings.Repeat("b", 64)
	users := &stage10V5UserRepoFake{user: &domain.User{ID: "usr_1", Enabled: true}}
	keys := &stage10V5APIKeyRepoFake{}
	hasher := &stage10V5FixedHasher{hash: hash}
	service := &Service{deps: Dependencies{
		Users:        users,
		APIKeys:      keys,
		KeyGenerator: &stage10V5FixedKeyGenerator{raw: raw},
		Hasher:       hasher,
		Clock:        &stage10V5AdminClock{now: at},
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

func TestStage10V5CreateAPIKeyPersistenceFailureDoesNotReturnRawKey(t *testing.T) {
	raw := "sk_live_" + strings.Repeat("A", 43)
	keys := &stage10V5APIKeyRepoFake{createErr: errors.New("store down")}
	service := &Service{deps: Dependencies{
		Users:        &stage10V5UserRepoFake{user: &domain.User{ID: "usr_1", Enabled: true}},
		APIKeys:      keys,
		KeyGenerator: &stage10V5FixedKeyGenerator{raw: raw},
		Hasher:       &stage10V5FixedHasher{hash: strings.Repeat("b", 64)},
		Clock:        &stage10V5AdminClock{now: time.Unix(100, 0).UTC()},
	}}
	result, err := service.CreateAPIKey(context.Background(), CommandContext{RequestID: "admreq_1", AdminSubject: "admin_token"}, CreateAPIKeyInput{UserID: "usr_1", Name: "Laptop"})
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if result.APIKey != "" {
		t.Fatalf("raw key returned on failed commit: %q", result.APIKey)
	}
}
