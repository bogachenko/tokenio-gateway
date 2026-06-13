package provisioning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const (
	defaultKeyName = "Provisioned API key"

	userIDDomain         = "tokenio:provisioning:user-id:v1"
	provisioningIDDomain = "tokenio:provisioning:record-id:v1"
	apiKeyIDDomain       = "tokenio:provisioning:api-key-id:v1"
)

type Dependencies struct {
	Store             ports.APIKeyProvisioningStore
	MaterialFactory   ports.APIKeyProvisioningMaterialFactory
	MaterialDecryptor ports.APIKeyProvisioningMaterialDecryptor
	Clock             ports.Clock
	TTL               time.Duration
}

type Service struct {
	store             ports.APIKeyProvisioningStore
	materialFactory   ports.APIKeyProvisioningMaterialFactory
	materialDecryptor ports.APIKeyProvisioningMaterialDecryptor
	clock             ports.Clock
	ttl               time.Duration
}

func NewService(deps Dependencies) (*Service, error) {
	if deps.Store == nil ||
		deps.MaterialFactory == nil ||
		deps.MaterialDecryptor == nil ||
		deps.Clock == nil ||
		deps.TTL <= 0 {
		return nil, ErrInternal
	}
	return &Service{
		store:             deps.Store,
		materialFactory:   deps.MaterialFactory,
		materialDecryptor: deps.MaterialDecryptor,
		clock:             deps.Clock,
		ttl:               deps.TTL,
	}, nil
}

func (s *Service) Provision(
	ctx context.Context,
	input ProvisionInput,
) (ProvisionResult, error) {
	if ctx == nil {
		return ProvisionResult{}, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return ProvisionResult{}, err
	}
	if s == nil ||
		s.store == nil ||
		s.materialFactory == nil ||
		s.materialDecryptor == nil ||
		s.clock == nil ||
		s.ttl <= 0 {
		return ProvisionResult{}, ErrInternal
	}

	request, now, err := s.buildStoreRequest(input)
	if err != nil {
		return ProvisionResult{}, err
	}

	trackedFactory := &trackingMaterialFactory{
		inner: s.materialFactory,
	}
	stored, err := s.store.ProvisionAPIKey(
		ctx,
		request,
		trackedFactory,
	)
	if err != nil {
		return ProvisionResult{}, mapProvisionError(
			err,
			trackedFactory.failed,
		)
	}
	if err := validateProvisioningResult(
		request,
		stored,
	); err != nil {
		return ProvisionResult{}, err
	}

	switch stored.Outcome {
	case ports.APIKeyProvisioningOutcomeExpired:
		return ProvisionResult{}, ErrExpired

	case ports.APIKeyProvisioningOutcomeCreated,
		ports.APIKeyProvisioningOutcomeReplayedPending:
		if stored.Provisioning.ExpiresAt == nil {
			return ProvisionResult{}, ErrStoreUnavailable
		}
		if !stored.Provisioning.ExpiresAt.After(now) {
			_, expireErr := s.store.ExpireAPIKeyProvisioning(
				ctx,
				stored.Provisioning.ID,
				now,
			)
			if expireErr == nil ||
				errors.Is(
					expireErr,
					ports.ErrProvisioningExpired,
				) {
				return ProvisionResult{}, ErrExpired
			}
			return ProvisionResult{},
				mapStoreError(expireErr, false)
		}

		rawAPIKey, decryptErr :=
			s.materialDecryptor.DecryptProvisioningMaterial(
				ctx,
				stored.Provisioning,
				stored.APIKey,
			)
		if decryptErr != nil {
			if isContextError(decryptErr) {
				return ProvisionResult{}, decryptErr
			}
			return ProvisionResult{}, ErrCryptoUnavailable
		}
		if rawAPIKey == "" {
			return ProvisionResult{}, ErrCryptoUnavailable
		}

		attempted, attemptErr :=
			s.store.RecordAPIKeyDeliveryAttempt(
				ctx,
				stored.Provisioning.ID,
				now,
			)
		if attemptErr != nil {
			return ProvisionResult{},
				mapStoreError(attemptErr, false)
		}
		if err := validateDeliveryAttemptRecord(
			stored.Provisioning,
			attempted,
			now,
		); err != nil {
			return ProvisionResult{}, err
		}
		stored.Provisioning = attempted

		result := provisioningView(stored)
		result.APIKey = rawAPIKey
		return result, nil

	case ports.APIKeyProvisioningOutcomeReplayedDelivered,
		ports.APIKeyProvisioningOutcomeAlreadyProvisioned:
		return provisioningView(stored), nil

	default:
		return ProvisionResult{}, ErrStoreUnavailable
	}
}

