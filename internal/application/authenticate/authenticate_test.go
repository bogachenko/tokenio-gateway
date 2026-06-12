package authenticate

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const (
	testSecret = "test-secret"
	testRawKey = "sk_test_public_key"
)

type apiKeyRepositoryFake struct {
	record       *domain.APIKeyRecord
	err          error
	receivedHash string
	calls        int
}

func (f *apiKeyRepositoryFake) FindByHash(ctx context.Context, keyHash string) (*domain.APIKeyRecord, error) {
	f.calls++
	f.receivedHash = keyHash
	if f.err != nil {
		return nil, f.err
	}
	return f.record, nil
}

type userRepositoryFake struct {
	user       *domain.User
	err        error
	receivedID string
	calls      int
}

func (f *userRepositoryFake) FindByID(ctx context.Context, userID string) (*domain.User, error) {
	f.calls++
	f.receivedID = userID
	if f.err != nil {
		return nil, f.err
	}
	return f.user, nil
}

type clockFake struct {
	now   time.Time
	calls int
}

func (f *clockFake) Now() time.Time {
	f.calls++
	return f.now
}

func TestAuthenticatePublicRequestValidKeyReturnsPrincipal(t *testing.T) {
	fixedNow := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	expiresAt := fixedNow.Add(time.Hour)
	keyRepo := &apiKeyRepositoryFake{record: validKeyRecord(&expiresAt)}
	userRepo := &userRepositoryFake{user: validUser()}
	clock := &clockFake{now: fixedNow}
	usecase := mustUseCase(t, keyRepo, userRepo, clock)

	result, err := usecase.AuthenticatePublicRequest(context.Background(), Input{RawAPIKey: testRawKey})
	if err != nil {
		t.Fatalf("AuthenticatePublicRequest returned error: %v", err)
	}

	want := auth.APIKeyPrincipal{
		UserID:               "usr_1",
		APIKeyID:             "ak_1",
		BillingSubjectUserID: "billing_usr_1",
	}
	if result.Principal != want {
		t.Fatalf("principal = %#v, want %#v", result.Principal, want)
	}
	assertPrincipalHasNoBillingTokenField(t, result.Principal)
	assertRepositoryReceivedExpectedHash(t, keyRepo.receivedHash)
	if keyRepo.receivedHash == testRawKey {
		t.Fatal("repository received raw API key instead of HMAC hash")
	}
	if clock.calls != 1 {
		t.Fatalf("clock calls = %d, want 1", clock.calls)
	}
	if userRepo.receivedID != "usr_1" {
		t.Fatalf("user lookup id = %q, want usr_1", userRepo.receivedID)
	}
}

func TestAuthenticatePublicRequestEmptyRawKeyRejected(t *testing.T) {
	keyRepo := &apiKeyRepositoryFake{record: validKeyRecord(nil)}
	userRepo := &userRepositoryFake{user: validUser()}
	usecase := mustUseCase(t, keyRepo, userRepo, &clockFake{})

	_, err := usecase.AuthenticatePublicRequest(context.Background(), Input{RawAPIKey: "   "})
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("error = %v, want ErrInvalidAPIKey", err)
	}
	if keyRepo.calls != 0 || userRepo.calls != 0 {
		t.Fatalf("repositories were called for empty key: key=%d user=%d", keyRepo.calls, userRepo.calls)
	}
}

func TestAuthenticatePublicRequestUnknownKey(t *testing.T) {
	usecase := mustUseCase(t, &apiKeyRepositoryFake{err: ports.ErrNotFound}, &userRepositoryFake{user: validUser()}, &clockFake{})

	_, err := usecase.AuthenticatePublicRequest(context.Background(), Input{RawAPIKey: testRawKey})
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("error = %v, want ErrInvalidAPIKey", err)
	}
}

func TestAuthenticatePublicRequestWrappedNotFoundMapsToInvalidAPIKey(t *testing.T) {
	usecase := mustUseCase(t, &apiKeyRepositoryFake{err: fmt.Errorf("query api key: %w", ports.ErrNotFound)}, &userRepositoryFake{user: validUser()}, &clockFake{})

	_, err := usecase.AuthenticatePublicRequest(context.Background(), Input{RawAPIKey: testRawKey})
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("error = %v, want ErrInvalidAPIKey", err)
	}
}

