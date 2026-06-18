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
)

func TestAPIKeyProvisioningStoreIntegration(t *testing.T) {
	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)

	store, err := NewAPIKeyProvisioningStore(db)
	if err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	now := time.Now().UTC().Truncate(time.Microsecond)

	firstExternalID := "billing-provisioning-" + suffix
	firstUserID := "provisioning-user-" + suffix
	firstKeyID := "provisioning-key-" + suffix
	firstProvisioningID := "provisioning-" + suffix

	expiringExternalID := "billing-expiring-" + suffix
	expiringUserID := "expiring-user-" + suffix
	expiringKeyID := "expiring-key-" + suffix
	expiringProvisioningID := "expiring-provisioning-" + suffix

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			`
DELETE FROM tokenio_api_key_provisionings
WHERE external_billing_user_id IN ($1, $2)
`,
			firstExternalID,
			expiringExternalID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_api_keys WHERE id IN ($1, $2)",
			firstKeyID,
			expiringKeyID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_users WHERE id IN ($1, $2)",
			firstUserID,
			expiringUserID,
		)
	})

	factoryCalls := 0
	factory := provisioningMaterialFactoryFunc(
		func(
			_ context.Context,
			request ports.APIKeyProvisioningMaterialRequest,
		) (ports.APIKeyProvisioningMaterial, error) {
			factoryCalls++
			return ports.APIKeyProvisioningMaterial{
				APIKey: domain.APIKeyRecord{
					ID:        request.APIKeyID,
					UserID:    request.User.ID,
					Name:      request.KeyName,
					KeyHash:   "hash-" + request.APIKeyID,
					KeyPrefix: "sk_live_abcd...",
					Enabled:   true,
					CreatedAt: request.CreatedAt,
					UpdatedAt: request.CreatedAt,
				},
				EncryptedRawKey:      []byte("ciphertext-" + request.APIKeyID),
				EncryptionNonce:      []byte("nonce-" + request.APIKeyID),
				EncryptionKeyVersion: "v1",
			}, nil
		},
	)

	firstRequest := provisioningRequestForTest(
		firstExternalID,
		firstUserID,
		firstKeyID,
		firstProvisioningID,
		"idem-"+suffix,
		now,
	)
	created, err := store.ProvisionAPIKey(
		ctx,
		firstRequest,
		factory,
	)
	if err != nil {
		t.Fatalf("ProvisionAPIKey: %v", err)
	}
	if created.Outcome != ports.APIKeyProvisioningOutcomeCreated ||
		created.Provisioning.Status !=
			domain.APIKeyProvisioningStatusPendingDelivery ||
		len(created.Provisioning.EncryptedRawKey) == 0 ||
		factoryCalls != 1 {
		t.Fatalf("created result = %+v, calls=%d", created, factoryCalls)
	}

	replayed, err := store.ProvisionAPIKey(
		ctx,
		firstRequest,
		factory,
	)
	if err != nil {
		t.Fatalf("ProvisionAPIKey replay: %v", err)
	}
	if replayed.Outcome !=
		ports.APIKeyProvisioningOutcomeReplayedPending ||
		replayed.APIKey.ID != firstKeyID ||
		factoryCalls != 1 {
		t.Fatalf("replayed result = %+v, calls=%d", replayed, factoryCalls)
	}

	conflicting := firstRequest
	conflicting.SourceReferenceHash = "different-source"
	if _, err := store.ProvisionAPIKey(
		ctx,
		conflicting,
		factory,
	); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}

	attemptedAt := now.Add(time.Minute)
	attempted, err := store.RecordAPIKeyDeliveryAttempt(
		ctx,
		firstProvisioningID,
		attemptedAt,
	)
	if err != nil {
		t.Fatalf("RecordAPIKeyDeliveryAttempt: %v", err)
	}
	if attempted.DeliveryAttempts != 1 {
		t.Fatalf("delivery attempts = %d", attempted.DeliveryAttempts)
	}

	deliveredAt := now.Add(2 * time.Minute)
	delivered, err := store.ConfirmAPIKeyDelivery(
		ctx,
		firstProvisioningID,
		deliveredAt,
	)
	if err != nil {
		t.Fatalf("ConfirmAPIKeyDelivery: %v", err)
	}
	if delivered.Status !=
		domain.APIKeyProvisioningStatusDelivered ||
		len(delivered.EncryptedRawKey) != 0 ||
		len(delivered.EncryptionNonce) != 0 {
		t.Fatalf("delivered = %+v", delivered)
	}

	repeatedConfirm, err := store.ConfirmAPIKeyDelivery(
		ctx,
		firstProvisioningID,
		deliveredAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("repeated confirm: %v", err)
	}
	if !sameProvisioning(repeatedConfirm, delivered) {
		t.Fatalf("repeated confirm changed state")
	}

	deliveredReplay, err := store.ProvisionAPIKey(
		ctx,
		firstRequest,
		factory,
	)
	if err != nil {
		t.Fatalf("delivered replay: %v", err)
	}
	if deliveredReplay.Outcome !=
		ports.APIKeyProvisioningOutcomeReplayedDelivered {
		t.Fatalf("delivered replay = %+v", deliveredReplay)
	}

	secondRequest := firstRequest
	secondRequest.IdempotencyKey = "idem-second-" + suffix
	secondRequest.ProvisioningID = "provisioning-second-" + suffix
	secondRequest.APIKeyID = "unused-key-" + suffix
	secondRequest.CreatedAt = now.Add(3 * time.Minute)
	secondRequest.ExpiresAt =
		secondRequest.CreatedAt.Add(time.Hour)
	already, err := store.ProvisionAPIKey(
		ctx,
		secondRequest,
		factory,
	)
	if err != nil {
		t.Fatalf("already provisioned: %v", err)
	}
	if already.Outcome !=
		ports.APIKeyProvisioningOutcomeAlreadyProvisioned ||
		already.APIKey.ID != firstKeyID ||
		already.Provisioning.ResultType !=
			domain.APIKeyProvisioningResultTypeAlreadyProvisioned ||
		factoryCalls != 1 {
		t.Fatalf("already result = %+v, calls=%d", already, factoryCalls)
	}

	expiringRequest := provisioningRequestForTest(
		expiringExternalID,
		expiringUserID,
		expiringKeyID,
		expiringProvisioningID,
		"idem-expiring-"+suffix,
		now.Add(4*time.Minute),
	)
	expiringCreated, err := store.ProvisionAPIKey(
		ctx,
		expiringRequest,
		factory,
	)
	if err != nil {
		t.Fatalf("create expiring provisioning: %v", err)
	}

	due, err := store.ListPendingAPIKeyProvisioningsDue(
		ctx,
		expiringRequest.ExpiresAt,
		10,
	)
	if err != nil {
		t.Fatalf("ListPendingAPIKeyProvisioningsDue: %v", err)
	}
	foundDue := false
	for _, item := range due {
		if item.ID == expiringProvisioningID {
			foundDue = true
		}
	}
	if !foundDue {
		t.Fatalf("expiring provisioning not listed: %+v", due)
	}

	expired, err := store.ExpireAPIKeyProvisioning(
		ctx,
		expiringProvisioningID,
		expiringRequest.ExpiresAt,
	)
	if err != nil {
		t.Fatalf("ExpireAPIKeyProvisioning: %v", err)
	}
	if expired.Status !=
		domain.APIKeyProvisioningStatusExpired ||
		len(expired.EncryptedRawKey) != 0 ||
		len(expired.EncryptionNonce) != 0 {
		t.Fatalf("expired = %+v", expired)
	}

	expiredKey, err := scanAPIKey(db.QueryRow(
		ctx,
		`
SELECT
    id,
    user_id,
    name,
    key_hash,
    key_prefix,
    enabled,
    created_at,
    updated_at,
    last_used_at,
    revoked_at,
    expires_at
FROM tokenio_api_keys
WHERE id = $1
`,
		expiringCreated.APIKey.ID,
	))
	if err != nil {
		t.Fatalf("load expired key: %v", err)
	}
	if expiredKey.Enabled || expiredKey.RevokedAt == nil {
		t.Fatalf("expired key remains active: %+v", expiredKey)
	}

	expiredReplay, err := store.ProvisionAPIKey(
		ctx,
		expiringRequest,
		factory,
	)
	if err != nil {
		t.Fatalf("expired replay: %v", err)
	}
	if expiredReplay.Outcome !=
		ports.APIKeyProvisioningOutcomeExpired {
		t.Fatalf("expired replay = %+v", expiredReplay)
	}

	assertProvisioningParentDeleteProtection(
		t,
		ctx,
		db,
		firstProvisioningID,
		secondRequest.ProvisioningID,
		firstUserID,
		firstKeyID,
	)
}

