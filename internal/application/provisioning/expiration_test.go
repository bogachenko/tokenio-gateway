package provisioning

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type expirationStore struct {
	list func(
		context.Context,
		time.Time,
		int,
	) ([]domain.APIKeyProvisioning, error)
	expire func(
		context.Context,
		string,
		time.Time,
	) (domain.APIKeyProvisioning, error)
	find func(
		context.Context,
		string,
	) (*domain.APIKeyProvisioning, error)

	listCalls   int
	expireCalls int
	findCalls   int

	listedAsOf  time.Time
	listedLimit int
}

func (s *expirationStore) ProvisionAPIKey(
	context.Context,
	ports.APIKeyProvisioningRequest,
	ports.APIKeyProvisioningMaterialFactory,
) (ports.APIKeyProvisioningResult, error) {
	return ports.APIKeyProvisioningResult{},
		ports.ErrStoreUnavailable
}

func (s *expirationStore) FindAPIKeyProvisioningByID(
	ctx context.Context,
	id string,
) (*domain.APIKeyProvisioning, error) {
	s.findCalls++
	if s.find == nil {
		return nil, ports.ErrNotFound
	}
	return s.find(ctx, id)
}

func (*expirationStore) FindAPIKeyProvisioningByIdempotencyKey(
	context.Context,
	string,
) (*domain.APIKeyProvisioning, error) {
	return nil, ports.ErrNotFound
}

func (*expirationStore) RecordAPIKeyDeliveryAttempt(
	context.Context,
	string,
	time.Time,
) (domain.APIKeyProvisioning, error) {
	return domain.APIKeyProvisioning{},
		ports.ErrStoreUnavailable
}

func (*expirationStore) ConfirmAPIKeyDelivery(
	context.Context,
	string,
	time.Time,
) (domain.APIKeyProvisioning, error) {
	return domain.APIKeyProvisioning{},
		ports.ErrStoreUnavailable
}

func (s *expirationStore) ListPendingAPIKeyProvisioningsDue(
	ctx context.Context,
	asOf time.Time,
	limit int,
) ([]domain.APIKeyProvisioning, error) {
	s.listCalls++
	s.listedAsOf = asOf
	s.listedLimit = limit
	if s.list == nil {
		return nil, nil
	}
	return s.list(ctx, asOf, limit)
}

func (s *expirationStore) ExpireAPIKeyProvisioning(
	ctx context.Context,
	id string,
	asOf time.Time,
) (domain.APIKeyProvisioning, error) {
	s.expireCalls++
	if s.expire == nil {
		return domain.APIKeyProvisioning{},
			ports.ErrStoreUnavailable
	}
	return s.expire(ctx, id, asOf)
}

func expirationTime() time.Time {
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

func dueProvisioning(
	id string,
	expiresAt time.Time,
) domain.APIKeyProvisioning {
	createdAt := expiresAt.Add(-24 * time.Hour)
	return domain.APIKeyProvisioning{
		ID:                    id,
		IdempotencyKey:        "idem_" + id,
		SourceReferenceHash:   "source_hash_" + id,
		ExternalBillingUserID: "billing_" + id,
		UserID:                "usr_" + id,
		APIKeyID:              "ak_" + id,
		ResultType:            domain.APIKeyProvisioningResultTypeKeyCreated,
		Status:                domain.APIKeyProvisioningStatusPendingDelivery,
		EncryptedRawKey:       []byte("ciphertext"),
		EncryptionNonce:       []byte("nonce"),
		EncryptionKeyVersion:  "v1",
		CreatedAt:             createdAt,
		UpdatedAt:             createdAt,
		ExpiresAt:             &expiresAt,
	}
}

func expiredProvisioning(
	pending domain.APIKeyProvisioning,
	expiredAt time.Time,
) domain.APIKeyProvisioning {
	pending.Status =
		domain.APIKeyProvisioningStatusExpired
	pending.EncryptedRawKey = nil
	pending.EncryptionNonce = nil
	pending.UpdatedAt = expiredAt
	pending.ExpiredAt = &expiredAt
	return pending
}

func deliveredProvisioning(
	pending domain.APIKeyProvisioning,
	deliveredAt time.Time,
) domain.APIKeyProvisioning {
	pending.Status =
		domain.APIKeyProvisioningStatusDelivered
	pending.EncryptedRawKey = nil
	pending.EncryptionNonce = nil
	pending.UpdatedAt = deliveredAt
	pending.DeliveredAt = &deliveredAt
	return pending
}

func expirationService(
	store ports.APIKeyProvisioningStore,
) *Service {
	return &Service{
		store: store,
		clock: testClock{now: expirationTime()},
	}
}

func TestExpireDueExpiresSelectedRecords(t *testing.T) {
	asOf := expirationTime()
	records := []domain.APIKeyProvisioning{
		dueProvisioning(
			"prov_1",
			asOf.Add(-time.Hour),
		),
		dueProvisioning(
			"prov_2",
			asOf,
		),
	}
	byID := map[string]domain.APIKeyProvisioning{
		records[0].ID: records[0],
		records[1].ID: records[1],
	}

	store := &expirationStore{
		list: func(
			context.Context,
			time.Time,
			int,
		) ([]domain.APIKeyProvisioning, error) {
			return records, nil
		},
		expire: func(
			_ context.Context,
			id string,
			expiredAt time.Time,
		) (domain.APIKeyProvisioning, error) {
			return expiredProvisioning(
				byID[id],
				expiredAt,
			), nil
		},
	}

	result, err := expirationService(store).ExpireDue(
		context.Background(),
		10,
	)
	if err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}
	if result.AsOf != asOf ||
		result.Selected != 2 ||
		result.Expired != 2 ||
		result.AlreadyTerminal != 0 ||
		result.Failed != 0 ||
		len(result.FailedProvisioningIDs) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if store.listedAsOf != asOf ||
		store.listedLimit != 10 ||
		store.expireCalls != 2 {
		t.Fatalf(
			"asOf=%v limit=%d expireCalls=%d",
			store.listedAsOf,
			store.listedLimit,
			store.expireCalls,
		)
	}
}

