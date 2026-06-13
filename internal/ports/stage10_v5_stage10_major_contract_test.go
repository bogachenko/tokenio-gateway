package ports

import (
	"context"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type stage10V5ApiKeyAdminContractFake struct{}

func (stage10V5ApiKeyAdminContractFake) FindByHash(context.Context, string) (*domain.APIKeyRecord, error) {
	return nil, ErrNotFound
}
func (stage10V5ApiKeyAdminContractFake) FindAPIKeyByID(context.Context, string) (*domain.APIKeyRecord, error) {
	return nil, ErrNotFound
}
func (stage10V5ApiKeyAdminContractFake) ListAPIKeys(context.Context, APIKeyListFilter) (Page[domain.APIKeyRecord], error) {
	return Page[domain.APIKeyRecord]{}, nil
}
func (stage10V5ApiKeyAdminContractFake) CreateAPIKeyWithAudit(context.Context, domain.APIKeyRecord, domain.AuditContext) error {
	return nil
}
func (stage10V5ApiKeyAdminContractFake) CompareAndSwapAPIKeyWithAudit(context.Context, domain.APIKeyRecord, domain.APIKeyRecord, domain.AuditContext) (domain.APIKeyRecord, error) {
	return domain.APIKeyRecord{}, nil
}

func TestStage10V5AdminAPIKeyRepositoryCommitContract(t *testing.T) {
	var repository AdminAPIKeyRepository = stage10V5ApiKeyAdminContractFake{}
	record := domain.APIKeyRecord{ID: "ak_1"}
	if err := repository.CreateAPIKeyWithAudit(context.Background(), record, domain.AuditContext{}); err != nil {
		t.Fatal(err)
	}
}