func TestAPIKeyProvisioningRejectsDisabledExistingUserIntegration(
	t *testing.T,
) {
	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)

	store, err := NewAPIKeyProvisioningStore(db)
	if err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	now := time.Now().UTC().Truncate(time.Microsecond)
	externalID := "billing-disabled-" + suffix
	userID := "provisioning-disabled-user-" + suffix
	keyID := "provisioning-disabled-key-" + suffix
	provisioningID := "provisioning-disabled-" + suffix
	disabledAt := now.Add(-time.Minute)

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_api_key_provisionings WHERE external_billing_user_id = $1",
			externalID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_api_keys WHERE user_id = $1",
			userID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_users WHERE id = $1",
			userID,
		)
	})

	if _, err := db.Exec(
		ctx,
		`
INSERT INTO tokenio_users (
    id,
    external_billing_user_id,
    email,
    name,
    enabled,
    created_at,
    updated_at,
    disabled_at
)
VALUES ($1, $2, '', '', false, $3, $3, $4)
`,
		userID,
		externalID,
		now.Add(-time.Hour),
		disabledAt,
	); err != nil {
		t.Fatalf("insert disabled user: %v", err)
	}

	request := provisioningRequestForTest(
		externalID,
		userID,
		keyID,
		provisioningID,
		"idem-disabled-"+suffix,
		now,
	)

	factoryCalls := 0
	factory := provisioningMaterialFactoryFunc(
		func(
			_ context.Context,
			request ports.APIKeyProvisioningMaterialRequest,
		) (ports.APIKeyProvisioningMaterial, error) {
			factoryCalls++
			return ports.APIKeyProvisioningMaterial{
				APIKey: domain.APIKeyRecord{
					ID:        request.APIKeyID,
					UserID:    request.User.ID,
					Name:      request.KeyName,
					KeyHash:   "hash-" + request.APIKeyID,
					KeyPrefix: "sk_live_abcd...",
					Enabled:   true,
					CreatedAt: request.CreatedAt,
					UpdatedAt: request.CreatedAt,
				},
				EncryptedRawKey:      []byte("ciphertext"),
				EncryptionNonce:      []byte("nonce"),
				EncryptionKeyVersion: "v1",
			}, nil
		},
	)

	if _, err := store.ProvisionAPIKey(
		ctx,
		request,
		factory,
	); !errors.Is(err, ports.ErrProvisioningUserDisabled) {
		t.Fatalf("disabled user error=%v", err)
	}
	if factoryCalls != 0 {
		t.Fatalf("material factory calls=%d, want 0", factoryCalls)
	}

	for table, predicate := range map[string]string{
		"tokenio_api_keys":              "user_id = $1",
		"tokenio_api_key_provisionings": "user_id = $1",
	} {
		var count int
		query := "SELECT COUNT(*) FROM " + table + " WHERE " + predicate
		if err := db.QueryRow(ctx, query, userID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows=%d, want 0", table, count)
		}
	}
}

