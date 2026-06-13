package ports

import (
	"context"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type stage10MajorApiKeyAdminContractFake struct{}

func (stage10MajorApiKeyAdminContractFake) FindByHash(context.Context, string) (*domain.APIKeyRecord, error) {
	return nil, ErrNotFound
}
func (stage10MajorApiKeyAdminContractFake) FindAPIKeyByID(context.Context, string) (*domain.APIKeyRecord, error) {
	return nil, ErrNotFound
}
func (stage10MajorApiKeyAdminContractFake) ListAPIKeys(context.Context, APIKeyListFilter) (Page[domain.APIKeyRecord], error) {
	return Page[domain.APIKeyRecord]{}, nil
}
func (stage10MajorApiKeyAdminContractFake) CreateAPIKeyWithAudit(context.Context, domain.APIKeyRecord, domain.AuditContext) error {
	return nil
}
func (stage10MajorApiKeyAdminContractFake) CompareAndSwapAPIKeyWithAudit(context.Context, domain.APIKeyRecord, domain.APIKeyRecord, domain.AuditContext) (domain.APIKeyRecord, error) {
	return domain.APIKeyRecord{}, nil
}

func TestStage10MajorAdminAPIKeyRepositoryCommitContract(t *testing.T) {
	var repository AdminAPIKeyRepository = stage10MajorApiKeyAdminContractFake{}
	record := domain.APIKeyRecord{ID: "ak_1"}
	if err := repository.CreateAPIKeyWithAudit(context.Background(), record, domain.AuditContext{}); err != nil {
		t.Fatal(err)
	}
}