func (s *Service) ConfirmDelivery(
	ctx context.Context,
	provisioningID string,
) (ConfirmDeliveryResult, error) {
	if ctx == nil ||
		!validOpaque(provisioningID) {
		return ConfirmDeliveryResult{}, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return ConfirmDeliveryResult{}, err
	}
	if s == nil || s.store == nil || s.clock == nil {
		return ConfirmDeliveryResult{}, ErrInternal
	}

	now, err := s.operationTime()
	if err != nil {
		return ConfirmDeliveryResult{}, err
	}

	current, err :=
		s.store.FindAPIKeyProvisioningByID(
			ctx,
			provisioningID,
		)
	if err != nil {
		return ConfirmDeliveryResult{},
			mapStoreError(err, true)
	}
	if err := validateConfirmationRecord(
		provisioningID,
		*current,
	); err != nil {
		return ConfirmDeliveryResult{}, err
	}

	switch current.Status {
	case domain.APIKeyProvisioningStatusExpired:
		return ConfirmDeliveryResult{}, ErrExpired

	case domain.APIKeyProvisioningStatusDelivered:
		return confirmationView(*current), nil

	case domain.APIKeyProvisioningStatusPendingDelivery:
		if current.ExpiresAt == nil {
			return ConfirmDeliveryResult{},
				ErrStoreUnavailable
		}
		if !current.ExpiresAt.After(now) {
			_, expireErr :=
				s.store.ExpireAPIKeyProvisioning(
					ctx,
					provisioningID,
					now,
				)
			if expireErr == nil ||
				errors.Is(
					expireErr,
					ports.ErrProvisioningExpired,
				) {
				return ConfirmDeliveryResult{}, ErrExpired
			}
			return ConfirmDeliveryResult{},
				mapStoreError(expireErr, false)
		}

	default:
		return ConfirmDeliveryResult{},
			ErrStoreUnavailable
	}

	confirmed, err := s.store.ConfirmAPIKeyDelivery(
		ctx,
		provisioningID,
		now,
	)
	if err != nil {
		return ConfirmDeliveryResult{},
			mapConfirmError(err)
	}
	if err := validateConfirmationRecord(
		provisioningID,
		confirmed,
	); err != nil {
		return ConfirmDeliveryResult{}, err
	}
	if confirmed.Status !=
		domain.APIKeyProvisioningStatusDelivered {
		return ConfirmDeliveryResult{},
			ErrStoreUnavailable
	}
	return confirmationView(confirmed), nil
}

func (s *Service) buildStoreRequest(
	input ProvisionInput,
) (ports.APIKeyProvisioningRequest, time.Time, error) {
	if !validOpaque(input.IdempotencyKey) ||
		!validOpaque(input.ExternalBillingUserID) ||
		!validOpaque(input.SourceReference) {
		return ports.APIKeyProvisioningRequest{},
			time.Time{},
			ErrInvalidRequest
	}

	keyName := input.KeyName
	if keyName == "" {
		keyName = defaultKeyName
	} else if !validOpaque(keyName) {
		return ports.APIKeyProvisioningRequest{},
			time.Time{},
			ErrInvalidRequest
	}

	now, err := s.operationTime()
	if err != nil {
		return ports.APIKeyProvisioningRequest{},
			time.Time{},
			err
	}
	expiresAt := now.Add(s.ttl)
	if !expiresAt.After(now) ||
		expiresAt.Location() != time.UTC {
		return ports.APIKeyProvisioningRequest{},
			time.Time{},
			ErrInternal
	}

	sourceHash := sha256.Sum256(
		[]byte(input.SourceReference),
	)
	userID := stableID(
		"usr_",
		userIDDomain,
		input.ExternalBillingUserID,
	)
	provisioningID := stableID(
		"prov_",
		provisioningIDDomain,
		input.IdempotencyKey,
	)
	apiKeyID := stableID(
		"ak_",
		apiKeyIDDomain,
		input.IdempotencyKey,
	)

	return ports.APIKeyProvisioningRequest{
		IdempotencyKey:        input.IdempotencyKey,
		SourceReferenceHash:   hex.EncodeToString(sourceHash[:]),
		ExternalBillingUserID: input.ExternalBillingUserID,
		NewUser: domain.User{
			ID:                    userID,
			ExternalBillingUserID: input.ExternalBillingUserID,
			Enabled:               true,
			CreatedAt:             now,
			UpdatedAt:             now,
		},
		ProvisioningID: provisioningID,
		APIKeyID:       apiKeyID,
		KeyName:        keyName,
		CreatedAt:      now,
		ExpiresAt:      expiresAt,
	}, now, nil
}