func TestAPIKeyProvisioningConcurrentSameInputCreatesOneKeyIntegration(
	t *testing.T,
) {
	ctx := t.Context()
	db := openIsolatedPostgresIntegrationDB(t)

	store, err := NewAPIKeyProvisioningStore(db)
	if err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	now := time.Now().UTC().Truncate(time.Microsecond)
	externalID := "billing-concurrent-" + suffix
	userID := "provisioning-concurrent-user-" + suffix
	keyID := "provisioning-concurrent-key-" + suffix
	provisioningID := "provisioning-concurrent-" + suffix

	t.Cleanup(func() {
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_api_key_provisionings WHERE external_billing_user_id = $1",
			externalID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_api_keys WHERE user_id = $1",
			userID,
		)
		_, _ = db.Exec(
			context.Background(),
			"DELETE FROM tokenio_users WHERE id = $1",
			userID,
		)
	})

	request := provisioningRequestForTest(
		externalID,
		userID,
		keyID,
		provisioningID,
		"idem-concurrent-"+suffix,
		now,
	)

	var factoryMu sync.Mutex
	factoryCalls := 0
	factory := provisioningMaterialFactoryFunc(
		func(
			_ context.Context,
			request ports.APIKeyProvisioningMaterialRequest,
		) (ports.APIKeyProvisioningMaterial, error) {
			factoryMu.Lock()
			factoryCalls++
			factoryMu.Unlock()
			return ports.APIKeyProvisioningMaterial{
				APIKey: domain.APIKeyRecord{
					ID:        request.APIKeyID,
					UserID:    request.User.ID,
					Name:      request.KeyName,
					KeyHash:   "hash-" + request.APIKeyID,
					KeyPrefix: "sk_live_abcd...",
					Enabled:   true,
					CreatedAt: request.CreatedAt,
					UpdatedAt: request.CreatedAt,
				},
				EncryptedRawKey:      []byte("ciphertext-" + request.APIKeyID),
				EncryptionNonce:      []byte("nonce-" + request.APIKeyID),
				EncryptionKeyVersion: "v1",
			}, nil
		},
	)

	type callResult struct {
		value ports.APIKeyProvisioningResult
		err   error
	}
	start := make(chan struct{})
	results := make(chan callResult, 2)

	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			<-start
			value, err := store.ProvisionAPIKey(ctx, request, factory)
			results <- callResult{value: value, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	outcomes := map[ports.APIKeyProvisioningOutcome]int{}
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent provision error=%v", result.err)
		}
		outcomes[result.value.Outcome]++
		if result.value.APIKey.ID != keyID ||
			result.value.Provisioning.ID != provisioningID {
			t.Fatalf("concurrent result=%+v", result.value)
		}
	}
	if outcomes[ports.APIKeyProvisioningOutcomeCreated] != 1 ||
		outcomes[ports.APIKeyProvisioningOutcomeReplayedPending] != 1 {
		t.Fatalf("outcomes=%v", outcomes)
	}

	factoryMu.Lock()
	calls := factoryCalls
	factoryMu.Unlock()
	if calls != 1 {
		t.Fatalf("material factory calls=%d, want 1", calls)
	}

	identities := map[string]string{
		"tokenio_users":                 userID,
		"tokenio_api_keys":              keyID,
		"tokenio_api_key_provisionings": provisioningID,
	}
	for table, identity := range identities {
		var count int
		query := "SELECT COUNT(*) FROM " + table + " WHERE id = $1"
		if err := db.QueryRow(ctx, query, identity).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s rows=%d, want 1", table, count)
		}
	}
}

