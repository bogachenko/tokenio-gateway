package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type APIKeyProvisioningListInput struct {
	ProvisioningID        string
	ExternalBillingUserID string
	UserID                string
	APIKeyID              string
	Status                domain.APIKeyProvisioningStatus
	ResultType            domain.APIKeyProvisioningResultType
	CreatedFrom           *time.Time
	CreatedTo             *time.Time
	Limit                 int
	Offset                int
}

type APIKeyProvisioningView struct {
	ProvisioningID        string                              `json:"provisioning_id"`
	ExternalBillingUserID string                              `json:"external_billing_user_id"`
	UserID                string                              `json:"user_id"`
	APIKeyID              string                              `json:"api_key_id"`
	KeyPrefix             string                              `json:"key_prefix"`
	ResultType            domain.APIKeyProvisioningResultType `json:"result_type"`
	Status                domain.APIKeyProvisioningStatus     `json:"status"`
	SourceReferenceHash   string                              `json:"source_reference_hash"`
	CreatedAt             time.Time                           `json:"created_at"`
	ExpiresAt             *time.Time                          `json:"expires_at,omitempty"`
	DeliveredAt           *time.Time                          `json:"delivered_at,omitempty"`
	ExpiredAt             *time.Time                          `json:"expired_at,omitempty"`
}

type ProvisioningQueryService struct {
	repository ports.AdminAPIKeyProvisioningRepository
}

func NewProvisioningQueryService(
	repository ports.AdminAPIKeyProvisioningRepository,
) (*ProvisioningQueryService, error) {
	if repository == nil {
		return nil, ErrInternal
	}
	return &ProvisioningQueryService{
		repository: repository,
	}, nil
}

func (s *ProvisioningQueryService) ListAPIKeyProvisionings(
	ctx context.Context,
	input APIKeyProvisioningListInput,
) (ListResult[APIKeyProvisioningView], error) {
	if ctx == nil || s == nil ||
		s.repository == nil {
		return ListResult[APIKeyProvisioningView]{}, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return ListResult[APIKeyProvisioningView]{}, err
	}

	page, err := normalizePage(
		input.Limit,
		input.Offset,
	)
	if err != nil ||
		validateWindow(
			input.CreatedFrom,
			input.CreatedTo,
		) != nil ||
		!validOptionalOpaque(input.ProvisioningID) ||
		!validOptionalOpaque(
			input.ExternalBillingUserID,
		) ||
		!validOptionalOpaque(input.UserID) ||
		!validOptionalOpaque(input.APIKeyID) ||
		input.Status != "" &&
			!validProvisioningStatus(
				input.Status,
			) ||
		input.ResultType != "" &&
			!validProvisioningResultType(
				input.ResultType,
			) {
		return ListResult[APIKeyProvisioningView]{}, ErrInvalidRequest
	}

	stored, err :=
		s.repository.ListAPIKeyProvisionings(
			ctx,
			ports.APIKeyProvisioningListFilter{
				ProvisioningID:        input.ProvisioningID,
				ExternalBillingUserID: input.ExternalBillingUserID,
				UserID:                input.UserID,
				APIKeyID:              input.APIKeyID,
				Status:                input.Status,
				ResultType:            input.ResultType,
				CreatedFrom:           cloneAdminTime(input.CreatedFrom),
				CreatedTo:             cloneAdminTime(input.CreatedTo),
				Page:                  page,
			},
		)
	if err != nil {
		return ListResult[APIKeyProvisioningView]{}, mapStoreError(err)
	}

	views := make(
		[]APIKeyProvisioningView,
		0,
		len(stored.Items),
	)
	for _, record := range stored.Items {
		if err :=
			validateAdminProvisioningRecord(
				record,
			); err != nil {
			return ListResult[APIKeyProvisioningView]{}, ErrStoreUnavailable
		}
		views = append(
			views,
			APIKeyProvisioningView{
				ProvisioningID:        record.ID,
				ExternalBillingUserID: record.ExternalBillingUserID,
				UserID:                record.UserID,
				APIKeyID:              record.APIKeyID,
				KeyPrefix:             record.KeyPrefix,
				ResultType:            record.ResultType,
				Status:                record.Status,
				SourceReferenceHash:   record.SourceReferenceHash,
				CreatedAt:             record.CreatedAt,
				ExpiresAt: cloneAdminTime(
					record.ExpiresAt,
				),
				DeliveredAt: cloneAdminTime(
					record.DeliveredAt,
				),
				ExpiredAt: cloneAdminTime(
					record.ExpiredAt,
				),
			},
		)
	}

	return ListResult[APIKeyProvisioningView]{
		Data: views,
		Pagination: Pagination{
			Limit:  page.Limit,
			Offset: page.Offset,
			Total:  stored.Total,
		},
	}, nil
}

