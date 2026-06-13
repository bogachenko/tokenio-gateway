package provisioning

import (
	"context"
	"errors"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var (
	ErrInvalidExpirationBatch = errors.New(
		"invalid provisioning expiration batch",
	)
	ErrExpirationPartialFailure = errors.New(
		"provisioning expiration batch partially failed",
	)
)

type ExpireDueResult struct {
	AsOf time.Time

	Selected        int
	Expired         int
	AlreadyTerminal int
	Failed          int

	FailedProvisioningIDs []string
}

func (s *Service) ExpireDue(
	ctx context.Context,
	limit int,
) (ExpireDueResult, error) {
	if ctx == nil || limit <= 0 {
		return ExpireDueResult{},
			ErrInvalidExpirationBatch
	}
	if err := ctx.Err(); err != nil {
		return ExpireDueResult{}, err
	}
	if s == nil || s.store == nil || s.clock == nil {
		return ExpireDueResult{}, ErrInternal
	}

	asOf, err := s.operationTime()
	if err != nil {
		return ExpireDueResult{}, err
	}
	result := ExpireDueResult{AsOf: asOf}

	records, err :=
		s.store.ListPendingAPIKeyProvisioningsDue(
			ctx,
			asOf,
			limit,
		)
	if err != nil {
		if isContextError(err) {
			return result, err
		}
		return result, ErrStoreUnavailable
	}
	result.Selected = len(records)

	if err := validateDueExpirationBatch(
		records,
		asOf,
		limit,
	); err != nil {
		return result, ErrStoreUnavailable
	}

	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		expired, expireErr :=
			s.store.ExpireAPIKeyProvisioning(
				ctx,
				record.ID,
				asOf,
			)
		if expireErr == nil {
			if validateExpiredProvisioning(
				record.ID,
				asOf,
				expired,
			) != nil {
				addExpirationFailure(
					&result,
					record.ID,
				)
				continue
			}
			result.Expired++
			continue
		}
		if isContextError(expireErr) {
			return result, expireErr
		}

		if errors.Is(
			expireErr,
			ports.ErrStoreConflict,
		) || errors.Is(
			expireErr,
			ports.ErrProvisioningExpired,
		) {
			current, findErr :=
				s.store.FindAPIKeyProvisioningByID(
					ctx,
					record.ID,
				)
			if findErr == nil &&
				current != nil &&
				validateTerminalProvisioning(
					record.ID,
					*current,
				) == nil {
				result.AlreadyTerminal++
				continue
			}
			if isContextError(findErr) {
				return result, findErr
			}
		}

		addExpirationFailure(
			&result,
			record.ID,
		)
	}

	if result.Failed > 0 {
		return result, ErrExpirationPartialFailure
	}
	return result, nil
}

func validateDueExpirationBatch(
	records []domain.APIKeyProvisioning,
	asOf time.Time,
	limit int,
) error {
	if len(records) > limit {
		return ErrStoreUnavailable
	}

	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if !validOpaque(record.ID) ||
			!validOpaque(record.UserID) ||
			!validOpaque(record.APIKeyID) ||
			record.ResultType !=
				domain.APIKeyProvisioningResultTypeKeyCreated ||
			record.Status !=
				domain.APIKeyProvisioningStatusPendingDelivery ||
			!validUTCTime(record.CreatedAt) ||
			!validUTCTime(record.UpdatedAt) ||
			record.ExpiresAt == nil ||
			!validUTCTime(*record.ExpiresAt) ||
			record.ExpiresAt.After(asOf) ||
			len(record.EncryptedRawKey) == 0 ||
			len(record.EncryptionNonce) == 0 ||
			!validOpaque(
				record.EncryptionKeyVersion,
			) ||
			record.DeliveredAt != nil ||
			record.ExpiredAt != nil {
			return ErrStoreUnavailable
		}
		if _, exists := seen[record.ID]; exists {
			return ErrStoreUnavailable
		}
		seen[record.ID] = struct{}{}
	}
	return nil
}

func validateExpiredProvisioning(
	expectedID string,
	asOf time.Time,
	record domain.APIKeyProvisioning,
) error {
	if record.ID != expectedID ||
		!validOpaque(record.UserID) ||
		!validOpaque(record.APIKeyID) ||
		record.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
		record.Status !=
			domain.APIKeyProvisioningStatusExpired ||
		!validUTCTime(record.CreatedAt) ||
		!validUTCTime(record.UpdatedAt) ||
		record.ExpiresAt == nil ||
		!validUTCTime(*record.ExpiresAt) ||
		record.ExpiredAt == nil ||
		!validUTCTime(*record.ExpiredAt) ||
		record.ExpiredAt.Before(*record.ExpiresAt) ||
		record.ExpiredAt.After(asOf) ||
		!record.UpdatedAt.Equal(*record.ExpiredAt) ||
		len(record.EncryptedRawKey) != 0 ||
		len(record.EncryptionNonce) != 0 ||
		!validOpaque(record.EncryptionKeyVersion) ||
		record.DeliveredAt != nil {
		return ErrStoreUnavailable
	}
	return nil
}

func validateTerminalProvisioning(
	expectedID string,
	record domain.APIKeyProvisioning,
) error {
	if record.ID != expectedID ||
		!validOpaque(record.UserID) ||
		!validOpaque(record.APIKeyID) ||
		len(record.EncryptedRawKey) != 0 ||
		len(record.EncryptionNonce) != 0 ||
		!validUTCTime(record.CreatedAt) ||
		!validUTCTime(record.UpdatedAt) {
		return ErrStoreUnavailable
	}

	switch record.Status {
	case domain.APIKeyProvisioningStatusDelivered:
		if record.DeliveredAt == nil ||
			!validUTCTime(*record.DeliveredAt) ||
			record.ExpiredAt != nil {
			return ErrStoreUnavailable
		}

	case domain.APIKeyProvisioningStatusExpired:
		if record.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			record.ExpiresAt == nil ||
			!validUTCTime(*record.ExpiresAt) ||
			record.ExpiredAt == nil ||
			!validUTCTime(*record.ExpiredAt) ||
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

func addExpirationFailure(
	result *ExpireDueResult,
	provisioningID string,
) {
	result.Failed++
	result.FailedProvisioningIDs = append(
		result.FailedProvisioningIDs,
		provisioningID,
	)
}