func assertProvisioningParentDeleteProtection(
	t *testing.T,
	ctx context.Context,
	db *DB,
	createdProvisioningID string,
	alreadyProvisionedID string,
	userID string,
	apiKeyID string,
) {
	t.Helper()

	if _, err := db.Exec(
		ctx,
		"DELETE FROM tokenio_api_keys WHERE id = $1",
		apiKeyID,
	); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("api key delete error=%v, want ErrStoreConflict", err)
	}

	assertProvisioningParentRowCount(
		t,
		ctx,
		db,
		"tokenio_api_keys",
		"id",
		apiKeyID,
		1,
	)
	assertProvisioningParentRowCount(
		t,
		ctx,
		db,
		"tokenio_api_key_provisionings",
		"id",
		createdProvisioningID,
		1,
	)
	assertProvisioningParentRowCount(
		t,
		ctx,
		db,
		"tokenio_api_key_provisionings",
		"id",
		alreadyProvisionedID,
		1,
	)

	if _, err := db.Exec(
		ctx,
		"DELETE FROM tokenio_users WHERE id = $1",
		userID,
	); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf("user delete error=%v, want ErrStoreConflict", err)
	}

	tag, err := db.Exec(
		ctx,
		"DELETE FROM tokenio_api_key_provisionings WHERE id = $1",
		createdProvisioningID,
	)
	if err != nil {
		t.Fatalf("delete created provisioning: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf(
			"deleted created provisioning rows=%d, want 1",
			tag.RowsAffected(),
		)
	}

	if _, err := db.Exec(
		ctx,
		"DELETE FROM tokenio_api_keys WHERE id = $1",
		apiKeyID,
	); !errors.Is(err, ports.ErrStoreConflict) {
		t.Fatalf(
			"api key delete with remaining history error=%v, want ErrStoreConflict",
			err,
		)
	}
	assertProvisioningParentRowCount(
		t,
		ctx,
		db,
		"tokenio_api_key_provisionings",
		"id",
		alreadyProvisionedID,
		1,
	)

	tag, err = db.Exec(
		ctx,
		"DELETE FROM tokenio_api_key_provisionings WHERE id = $1",
		alreadyProvisionedID,
	)
	if err != nil {
		t.Fatalf("delete already-provisioned history: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf(
			"deleted already-provisioned rows=%d, want 1",
			tag.RowsAffected(),
		)
	}

	tag, err = db.Exec(
		ctx,
		"DELETE FROM tokenio_api_keys WHERE id = $1",
		apiKeyID,
	)
	if err != nil {
		t.Fatalf("delete api key after all provisioning removal: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("deleted api key rows=%d, want 1", tag.RowsAffected())
	}

	tag, err = db.Exec(
		ctx,
		"DELETE FROM tokenio_users WHERE id = $1",
		userID,
	)
	if err != nil {
		t.Fatalf("delete user after provisioning and key removal: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("deleted user rows=%d, want 1", tag.RowsAffected())
	}
}

func assertProvisioningParentRowCount(
	t *testing.T,
	ctx context.Context,
	db *DB,
	table string,
	column string,
	value string,
	want int,
) {
	t.Helper()

	query := "SELECT COUNT(*) FROM " + table + " WHERE " + column + " = $1"
	var got int
	if err := db.QueryRow(ctx, query, value).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count=%d, want %d", table, got, want)
	}
}

func provisioningRequestForTest(
	externalBillingUserID string,
	userID string,
	apiKeyID string,
	provisioningID string,
	idempotencyKey string,
	createdAt time.Time,
) ports.APIKeyProvisioningRequest {
	return ports.APIKeyProvisioningRequest{
		IdempotencyKey:        idempotencyKey,
		SourceReferenceHash:   "source-" + idempotencyKey,
		ExternalBillingUserID: externalBillingUserID,
		NewUser: domain.User{
			ID:                    userID,
			ExternalBillingUserID: externalBillingUserID,
			Enabled:               true,
			CreatedAt:             createdAt,
			UpdatedAt:             createdAt,
		},
		ProvisioningID: provisioningID,
		APIKeyID:       apiKeyID,
		KeyName:        "Telegram payment key",
		CreatedAt:      createdAt,
		ExpiresAt:      createdAt.Add(time.Hour),
	}
}