func validateAdminProvisioningRecord(
	record ports.APIKeyProvisioningAdminRecord,
) error {
	if isBlank(record.ID) ||
		isBlank(record.ExternalBillingUserID) ||
		isBlank(record.UserID) ||
		isBlank(record.APIKeyID) ||
		isBlank(record.KeyPrefix) ||
		!validAdminSHA256Hex(
			record.SourceReferenceHash,
		) ||
		requireUTC(record.CreatedAt) != nil ||
		optionalUTC(record.ExpiresAt) != nil ||
		optionalUTC(record.DeliveredAt) != nil ||
		optionalUTC(record.ExpiredAt) != nil {
		return ErrStoreUnavailable
	}

	switch record.ResultType {
	case domain.APIKeyProvisioningResultTypeKeyCreated:
		if record.ExpiresAt == nil {
			return ErrStoreUnavailable
		}
	case domain.APIKeyProvisioningResultTypeAlreadyProvisioned:
		if record.Status !=
			domain.APIKeyProvisioningStatusDelivered ||
			record.ExpiresAt != nil {
			return ErrStoreUnavailable
		}
	default:
		return ErrStoreUnavailable
	}

	switch record.Status {
	case domain.APIKeyProvisioningStatusPendingDelivery:
		if record.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			record.ExpiresAt == nil ||
			!record.ExpiresAt.After(
				record.CreatedAt,
			) ||
			record.DeliveredAt != nil ||
			record.ExpiredAt != nil {
			return ErrStoreUnavailable
		}

	case domain.APIKeyProvisioningStatusDelivered:
		if record.DeliveredAt == nil ||
			record.DeliveredAt.Before(
				record.CreatedAt,
			) ||
			record.ExpiredAt != nil {
			return ErrStoreUnavailable
		}

	case domain.APIKeyProvisioningStatusExpired:
		if record.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			record.ExpiresAt == nil ||
			record.ExpiredAt == nil ||
			record.ExpiredAt.Before(
				*record.ExpiresAt,
			) ||
			record.DeliveredAt != nil {
			return ErrStoreUnavailable
		}

	default:
		return ErrStoreUnavailable
	}
	return nil
}

func validOptionalOpaque(value string) bool {
	return value == "" ||
		value == strings.TrimSpace(value)
}

func validProvisioningStatus(
	value domain.APIKeyProvisioningStatus,
) bool {
	switch value {
	case domain.APIKeyProvisioningStatusPendingDelivery,
		domain.APIKeyProvisioningStatusDelivered,
		domain.APIKeyProvisioningStatusExpired:
		return true
	default:
		return false
	}
}

func validProvisioningResultType(
	value domain.APIKeyProvisioningResultType,
) bool {
	switch value {
	case domain.APIKeyProvisioningResultTypeKeyCreated,
		domain.APIKeyProvisioningResultTypeAlreadyProvisioned:
		return true
	default:
		return false
	}
}

func validAdminSHA256Hex(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil &&
		len(decoded) == sha256.Size
}

func cloneAdminTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func (s *ProvisioningQueryService) GetAPIKeyProvisioning(
	ctx context.Context,
	provisioningID string,
) (APIKeyProvisioningView, error) {
	if ctx == nil || s == nil || s.repository == nil ||
		isBlank(provisioningID) ||
		provisioningID != strings.TrimSpace(provisioningID) {
		return APIKeyProvisioningView{}, ErrInvalidRequest
	}
	result, err := s.ListAPIKeyProvisionings(
		ctx,
		APIKeyProvisioningListInput{
			ProvisioningID: provisioningID,
			Limit:          1,
		},
	)
	if err != nil {
		return APIKeyProvisioningView{}, err
	}
	if result.Pagination.Total == 0 || len(result.Data) == 0 {
		return APIKeyProvisioningView{}, ErrNotFound
	}
	if result.Pagination.Total != 1 || len(result.Data) != 1 {
		return APIKeyProvisioningView{}, ErrStoreUnavailable
	}
	return result.Data[0], nil
}
