package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type testClock struct {
	now time.Time
}

func (c testClock) Now() time.Time {
	return c.now
}

type testMaterialFactory struct {
	create func(
		context.Context,
		ports.APIKeyProvisioningMaterialRequest,
	) (ports.APIKeyProvisioningMaterial, error)
	calls int
}

func (f *testMaterialFactory) CreateProvisioningMaterial(
	ctx context.Context,
	request ports.APIKeyProvisioningMaterialRequest,
) (ports.APIKeyProvisioningMaterial, error) {
	f.calls++
	if f.create == nil {
		return ports.APIKeyProvisioningMaterial{}, nil
	}
	return f.create(ctx, request)
}

type testMaterialDecryptor struct {
	raw   string
	err   error
	calls int
}

func (d *testMaterialDecryptor) DecryptProvisioningMaterial(
	context.Context,
	domain.APIKeyProvisioning,
	domain.APIKeyRecord,
) (string, error) {
	d.calls++
	return d.raw, d.err
}

type testProvisioningStore struct {
	provision func(
		context.Context,
		ports.APIKeyProvisioningRequest,
		ports.APIKeyProvisioningMaterialFactory,
	) (ports.APIKeyProvisioningResult, error)
	find func(
		context.Context,
		string,
	) (*domain.APIKeyProvisioning, error)
	recordAttempt func(
		context.Context,
		string,
		time.Time,
	) (domain.APIKeyProvisioning, error)
	confirm func(
		context.Context,
		string,
		time.Time,
	) (domain.APIKeyProvisioning, error)
	expire func(
		context.Context,
		string,
		time.Time,
	) (domain.APIKeyProvisioning, error)

	provisionCalls     int
	findCalls          int
	recordAttemptCalls int
	confirmCalls       int
	expireCalls        int

	lastRequest      ports.APIKeyProvisioningRequest
	lastProvisioning domain.APIKeyProvisioning
	lastAttemptID    string
	lastAttemptAt    time.Time
}

func (s *testProvisioningStore) ProvisionAPIKey(
	ctx context.Context,
	request ports.APIKeyProvisioningRequest,
	factory ports.APIKeyProvisioningMaterialFactory,
) (ports.APIKeyProvisioningResult, error) {
	s.provisionCalls++
	s.lastRequest = request
	if s.provision == nil {
		return ports.APIKeyProvisioningResult{}, nil
	}
	result, err := s.provision(ctx, request, factory)
	if err == nil {
		s.lastProvisioning = result.Provisioning
	}
	return result, err
}

func (s *testProvisioningStore) FindAPIKeyProvisioningByID(
	ctx context.Context,
	id string,
) (*domain.APIKeyProvisioning, error) {
	s.findCalls++
	if s.find == nil {
		return nil, ports.ErrNotFound
	}
	return s.find(ctx, id)
}

func (s *testProvisioningStore) FindAPIKeyProvisioningByIdempotencyKey(
	context.Context,
	string,
) (*domain.APIKeyProvisioning, error) {
	return nil, ports.ErrNotFound
}

func (s *testProvisioningStore) RecordAPIKeyDeliveryAttempt(
	ctx context.Context,
	id string,
	at time.Time,
) (domain.APIKeyProvisioning, error) {
	s.recordAttemptCalls++
	s.lastAttemptID = id
	s.lastAttemptAt = at
	if s.recordAttempt != nil {
		return s.recordAttempt(ctx, id, at)
	}

	updated := s.lastProvisioning
	updated.DeliveryAttempts++
	updated.UpdatedAt = at
	return updated, nil
}

func (s *testProvisioningStore) ConfirmAPIKeyDelivery(
	ctx context.Context,
	id string,
	at time.Time,
) (domain.APIKeyProvisioning, error) {
	s.confirmCalls++
	if s.confirm == nil {
		return domain.APIKeyProvisioning{},
			ports.ErrStoreUnavailable
	}
	return s.confirm(ctx, id, at)
}