func TestAuthenticatePublicRequestDisabledKey(t *testing.T) {
	key := validKeyRecord(nil)
	key.Enabled = false
	assertAuthError(t, key, validUser(), nil, nil, ErrInvalidAPIKey)
}

func TestAuthenticatePublicRequestRevokedKey(t *testing.T) {
	revokedAt := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	key := validKeyRecord(nil)
	key.RevokedAt = &revokedAt
	assertAuthError(t, key, validUser(), nil, nil, ErrInvalidAPIKey)
}

func TestAuthenticatePublicRequestExpiredKey(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	expiresAt := now.Add(-time.Nanosecond)
	key := validKeyRecord(&expiresAt)
	assertAuthError(t, key, validUser(), &clockFake{now: now}, nil, ErrInvalidAPIKey)
}

func TestAuthenticatePublicRequestKeyExpiringInFutureAccepted(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Nanosecond)
	clock := &clockFake{now: now}
	usecase := mustUseCase(t, &apiKeyRepositoryFake{record: validKeyRecord(&expiresAt)}, &userRepositoryFake{user: validUser()}, clock)

	_, err := usecase.AuthenticatePublicRequest(context.Background(), Input{RawAPIKey: testRawKey})
	if err != nil {
		t.Fatalf("AuthenticatePublicRequest returned error: %v", err)
	}
	if clock.calls != 1 {
		t.Fatalf("clock calls = %d, want 1", clock.calls)
	}
}

func TestAuthenticatePublicRequestMissingUser(t *testing.T) {
	assertAuthError(t, validKeyRecord(nil), nil, nil, ports.ErrNotFound, ErrInvalidAPIKey)
}

func TestAuthenticatePublicRequestDisabledUser(t *testing.T) {
	user := validUser()
	user.Enabled = false
	assertAuthError(t, validKeyRecord(nil), user, nil, nil, ErrUserDisabled)
}

func TestAuthenticatePublicRequestEmptyExternalBillingUserID(t *testing.T) {
	user := validUser()
	user.ExternalBillingUserID = ""
	assertAuthError(t, validKeyRecord(nil), user, nil, nil, ErrInvalidIdentity)
}

func TestAuthenticatePublicRequestAPIKeyRepositoryFailureIsWrapped(t *testing.T) {
	infraErr := errors.New("key store unavailable")
	usecase := mustUseCase(t, &apiKeyRepositoryFake{err: infraErr}, &userRepositoryFake{user: validUser()}, &clockFake{})

	_, err := usecase.AuthenticatePublicRequest(context.Background(), Input{RawAPIKey: testRawKey})
	if !errors.Is(err, infraErr) {
		t.Fatalf("error = %v, want wrapped infrastructure error", err)
	}
	if errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("infrastructure error should not map to ErrInvalidAPIKey: %v", err)
	}
}

func TestAuthenticatePublicRequestUserRepositoryFailureIsWrapped(t *testing.T) {
	infraErr := errors.New("user store unavailable")
	usecase := mustUseCase(t, &apiKeyRepositoryFake{record: validKeyRecord(nil)}, &userRepositoryFake{err: infraErr}, &clockFake{})

	_, err := usecase.AuthenticatePublicRequest(context.Background(), Input{RawAPIKey: testRawKey})
	if !errors.Is(err, infraErr) {
		t.Fatalf("error = %v, want wrapped infrastructure error", err)
	}
	if errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("infrastructure error should not map to ErrInvalidAPIKey: %v", err)
	}
}

func TestAuthenticatePublicRequestRepositoryReceivesExpectedHMACHash(t *testing.T) {
	keyRepo := &apiKeyRepositoryFake{record: validKeyRecord(nil)}
	usecase := mustUseCase(t, keyRepo, &userRepositoryFake{user: validUser()}, &clockFake{})

	_, err := usecase.AuthenticatePublicRequest(context.Background(), Input{RawAPIKey: testRawKey})
	if err != nil {
		t.Fatalf("AuthenticatePublicRequest returned error: %v", err)
	}
	assertRepositoryReceivedExpectedHash(t, keyRepo.receivedHash)
}

