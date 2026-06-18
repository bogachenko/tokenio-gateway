package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type fakeAdminProvisioningRepository struct {
	page   ports.Page[ports.APIKeyProvisioningAdminRecord]
	err    error
	calls  int
	filter ports.APIKeyProvisioningListFilter
}

func (f *fakeAdminProvisioningRepository) ListAPIKeyProvisionings(
	_ context.Context,
	filter ports.APIKeyProvisioningListFilter,
) (ports.Page[ports.APIKeyProvisioningAdminRecord], error) {
	f.calls++
	f.filter = filter
	return f.page, f.err
}

func validAdminProvisioningRecord() ports.APIKeyProvisioningAdminRecord {
	createdAt := time.Date(
		2026,
		time.June,
		13,
		12,
		0,
		0,
		0,
		time.UTC,
	)
	expiresAt := createdAt.Add(24 * time.Hour)
	sourceHash := sha256.Sum256(
		[]byte("payment-1"),
	)
	return ports.APIKeyProvisioningAdminRecord{
		ID:                    "prov_1",
		ExternalBillingUserID: "billing_1",
		UserID:                "usr_1",
		APIKeyID:              "ak_1",
		KeyPrefix:             "sk_live_abcd...",
		ResultType:            domain.APIKeyProvisioningResultTypeKeyCreated,
		Status:                domain.APIKeyProvisioningStatusPendingDelivery,
		SourceReferenceHash:   hex.EncodeToString(sourceHash[:]),
		CreatedAt:             createdAt,
		ExpiresAt:             &expiresAt,
	}
}

func TestProvisioningQueryServiceListsSafeViews(
	t *testing.T,
) {
	record := validAdminProvisioningRecord()
	repository := &fakeAdminProvisioningRepository{
		page: ports.Page[ports.APIKeyProvisioningAdminRecord]{
			Items: []ports.APIKeyProvisioningAdminRecord{
				record,
			},
			Total: 1,
		},
	}
	service, err :=
		NewProvisioningQueryService(repository)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.ListAPIKeyProvisionings(
		context.Background(),
		APIKeyProvisioningListInput{
			ExternalBillingUserID: "billing_1",
			Status:                domain.APIKeyProvisioningStatusPendingDelivery,
			Limit:                 25,
			Offset:                5,
		},
	)
	if err != nil {
		t.Fatalf(
			"ListAPIKeyProvisionings: %v",
			err,
		)
	}
	if repository.calls != 1 ||
		repository.filter.Page.Limit != 25 ||
		repository.filter.Page.Offset != 5 ||
		repository.filter.ExternalBillingUserID !=
			"billing_1" {
		t.Fatalf(
			"filter = %+v calls = %d",
			repository.filter,
			repository.calls,
		)
	}
	if len(result.Data) != 1 ||
		result.Data[0].ProvisioningID != "prov_1" ||
		result.Data[0].KeyPrefix !=
			"sk_live_abcd..." ||
		result.Pagination.Total != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestProvisioningQueryServiceRejectsInvalidInputBeforeStore(
	t *testing.T,
) {
	repository := &fakeAdminProvisioningRepository{}
	service, err :=
		NewProvisioningQueryService(repository)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.ListAPIKeyProvisionings(
		context.Background(),
		APIKeyProvisioningListInput{
			UserID: " usr_1",
		},
	)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf(
			"error = %v, want ErrInvalidRequest",
			err,
		)
	}
	if repository.calls != 0 {
		t.Fatalf(
			"repository calls = %d, want 0",
			repository.calls,
		)
	}
}

func TestProvisioningQueryServiceRejectsMalformedStoreRecord(
	t *testing.T,
) {
	record := validAdminProvisioningRecord()
	record.SourceReferenceHash = "not-a-hash"
	repository := &fakeAdminProvisioningRepository{
		page: ports.Page[ports.APIKeyProvisioningAdminRecord]{
			Items: []ports.APIKeyProvisioningAdminRecord{
				record,
			},
			Total: 1,
		},
	}
	service, err :=
		NewProvisioningQueryService(repository)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.ListAPIKeyProvisionings(
		context.Background(),
		APIKeyProvisioningListInput{},
	)
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf(
			"error = %v, want ErrStoreUnavailable",
			err,
		)
	}
}

func TestProvisioningQueryServiceMapsStoreFailure(
	t *testing.T,
) {
	repository := &fakeAdminProvisioningRepository{
		err: ports.ErrStoreUnavailable,
	}
	service, err :=
		NewProvisioningQueryService(repository)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.ListAPIKeyProvisionings(
		context.Background(),
		APIKeyProvisioningListInput{},
	)
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf(
			"error = %v, want ErrStoreUnavailable",
			err,
		)
	}
}

func TestNewProvisioningQueryServiceRejectsNilRepository(
	t *testing.T,
) {
	service, err :=
		NewProvisioningQueryService(nil)
	if service != nil ||
		!errors.Is(err, ErrInternal) {
		t.Fatalf(
			"service=%v error=%v",
			service,
			err,
		)
	}
}

func TestProvisioningQueryServiceGetsSafeViewByID(t *testing.T) {
	record := validAdminProvisioningRecord()
	repository := &fakeAdminProvisioningRepository{
		page: ports.Page[ports.APIKeyProvisioningAdminRecord]{
			Items: []ports.APIKeyProvisioningAdminRecord{record},
			Total: 1,
		},
	}
	service, err := NewProvisioningQueryService(repository)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.GetAPIKeyProvisioning(
		context.Background(),
		record.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if view.ProvisioningID != record.ID ||
		repository.filter.ProvisioningID != record.ID ||
		repository.filter.Page.Limit != 1 {
		t.Fatalf("view=%+v filter=%+v", view, repository.filter)
	}
}