func (s *testProvisioningStore) ListPendingAPIKeyProvisioningsDue(
	context.Context,
	time.Time,
	int,
) ([]domain.APIKeyProvisioning, error) {
	return nil, nil
}

func (s *testProvisioningStore) ExpireAPIKeyProvisioning(
	ctx context.Context,
	id string,
	at time.Time,
) (domain.APIKeyProvisioning, error) {
	s.expireCalls++
	if s.expire == nil {
		return domain.APIKeyProvisioning{},
			ports.ErrStoreUnavailable
	}
	return s.expire(ctx, id, at)
}

func fixedTime() time.Time {
	return time.Date(
		2026,
		time.June,
		13,
		12,
		0,
		0,
		0,
		time.UTC,
	)
}

func validInput() ProvisionInput {
	return ProvisionInput{
		IdempotencyKey:        "payment-order-1",
		ExternalBillingUserID: "billing-user-1",
		SourceReference:       "bank-payment-1",
	}
}

func materialFor(
	request ports.APIKeyProvisioningMaterialRequest,
) ports.APIKeyProvisioningMaterial {
	return ports.APIKeyProvisioningMaterial{
		APIKey: domain.APIKeyRecord{
			ID:        request.APIKeyID,
			UserID:    request.User.ID,
			Name:      request.KeyName,
			KeyHash:   strings.Repeat("a", 64),
			KeyPrefix: "sk_live_abcdefgh...",
			Enabled:   true,
			CreatedAt: request.CreatedAt,
			UpdatedAt: request.CreatedAt,
		},
		EncryptedRawKey:      []byte("ciphertext"),
		EncryptionNonce:      []byte("nonce"),
		EncryptionKeyVersion: "v1",
	}
}

func resultFor(
	request ports.APIKeyProvisioningRequest,
	outcome ports.APIKeyProvisioningOutcome,
) ports.APIKeyProvisioningResult {
	user := request.NewUser
	apiKey := domain.APIKeyRecord{
		ID:        request.APIKeyID,
		UserID:    user.ID,
		Name:      request.KeyName,
		KeyHash:   strings.Repeat("a", 64),
		KeyPrefix: "sk_live_abcdefgh...",
		Enabled:   true,
		CreatedAt: request.CreatedAt,
		UpdatedAt: request.CreatedAt,
	}
	expiresAt := request.ExpiresAt
	provisioning := domain.APIKeyProvisioning{
		ID:                    request.ProvisioningID,
		IdempotencyKey:        request.IdempotencyKey,
		SourceReferenceHash:   request.SourceReferenceHash,
		ExternalBillingUserID: request.ExternalBillingUserID,
		UserID:                user.ID,
		APIKeyID:              apiKey.ID,
		ResultType:            domain.APIKeyProvisioningResultTypeKeyCreated,
		Status:                domain.APIKeyProvisioningStatusPendingDelivery,
		EncryptedRawKey:       []byte("ciphertext"),
		EncryptionNonce:       []byte("nonce"),
		EncryptionKeyVersion:  "v1",
		CreatedAt:             request.CreatedAt,
		UpdatedAt:             request.CreatedAt,
		ExpiresAt:             &expiresAt,
	}

	switch outcome {
	case ports.APIKeyProvisioningOutcomeCreated,
		ports.APIKeyProvisioningOutcomeReplayedPending:

	case ports.APIKeyProvisioningOutcomeReplayedDelivered:
		deliveredAt := request.CreatedAt.Add(time.Minute)
		provisioning.Status =
			domain.APIKeyProvisioningStatusDelivered
		provisioning.EncryptedRawKey = nil
		provisioning.EncryptionNonce = nil
		provisioning.DeliveredAt = &deliveredAt

	case ports.APIKeyProvisioningOutcomeAlreadyProvisioned:
		deliveredAt := request.CreatedAt
		apiKey.ID = "ak_existing"
		apiKey.Name = "Existing key"
		provisioning.APIKeyID = apiKey.ID
		provisioning.ResultType =
			domain.APIKeyProvisioningResultTypeAlreadyProvisioned
		provisioning.Status =
			domain.APIKeyProvisioningStatusDelivered
		provisioning.EncryptedRawKey = nil
		provisioning.EncryptionNonce = nil
		provisioning.EncryptionKeyVersion = ""
		provisioning.ExpiresAt = nil
		provisioning.DeliveredAt = &deliveredAt

	case ports.APIKeyProvisioningOutcomeExpired:
		expiredAt := request.ExpiresAt
		apiKey.Enabled = false
		apiKey.RevokedAt = &expiredAt
		apiKey.UpdatedAt = expiredAt
		provisioning.Status =
			domain.APIKeyProvisioningStatusExpired
		provisioning.EncryptedRawKey = nil
		provisioning.EncryptionNonce = nil
		provisioning.UpdatedAt = expiredAt
		provisioning.ExpiredAt = &expiredAt
	}

	return ports.APIKeyProvisioningResult{
		Outcome:      outcome,
		User:         user,
		APIKey:       apiKey,
		Provisioning: provisioning,
	}
}