func (s *Service) operationTime() (time.Time, error) {
	now := s.clock.Now()
	if now.IsZero() {
		return time.Time{}, ErrInternal
	}
	return now.UTC(), nil
}

type trackingMaterialFactory struct {
	inner  ports.APIKeyProvisioningMaterialFactory
	failed bool
}

func (f *trackingMaterialFactory) CreateProvisioningMaterial(
	ctx context.Context,
	request ports.APIKeyProvisioningMaterialRequest,
) (ports.APIKeyProvisioningMaterial, error) {
	material, err :=
		f.inner.CreateProvisioningMaterial(ctx, request)
	if err != nil {
		f.failed = true
	}
	return material, err
}

func mapProvisionError(
	err error,
	materialFactoryFailed bool,
) error {
	if isContextError(err) {
		return err
	}
	if materialFactoryFailed {
		return ErrCryptoUnavailable
	}
	return mapStoreError(err, false)
}

func mapConfirmError(err error) error {
	if isContextError(err) {
		return err
	}
	switch {
	case errors.Is(err, ports.ErrProvisioningExpired):
		return ErrExpired
	case errors.Is(err, ports.ErrStoreConflict):
		return ErrConflict
	case errors.Is(err, ports.ErrNotFound):
		return ErrInvalidRequest
	default:
		return ErrStoreUnavailable
	}
}

func mapStoreError(
	err error,
	notFoundIsInvalidRequest bool,
) error {
	if isContextError(err) {
		return err
	}
	switch {
	case errors.Is(err, ports.ErrProvisioningExpired):
		return ErrExpired
	case errors.Is(
		err,
		ports.ErrProvisioningUserDisabled,
	):
		return ErrConflict
	case errors.Is(err, ports.ErrStoreConflict):
		return ErrConflict
	case errors.Is(err, ports.ErrNotFound) &&
		notFoundIsInvalidRequest:
		return ErrInvalidRequest
	default:
		return ErrStoreUnavailable
	}
}