func TestExpireDueContinuesAfterIndividualFailure(
	t *testing.T,
) {
	asOf := expirationTime()
	records := []domain.APIKeyProvisioning{
		dueProvisioning(
			"prov_expired",
			asOf.Add(-time.Hour),
		),
		dueProvisioning(
			"prov_delivered",
			asOf.Add(-time.Hour),
		),
		dueProvisioning(
			"prov_failed",
			asOf.Add(-time.Hour),
		),
	}
	byID := make(
		map[string]domain.APIKeyProvisioning,
		len(records),
	)
	for _, record := range records {
		byID[record.ID] = record
	}

	store := &expirationStore{
		list: func(
			context.Context,
			time.Time,
			int,
		) ([]domain.APIKeyProvisioning, error) {
			return records, nil
		},
		expire: func(
			_ context.Context,
			id string,
			expiredAt time.Time,
		) (domain.APIKeyProvisioning, error) {
			switch id {
			case "prov_expired":
				return expiredProvisioning(
					byID[id],
					expiredAt,
				), nil
			case "prov_delivered":
				return domain.APIKeyProvisioning{},
					ports.ErrStoreConflict
			default:
				return domain.APIKeyProvisioning{},
					errors.New("database unavailable")
			}
		},
		find: func(
			_ context.Context,
			id string,
		) (*domain.APIKeyProvisioning, error) {
			delivered := deliveredProvisioning(
				byID[id],
				*byID[id].ExpiresAt,
			)
			return &delivered, nil
		},
	}

	result, err := expirationService(store).ExpireDue(
		context.Background(),
		10,
	)
	if !errors.Is(
		err,
		ErrExpirationPartialFailure,
	) {
		t.Fatalf(
			"error = %v, want partial failure",
			err,
		)
	}
	if result.Selected != 3 ||
		result.Expired != 1 ||
		result.AlreadyTerminal != 1 ||
		result.Failed != 1 ||
		len(result.FailedProvisioningIDs) != 1 ||
		result.FailedProvisioningIDs[0] !=
			"prov_failed" {
		t.Fatalf("result = %+v", result)
	}
	if store.expireCalls != 3 ||
		store.findCalls != 1 {
		t.Fatalf(
			"expireCalls=%d findCalls=%d",
			store.expireCalls,
			store.findCalls,
		)
	}
}

func TestExpireDueRejectsMalformedBatchBeforeMutation(
	t *testing.T,
) {
	asOf := expirationTime()
	record := dueProvisioning(
		"prov_future",
		asOf.Add(time.Minute),
	)
	store := &expirationStore{
		list: func(
			context.Context,
			time.Time,
			int,
		) ([]domain.APIKeyProvisioning, error) {
			return []domain.APIKeyProvisioning{
				record,
			}, nil
		},
	}

	result, err := expirationService(store).ExpireDue(
		context.Background(),
		10,
	)
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf(
			"error = %v, want ErrStoreUnavailable",
			err,
		)
	}
	if result.Selected != 1 ||
		store.expireCalls != 0 {
		t.Fatalf(
			"result=%+v expireCalls=%d",
			result,
			store.expireCalls,
		)
	}
}

func TestExpireDueMapsListFailure(t *testing.T) {
	store := &expirationStore{
		list: func(
			context.Context,
			time.Time,
			int,
		) ([]domain.APIKeyProvisioning, error) {
			return nil, errors.New(
				"database unavailable",
			)
		},
	}

	result, err := expirationService(store).ExpireDue(
		context.Background(),
		10,
	)
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf(
			"error = %v, want ErrStoreUnavailable",
			err,
		)
	}
	if result.AsOf != expirationTime() ||
		result.Selected != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestExpireDueRejectsInvalidInputBeforeStore(
	t *testing.T,
) {
	store := &expirationStore{}
	service := expirationService(store)

	if _, err := service.ExpireDue(
		context.Background(),
		0,
	); !errors.Is(
		err,
		ErrInvalidExpirationBatch,
	) {
		t.Fatalf("invalid limit error = %v", err)
	}

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()
	if _, err := service.ExpireDue(
		ctx,
		10,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}

	if store.listCalls != 0 {
		t.Fatalf(
			"list calls = %d, want 0",
			store.listCalls,
		)
	}
}