func newServiceForTest(
	t *testing.T,
	store ports.APIKeyProvisioningStore,
	factory ports.APIKeyProvisioningMaterialFactory,
	decryptor ports.APIKeyProvisioningMaterialDecryptor,
) *Service {
	t.Helper()
	service, err := NewService(Dependencies{
		Store:             store,
		MaterialFactory:   factory,
		MaterialDecryptor: decryptor,
		Clock:             testClock{now: fixedTime()},
		TTL:               24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func TestProvisionCreatedBuildsCanonicalStoreCommand(
	t *testing.T,
) {
	const rawAPIKey = "sk_live_returned_once"
	factory := &testMaterialFactory{
		create: func(
			_ context.Context,
			request ports.APIKeyProvisioningMaterialRequest,
		) (ports.APIKeyProvisioningMaterial, error) {
			return materialFor(request), nil
		},
	}
	decryptor := &testMaterialDecryptor{
		raw: rawAPIKey,
	}
	store := &testProvisioningStore{}
	store.provision = func(
		ctx context.Context,
		request ports.APIKeyProvisioningRequest,
		passedFactory ports.APIKeyProvisioningMaterialFactory,
	) (ports.APIKeyProvisioningResult, error) {
		material, err :=
			passedFactory.CreateProvisioningMaterial(
				ctx,
				ports.APIKeyProvisioningMaterialRequest{
					User:           request.NewUser,
					ProvisioningID: request.ProvisioningID,
					APIKeyID:       request.APIKeyID,
					KeyName:        request.KeyName,
					CreatedAt:      request.CreatedAt,
					ExpiresAt:      request.ExpiresAt,
				},
			)
		if err != nil {
			return ports.APIKeyProvisioningResult{}, err
		}
		result := resultFor(
			request,
			ports.APIKeyProvisioningOutcomeCreated,
		)
		result.APIKey = material.APIKey
		result.Provisioning.EncryptedRawKey =
			material.EncryptedRawKey
		result.Provisioning.EncryptionNonce =
			material.EncryptionNonce
		result.Provisioning.EncryptionKeyVersion =
			material.EncryptionKeyVersion
		return result, nil
	}

	service := newServiceForTest(
		t,
		store,
		factory,
		decryptor,
	)
	input := validInput()
	result, err := service.Provision(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if result.Result != ResultTypeCreated ||
		result.APIKey != rawAPIKey ||
		result.ProvisioningStatus !=
			domain.APIKeyProvisioningStatusPendingDelivery {
		t.Fatalf("result = %+v", result)
	}
	if factory.calls != 1 ||
		decryptor.calls != 1 ||
		store.recordAttemptCalls != 1 ||
		store.lastAttemptID != store.lastRequest.ProvisioningID ||
		store.lastAttemptAt != fixedTime() {
		t.Fatalf(
			"factory calls = %d, decrypt calls = %d, "+
				"attempt calls = %d, attempt id = %q, attempt at = %v",
			factory.calls,
			decryptor.calls,
			store.recordAttemptCalls,
			store.lastAttemptID,
			store.lastAttemptAt,
		)
	}
	if store.lastRequest.KeyName != defaultKeyName ||
		store.lastRequest.NewUser.ExternalBillingUserID !=
			input.ExternalBillingUserID ||
		!store.lastRequest.NewUser.Enabled ||
		store.lastRequest.CreatedAt != fixedTime() ||
		store.lastRequest.ExpiresAt !=
			fixedTime().Add(24*time.Hour) {
		t.Fatalf(
			"store request = %+v",
			store.lastRequest,
		)
	}

	sourceHash := sha256.Sum256(
		[]byte(input.SourceReference),
	)
	if store.lastRequest.SourceReferenceHash !=
		hex.EncodeToString(sourceHash[:]) {
		t.Fatalf(
			"source hash = %q",
			store.lastRequest.SourceReferenceHash,
		)
	}
	if strings.Contains(
		store.lastRequest.SourceReferenceHash,
		input.SourceReference,
	) {
		t.Fatal("source reference persisted in plaintext")
	}
	for prefix, value := range map[string]string{
		"usr_":  store.lastRequest.NewUser.ID,
		"prov_": store.lastRequest.ProvisioningID,
		"ak_":   store.lastRequest.APIKeyID,
	} {
		if !strings.HasPrefix(value, prefix) ||
			len(value) != len(prefix)+64 {
			t.Fatalf("generated ID = %q", value)
		}
	}
}

func TestProvisionOutcomeControlsRawKeyDisclosure(
	t *testing.T,
) {
	tests := []struct {
		name         string
		outcome      ports.APIKeyProvisioningOutcome
		wantResult   ResultType
		wantRaw      bool
		wantDecrypts int
		wantAttempts int
	}{
		{
			name:         "pending replay",
			outcome:      ports.APIKeyProvisioningOutcomeReplayedPending,
			wantResult:   ResultTypeReplayed,
			wantRaw:      true,
			wantDecrypts: 1,
			wantAttempts: 1,
		},
		{
			name:       "delivered replay",
			outcome:    ports.APIKeyProvisioningOutcomeReplayedDelivered,
			wantResult: ResultTypeReplayed,
		},
		{
			name:       "already provisioned",
			outcome:    ports.APIKeyProvisioningOutcomeAlreadyProvisioned,
			wantResult: ResultTypeAlreadyProvisioned,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &testProvisioningStore{}
			store.provision = func(
				_ context.Context,
				request ports.APIKeyProvisioningRequest,
				_ ports.APIKeyProvisioningMaterialFactory,
			) (ports.APIKeyProvisioningResult, error) {
				return resultFor(
					request,
					test.outcome,
				), nil
			}
			decryptor := &testMaterialDecryptor{
				raw: "sk_live_same_retry_key",
			}
			service := newServiceForTest(
				t,
				store,
				&testMaterialFactory{},
				decryptor,
			)

			result, err := service.Provision(
				context.Background(),
				validInput(),
			)
			if err != nil {
				t.Fatalf("Provision: %v", err)
			}
			if result.Result != test.wantResult {
				t.Fatalf(
					"result type = %q",
					result.Result,
				)
			}
			if (result.APIKey != "") != test.wantRaw {
				t.Fatalf(
					"raw key = %q, want present = %t",
					result.APIKey,
					test.wantRaw,
				)
			}
			if decryptor.calls != test.wantDecrypts ||
				store.recordAttemptCalls != test.wantAttempts {
				t.Fatalf(
					"decrypt calls = %d, want %d; "+
						"attempt calls = %d, want %d",
					decryptor.calls,
					test.wantDecrypts,
					store.recordAttemptCalls,
					test.wantAttempts,
				)
			}
		})
	}
}

func TestProvisionExpiresDuePendingRecordBeforeDisclosure(
	t *testing.T,
) {
	store := &testProvisioningStore{}
	store.provision = func(
		_ context.Context,
		request ports.APIKeyProvisioningRequest,
		_ ports.APIKeyProvisioningMaterialFactory,
	) (ports.APIKeyProvisioningResult, error) {
		result := resultFor(
			request,
			ports.APIKeyProvisioningOutcomeReplayedPending,
		)
		due := fixedTime()
		result.Provisioning.CreatedAt =
			due.Add(-24 * time.Hour)
		result.Provisioning.UpdatedAt =
			result.Provisioning.CreatedAt
		result.Provisioning.ExpiresAt = &due
		result.APIKey.CreatedAt =
			result.Provisioning.CreatedAt
		result.APIKey.UpdatedAt =
			result.Provisioning.CreatedAt
		return result, nil
	}
	store.expire = func(
		_ context.Context,
		_ string,
		at time.Time,
	) (domain.APIKeyProvisioning, error) {
		if at != fixedTime() {
			t.Fatalf("expired at = %v", at)
		}
		return domain.APIKeyProvisioning{}, nil
	}
	decryptor := &testMaterialDecryptor{
		raw: "must-not-be-returned",
	}
	service := newServiceForTest(
		t,
		store,
		&testMaterialFactory{},
		decryptor,
	)

	result, err := service.Provision(
		context.Background(),
		validInput(),
	)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("error = %v, want ErrExpired", err)
	}
	if result.APIKey != "" ||
		decryptor.calls != 0 ||
		store.expireCalls != 1 {
		t.Fatalf(
			"result = %+v, decrypts = %d, expires = %d",
			result,
			decryptor.calls,
			store.expireCalls,
		)
	}
}

func TestProvisionDoesNotDiscloseRawKeyWhenDeliveryAttemptFails(
	t *testing.T,
) {
	store := &testProvisioningStore{}
	store.provision = func(
		_ context.Context,
		request ports.APIKeyProvisioningRequest,
		_ ports.APIKeyProvisioningMaterialFactory,
	) (ports.APIKeyProvisioningResult, error) {
		return resultFor(
			request,
			ports.APIKeyProvisioningOutcomeReplayedPending,
		), nil
	}
	store.recordAttempt = func(
		context.Context,
		string,
		time.Time,
	) (domain.APIKeyProvisioning, error) {
		return domain.APIKeyProvisioning{},
			errors.New("database unavailable with sk_live_secret")
	}
	decryptor := &testMaterialDecryptor{
		raw: "sk_live_must_not_leave_application",
	}
	service := newServiceForTest(
		t,
		store,
		&testMaterialFactory{},
		decryptor,
	)

	result, err := service.Provision(
		context.Background(),
		validInput(),
	)
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("error = %v, want ErrStoreUnavailable", err)
	}
	if result.APIKey != "" ||
		decryptor.calls != 1 ||
		store.recordAttemptCalls != 1 {
		t.Fatalf(
			"result=%+v decrypts=%d attempts=%d",
			result,
			decryptor.calls,
			store.recordAttemptCalls,
		)
	}
	if strings.Contains(err.Error(), "sk_live_") {
		t.Fatalf("raw key leaked through error: %v", err)
	}
}

func TestProvisionRejectsMalformedDeliveryAttemptRecord(
	t *testing.T,
) {
	store := &testProvisioningStore{}
	store.provision = func(
		_ context.Context,
		request ports.APIKeyProvisioningRequest,
		_ ports.APIKeyProvisioningMaterialFactory,
	) (ports.APIKeyProvisioningResult, error) {
		return resultFor(
			request,
			ports.APIKeyProvisioningOutcomeCreated,
		), nil
	}
	store.recordAttempt = func(
		_ context.Context,
		_ string,
		at time.Time,
	) (domain.APIKeyProvisioning, error) {
		malformed := store.lastProvisioning
		malformed.DeliveryAttempts += 2
		malformed.UpdatedAt = at
		return malformed, nil
	}
	service := newServiceForTest(
		t,
		store,
		&testMaterialFactory{},
		&testMaterialDecryptor{
			raw: "sk_live_must_not_be_returned",
		},
	)

	result, err := service.Provision(
		context.Background(),
		validInput(),
	)
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("error = %v, want ErrStoreUnavailable", err)
	}
	if result.APIKey != "" {
		t.Fatalf(
			"malformed attempt disclosed raw key: %+v",
			result,
		)
	}
}