func validateProvisioningResult(
	request ports.APIKeyProvisioningRequest,
	result ports.APIKeyProvisioningResult,
) error {
	provisioning := result.Provisioning
	user := result.User
	apiKey := result.APIKey

	if provisioning.ID != request.ProvisioningID ||
		provisioning.IdempotencyKey !=
			request.IdempotencyKey ||
		provisioning.SourceReferenceHash !=
			request.SourceReferenceHash ||
		provisioning.ExternalBillingUserID !=
			request.ExternalBillingUserID ||
		provisioning.UserID == "" ||
		provisioning.APIKeyID == "" ||
		user.ID != provisioning.UserID ||
		user.ExternalBillingUserID !=
			request.ExternalBillingUserID ||
		apiKey.ID != provisioning.APIKeyID ||
		apiKey.UserID != user.ID ||
		!validUTCTime(provisioning.CreatedAt) ||
		!validUTCTime(provisioning.UpdatedAt) ||
		!validUTCTime(user.CreatedAt) ||
		!validUTCTime(user.UpdatedAt) ||
		!validUTCTime(apiKey.CreatedAt) ||
		!validUTCTime(apiKey.UpdatedAt) ||
		!validKeyHash(apiKey.KeyHash) ||
		apiKey.KeyPrefix == "" {
		return ErrStoreUnavailable
	}

	if result.Outcome !=
		ports.APIKeyProvisioningOutcomeExpired &&
		(!user.Enabled || user.DisabledAt != nil) {
		return ErrConflict
	}

	switch result.Outcome {
	case ports.APIKeyProvisioningOutcomeCreated,
		ports.APIKeyProvisioningOutcomeReplayedPending:
		if provisioning.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			provisioning.Status !=
				domain.APIKeyProvisioningStatusPendingDelivery ||
			provisioning.APIKeyID != request.APIKeyID ||
			apiKey.Name != request.KeyName ||
			!apiKey.Enabled ||
			apiKey.RevokedAt != nil ||
			apiKey.ExpiresAt != nil ||
			provisioning.ExpiresAt == nil ||
			!validUTCTime(*provisioning.ExpiresAt) ||
			len(provisioning.EncryptedRawKey) == 0 ||
			len(provisioning.EncryptionNonce) == 0 ||
			provisioning.EncryptionKeyVersion == "" ||
			provisioning.DeliveredAt != nil ||
			provisioning.ExpiredAt != nil {
			return ErrStoreUnavailable
		}

	case ports.APIKeyProvisioningOutcomeReplayedDelivered:
		if provisioning.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			provisioning.Status !=
				domain.APIKeyProvisioningStatusDelivered ||
			provisioning.APIKeyID != request.APIKeyID ||
			apiKey.Name != request.KeyName ||
			!apiKey.Enabled ||
			apiKey.RevokedAt != nil ||
			len(provisioning.EncryptedRawKey) != 0 ||
			len(provisioning.EncryptionNonce) != 0 ||
			provisioning.EncryptionKeyVersion == "" ||
			provisioning.ExpiresAt == nil ||
			!validUTCTime(*provisioning.ExpiresAt) ||
			provisioning.DeliveredAt == nil ||
			!validUTCTime(*provisioning.DeliveredAt) ||
			provisioning.ExpiredAt != nil {
			return ErrStoreUnavailable
		}

	case ports.APIKeyProvisioningOutcomeAlreadyProvisioned:
		if provisioning.ResultType !=
			domain.APIKeyProvisioningResultTypeAlreadyProvisioned ||
			provisioning.Status !=
				domain.APIKeyProvisioningStatusDelivered ||
			!apiKey.Enabled ||
			apiKey.RevokedAt != nil ||
			len(provisioning.EncryptedRawKey) != 0 ||
			len(provisioning.EncryptionNonce) != 0 ||
			provisioning.EncryptionKeyVersion != "" ||
			provisioning.ExpiresAt != nil ||
			provisioning.DeliveredAt == nil ||
			!validUTCTime(*provisioning.DeliveredAt) ||
			provisioning.ExpiredAt != nil {
			return ErrStoreUnavailable
		}

	case ports.APIKeyProvisioningOutcomeExpired:
		if provisioning.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			provisioning.Status !=
				domain.APIKeyProvisioningStatusExpired ||
			provisioning.APIKeyID != request.APIKeyID ||
			apiKey.Name != request.KeyName ||
			apiKey.Enabled ||
			apiKey.RevokedAt == nil ||
			!validUTCTime(*apiKey.RevokedAt) ||
			len(provisioning.EncryptedRawKey) != 0 ||
			len(provisioning.EncryptionNonce) != 0 ||
			provisioning.EncryptionKeyVersion == "" ||
			provisioning.ExpiresAt == nil ||
			!validUTCTime(*provisioning.ExpiresAt) ||
			provisioning.ExpiredAt == nil ||
			!validUTCTime(*provisioning.ExpiredAt) ||
			provisioning.DeliveredAt != nil {
			return ErrStoreUnavailable
		}

	default:
		return ErrStoreUnavailable
	}

	return nil
}

func validateDeliveryAttemptRecord(
	before domain.APIKeyProvisioning,
	after domain.APIKeyProvisioning,
	attemptedAt time.Time,
) error {
	if before.DeliveryAttempts < 0 ||
		after.ID != before.ID ||
		after.IdempotencyKey != before.IdempotencyKey ||
		after.SourceReferenceHash != before.SourceReferenceHash ||
		after.ExternalBillingUserID != before.ExternalBillingUserID ||
		after.UserID != before.UserID ||
		after.APIKeyID != before.APIKeyID ||
		after.ResultType != before.ResultType ||
		after.Status != domain.APIKeyProvisioningStatusPendingDelivery ||
		after.DeliveryAttempts != before.DeliveryAttempts+1 ||
		!after.CreatedAt.Equal(before.CreatedAt) ||
		!validUTCTime(after.CreatedAt) ||
		!validUTCTime(after.UpdatedAt) ||
		after.UpdatedAt.Before(before.UpdatedAt) ||
		after.UpdatedAt.After(attemptedAt) ||
		!bytes.Equal(after.EncryptedRawKey, before.EncryptedRawKey) ||
		!bytes.Equal(after.EncryptionNonce, before.EncryptionNonce) ||
		after.EncryptionKeyVersion != before.EncryptionKeyVersion ||
		after.DeliveredAt != nil ||
		after.ExpiredAt != nil {
		return ErrStoreUnavailable
	}

	if before.ExpiresAt == nil ||
		after.ExpiresAt == nil ||
		!before.ExpiresAt.Equal(*after.ExpiresAt) ||
		!validUTCTime(*after.ExpiresAt) ||
		!after.ExpiresAt.After(attemptedAt) {
		return ErrStoreUnavailable
	}
	return nil
}