func TestAuthenticatePublicRequestUsesInjectedClockInsteadOfWallClock(t *testing.T) {
	realPast := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	injectedNow := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	key := validKeyRecord(&realPast)
	clock := &clockFake{now: injectedNow}
	usecase := mustUseCase(t, &apiKeyRepositoryFake{record: key}, &userRepositoryFake{user: validUser()}, clock)

	_, err := usecase.AuthenticatePublicRequest(context.Background(), Input{RawAPIKey: testRawKey})
	if err != nil {
		t.Fatalf("key should be valid relative to injected clock: %v", err)
	}
	if clock.calls != 1 {
		t.Fatalf("clock calls = %d, want 1", clock.calls)
	}
}

func TestNewUseCaseRejectsNilDependencies(t *testing.T) {
	hasher := mustHasher(t)
	keyRepo := &apiKeyRepositoryFake{}
	userRepo := &userRepositoryFake{}
	clock := &clockFake{}

	cases := []struct {
		name   string
		hasher *auth.APIKeyHasher
		keys   ports.APIKeyRepository
		users  ports.UserRepository
		clock  ports.Clock
	}{
		{name: "hasher", keys: keyRepo, users: userRepo, clock: clock},
		{name: "keys", hasher: hasher, users: userRepo, clock: clock},
		{name: "users", hasher: hasher, keys: keyRepo, clock: clock},
		{name: "clock", hasher: hasher, keys: keyRepo, users: userRepo},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewUseCase(tc.hasher, tc.keys, tc.users, tc.clock); err == nil {
				t.Fatal("NewUseCase returned nil error")
			}
		})
	}
}

func assertAuthError(t *testing.T, key *domain.APIKeyRecord, user *domain.User, clock *clockFake, userErr error, want error) {
	t.Helper()
	if clock == nil {
		clock = &clockFake{now: time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)}
	}
	usecase := mustUseCase(t, &apiKeyRepositoryFake{record: key}, &userRepositoryFake{user: user, err: userErr}, clock)

	_, err := usecase.AuthenticatePublicRequest(context.Background(), Input{RawAPIKey: testRawKey})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func assertPrincipalHasNoBillingTokenField(t *testing.T, principal auth.APIKeyPrincipal) {
	t.Helper()
	if _, ok := reflect.TypeOf(principal).FieldByName("Billing" + "JWT"); ok {
		t.Fatal("principal contains billing token field")
	}
}

func assertRepositoryReceivedExpectedHash(t *testing.T, got string) {
	t.Helper()
	want := expectedHMACSHA256Hex(testSecret, testRawKey)
	if got != want {
		t.Fatalf("repository hash = %q, want %q", got, want)
	}
}

func mustUseCase(t *testing.T, keyRepo ports.APIKeyRepository, userRepo ports.UserRepository, clock ports.Clock) *UseCase {
	t.Helper()
	usecase, err := NewUseCase(mustHasher(t), keyRepo, userRepo, clock)
	if err != nil {
		t.Fatalf("NewUseCase returned error: %v", err)
	}
	return usecase
}

func mustHasher(t *testing.T) *auth.APIKeyHasher {
	t.Helper()
	hasher, err := auth.NewAPIKeyHasher(testSecret)
	if err != nil {
		t.Fatalf("NewAPIKeyHasher returned error: %v", err)
	}
	return hasher
}

func validKeyRecord(expiresAt *time.Time) *domain.APIKeyRecord {
	return &domain.APIKeyRecord{
		ID:        "ak_1",
		UserID:    "usr_1",
		KeyHash:   expectedHMACSHA256Hex(testSecret, testRawKey),
		Enabled:   true,
		ExpiresAt: expiresAt,
	}
}

func validUser() *domain.User {
	return &domain.User{
		ID:                    "usr_1",
		ExternalBillingUserID: "billing_usr_1",
		Enabled:               true,
	}
}

func expectedHMACSHA256Hex(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