func TestProvisionMapsStoreAndCryptoErrorsWithoutSecrets(
	t *testing.T,
) {
	tests := []struct {
		name    string
		store   *testProvisioningStore
		factory *testMaterialFactory
		decrypt *testMaterialDecryptor
		want    error
	}{
		{
			name: "store conflict",
			store: &testProvisioningStore{
				provision: func(
					context.Context,
					ports.APIKeyProvisioningRequest,
					ports.APIKeyProvisioningMaterialFactory,
				) (ports.APIKeyProvisioningResult, error) {
					return ports.APIKeyProvisioningResult{},
						ports.ErrStoreConflict
				},
			},
			factory: &testMaterialFactory{},
			decrypt: &testMaterialDecryptor{},
			want:    ErrConflict,
		},
		{
			name: "factory failure",
			store: &testProvisioningStore{
				provision: func(
					ctx context.Context,
					request ports.APIKeyProvisioningRequest,
					factory ports.APIKeyProvisioningMaterialFactory,
				) (ports.APIKeyProvisioningResult, error) {
					_, err :=
						factory.CreateProvisioningMaterial(
							ctx,
							ports.APIKeyProvisioningMaterialRequest{
								User: request.NewUser,
							},
						)
					return ports.APIKeyProvisioningResult{},
						err
				},
			},
			factory: &testMaterialFactory{
				create: func(
					context.Context,
					ports.APIKeyProvisioningMaterialRequest,
				) (ports.APIKeyProvisioningMaterial, error) {
					return ports.APIKeyProvisioningMaterial{},
						errors.New(
							"raw-secret-from-crypto",
						)
				},
			},
			decrypt: &testMaterialDecryptor{},
			want:    ErrCryptoUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newServiceForTest(
				t,
				test.store,
				test.factory,
				test.decrypt,
			)
			_, err := service.Provision(
				context.Background(),
				validInput(),
			)
			if !errors.Is(err, test.want) {
				t.Fatalf(
					"error = %v, want %v",
					err,
					test.want,
				)
			}
			if strings.Contains(
				err.Error(),
				"raw-secret",
			) {
				t.Fatalf("secret leaked in error: %v", err)
			}
		})
	}
}