func validateConfirmationRecord(
	expectedID string,
	record domain.APIKeyProvisioning,
) error {
	if record.ID != expectedID ||
		record.UserID == "" ||
		record.APIKeyID == "" ||
		!validUTCTime(record.CreatedAt) ||
		!validUTCTime(record.UpdatedAt) {
		return ErrStoreUnavailable
	}

	switch record.Status {
	case domain.APIKeyProvisioningStatusPendingDelivery:
		if record.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			record.ExpiresAt == nil ||
			!validUTCTime(*record.ExpiresAt) ||
			len(record.EncryptedRawKey) == 0 ||
			len(record.EncryptionNonce) == 0 ||
			record.EncryptionKeyVersion == "" ||
			record.DeliveredAt != nil ||
			record.ExpiredAt != nil {
			return ErrStoreUnavailable
		}

	case domain.APIKeyProvisioningStatusDelivered:
		if len(record.EncryptedRawKey) != 0 ||
			len(record.EncryptionNonce) != 0 ||
			record.DeliveredAt == nil ||
			!validUTCTime(*record.DeliveredAt) ||
			record.ExpiredAt != nil {
			return ErrStoreUnavailable
		}

	case domain.APIKeyProvisioningStatusExpired:
		if record.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
			len(record.EncryptedRawKey) != 0 ||
			len(record.EncryptionNonce) != 0 ||
			record.ExpiresAt == nil ||
			!validUTCTime(*record.ExpiresAt) ||
			record.ExpiredAt == nil ||
			!validUTCTime(*record.ExpiredAt) ||
			record.DeliveredAt != nil {
			return ErrStoreUnavailable
		}

	default:
		return ErrStoreUnavailable
	}

	return nil
}

func provisioningView(
	result ports.APIKeyProvisioningResult,
) ProvisionResult {
	resultType := ResultTypeReplayed
	switch result.Outcome {
	case ports.APIKeyProvisioningOutcomeCreated:
		resultType = ResultTypeCreated
	case ports.APIKeyProvisioningOutcomeAlreadyProvisioned:
		resultType = ResultTypeAlreadyProvisioned
	}

	return ProvisionResult{
		Result:             resultType,
		ProvisioningID:     result.Provisioning.ID,
		ProvisioningStatus: result.Provisioning.Status,
		APIKeyID:           result.APIKey.ID,
		KeyPrefix:          result.APIKey.KeyPrefix,
		ExpiresAt:          cloneTime(result.Provisioning.ExpiresAt),
	}
}

func confirmationView(
	record domain.APIKeyProvisioning,
) ConfirmDeliveryResult {
	return ConfirmDeliveryResult{
		ProvisioningID: record.ID,
		Status:         record.Status,
		DeliveredAt:    cloneTime(record.DeliveredAt),
	}
}

func stableID(
	prefix string,
	domain string,
	values ...string,
) string {
	digest := sha256.New()
	writeStableValue(digest, domain)
	for _, value := range values {
		writeStableValue(digest, value)
	}
	return prefix + hex.EncodeToString(digest.Sum(nil))
}

func writeStableValue(digest hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(
		length[:],
		uint64(len(value)),
	)
	_, _ = digest.Write(length[:])
	_, _ = digest.Write([]byte(value))
}

func validOpaque(value string) bool {
	return value != "" &&
		value == strings.TrimSpace(value)
}

func validUTCTime(value time.Time) bool {
	return !value.IsZero() &&
		value.Location() == time.UTC
}

func validKeyHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil &&
		len(decoded) == sha256.Size
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}