func TestConfirmDeliveryHandlesPendingDeliveredAndExpired(
	t *testing.T,
) {
	request := ports.APIKeyProvisioningRequest{
		IdempotencyKey:        "payment-order-1",
		SourceReferenceHash:   strings.Repeat("a", 64),
		ExternalBillingUserID: "billing-user-1",
		NewUser: domain.User{
			ID:                    "usr_1",
			ExternalBillingUserID: "billing-user-1",
			Enabled:               true,
			CreatedAt:             fixedTime(),
			UpdatedAt:             fixedTime(),
		},
		ProvisioningID: "prov_1",
		APIKeyID:       "ak_1",
		KeyName:        defaultKeyName,
		CreatedAt:      fixedTime(),
		ExpiresAt:      fixedTime().Add(24 * time.Hour),
	}

	t.Run("pending becomes delivered", func(t *testing.T) {
		pending := resultFor(
			request,
			ports.APIKeyProvisioningOutcomeReplayedPending,
		).Provisioning
		store := &testProvisioningStore{}
		store.find = func(
			context.Context,
			string,
		) (*domain.APIKeyProvisioning, error) {
			copyValue := pending
			return &copyValue, nil
		}
		store.confirm = func(
			_ context.Context,
			id string,
			at time.Time,
		) (domain.APIKeyProvisioning, error) {
			confirmed := pending
			confirmed.Status =
				domain.APIKeyProvisioningStatusDelivered
			confirmed.EncryptedRawKey = nil
			confirmed.EncryptionNonce = nil
			confirmed.UpdatedAt = at
			confirmed.DeliveredAt = &at
			return confirmed, nil
		}
		service := newServiceForTest(
			t,
			store,
			&testMaterialFactory{},
			&testMaterialDecryptor{},
		)

		result, err := service.ConfirmDelivery(
			context.Background(),
			request.ProvisioningID,
		)
		if err != nil {
			t.Fatalf("ConfirmDelivery: %v", err)
		}
		if result.Status !=
			domain.APIKeyProvisioningStatusDelivered ||
			result.DeliveredAt == nil ||
			store.confirmCalls != 1 {
			t.Fatalf(
				"result = %+v, confirm calls = %d",
				result,
				store.confirmCalls,
			)
		}
	})

	t.Run("delivered is idempotent", func(t *testing.T) {
		delivered := resultFor(
			request,
			ports.APIKeyProvisioningOutcomeReplayedDelivered,
		).Provisioning
		store := &testProvisioningStore{}
		store.find = func(
			context.Context,
			string,
		) (*domain.APIKeyProvisioning, error) {
			copyValue := delivered
			return &copyValue, nil
		}
		service := newServiceForTest(
			t,
			store,
			&testMaterialFactory{},
			&testMaterialDecryptor{},
		)

		result, err := service.ConfirmDelivery(
			context.Background(),
			request.ProvisioningID,
		)
		if err != nil {
			t.Fatalf("ConfirmDelivery: %v", err)
		}
		if result.Status !=
			domain.APIKeyProvisioningStatusDelivered ||
			store.confirmCalls != 0 {
			t.Fatalf(
				"result = %+v, confirm calls = %d",
				result,
				store.confirmCalls,
			)
		}
	})

	t.Run("due pending expires", func(t *testing.T) {
		pending := resultFor(
			request,
			ports.APIKeyProvisioningOutcomeReplayedPending,
		).Provisioning
		due := fixedTime()
		pending.CreatedAt =
			due.Add(-24 * time.Hour)
		pending.UpdatedAt = pending.CreatedAt
		pending.ExpiresAt = &due

		store := &testProvisioningStore{}
		store.find = func(
			context.Context,
			string,
		) (*domain.APIKeyProvisioning, error) {
			copyValue := pending
			return &copyValue, nil
		}
		store.expire = func(
			context.Context,
			string,
			time.Time,
		) (domain.APIKeyProvisioning, error) {
			return domain.APIKeyProvisioning{}, nil
		}
		service := newServiceForTest(
			t,
			store,
			&testMaterialFactory{},
			&testMaterialDecryptor{},
		)

		_, err := service.ConfirmDelivery(
			context.Background(),
			request.ProvisioningID,
		)
		if !errors.Is(err, ErrExpired) {
			t.Fatalf(
				"error = %v, want ErrExpired",
				err,
			)
		}
		if store.expireCalls != 1 ||
			store.confirmCalls != 0 {
			t.Fatalf(
				"expire calls = %d, confirm calls = %d",
				store.expireCalls,
				store.confirmCalls,
			)
		}
	})
}

func TestInvalidInputDoesNotReachStore(t *testing.T) {
	store := &testProvisioningStore{}
	service := newServiceForTest(
		t,
		store,
		&testMaterialFactory{},
		&testMaterialDecryptor{},
	)

	input := validInput()
	input.SourceReference = "   "
	_, err := service.Provision(
		context.Background(),
		input,
	)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf(
			"error = %v, want ErrInvalidRequest",
			err,
		)
	}
	if store.provisionCalls != 0 {
		t.Fatalf(
			"store calls = %d, want 0",
			store.provisionCalls,
		)
	}
}

func TestNewServiceRejectsIncompleteDependencies(t *testing.T) {
	service, err := NewService(Dependencies{})
	if service != nil {
		t.Fatal("service must be nil")
	}
	if !errors.Is(err, ErrInternal) {
		t.Fatalf("error = %v, want ErrInternal", err)
	}
}
