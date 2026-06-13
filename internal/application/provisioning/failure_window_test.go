package provisioning

import (
	"context"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestProvisioningResponseLossReturnsSameRawKey(
	t *testing.T,
) {
	const rawAPIKey = "sk_live_same_key_after_response_loss"

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

	var persisted ports.APIKeyProvisioningResult
	created := false
	store := &testProvisioningStore{}
	store.provision = func(
		ctx context.Context,
		request ports.APIKeyProvisioningRequest,
		passedFactory ports.APIKeyProvisioningMaterialFactory,
	) (ports.APIKeyProvisioningResult, error) {
		if !created {
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
				return ports.APIKeyProvisioningResult{},
					err
			}

			persisted = resultFor(
				request,
				ports.APIKeyProvisioningOutcomeCreated,
			)
			persisted.APIKey = material.APIKey
			persisted.Provisioning.EncryptedRawKey =
				material.EncryptedRawKey
			persisted.Provisioning.EncryptionNonce =
				material.EncryptionNonce
			persisted.Provisioning.EncryptionKeyVersion =
				material.EncryptionKeyVersion
			created = true
			return persisted, nil
		}

		replay := persisted
		replay.Outcome =
			ports.APIKeyProvisioningOutcomeReplayedPending
		return replay, nil
	}
	store.recordAttempt = func(
		_ context.Context,
		id string,
		attemptedAt time.Time,
	) (domain.APIKeyProvisioning, error) {
		if id != persisted.Provisioning.ID {
			t.Fatalf(
				"attempt provisioning id = %q, want %q",
				id,
				persisted.Provisioning.ID,
			)
		}
		updated := persisted.Provisioning
		updated.DeliveryAttempts++
		updated.UpdatedAt = attemptedAt
		persisted.Provisioning = updated
		return updated, nil
	}

	service := newServiceForTest(
		t,
		store,
		factory,
		decryptor,
	)
	input := validInput()

	first, err := service.Provision(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("first Provision: %v", err)
	}

	// The trusted caller loses the first HTTP response and
	// retries with the same Idempotency-Key and input.
	second, err := service.Provision(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("retry Provision: %v", err)
	}

	if first.Result != ResultTypeCreated ||
		second.Result != ResultTypeReplayed {
		t.Fatalf(
			"first result=%q second result=%q",
			first.Result,
			second.Result,
		)
	}
	if first.APIKey != rawAPIKey ||
		second.APIKey != rawAPIKey {
		t.Fatalf(
			"first raw=%q second raw=%q",
			first.APIKey,
			second.APIKey,
		)
	}
	if first.ProvisioningID !=
		second.ProvisioningID ||
		first.APIKeyID != second.APIKeyID ||
		first.KeyPrefix != second.KeyPrefix {
		t.Fatalf(
			"first=%+v second=%+v",
			first,
			second,
		)
	}
	if store.provisionCalls != 2 ||
		factory.calls != 1 ||
		decryptor.calls != 2 ||
		store.recordAttemptCalls != 2 ||
		persisted.Provisioning.DeliveryAttempts != 2 {
		t.Fatalf(
			"provisions=%d factory=%d decrypts=%d "+
				"attempts=%d persisted_attempts=%d",
			store.provisionCalls,
			factory.calls,
			decryptor.calls,
			store.recordAttemptCalls,
			persisted.Provisioning.DeliveryAttempts,
		)
	}
}

func TestConfirmResponseLossIsIdempotentAndBlocksRawRecovery(
	t *testing.T,
) {
	const rawAPIKey = "sk_live_delivered_key_must_not_recover"

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

	var persisted ports.APIKeyProvisioningResult
	created := false
	store := &testProvisioningStore{}
	store.provision = func(
		ctx context.Context,
		request ports.APIKeyProvisioningRequest,
		passedFactory ports.APIKeyProvisioningMaterialFactory,
	) (ports.APIKeyProvisioningResult, error) {
		if !created {
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
				return ports.APIKeyProvisioningResult{},
					err
			}

			persisted = resultFor(
				request,
				ports.APIKeyProvisioningOutcomeCreated,
			)
			persisted.APIKey = material.APIKey
			persisted.Provisioning.EncryptedRawKey =
				material.EncryptedRawKey
			persisted.Provisioning.EncryptionNonce =
				material.EncryptionNonce
			persisted.Provisioning.EncryptionKeyVersion =
				material.EncryptionKeyVersion
			created = true
			return persisted, nil
		}

		replay := persisted
		if persisted.Provisioning.Status ==
			domain.APIKeyProvisioningStatusDelivered {
			replay.Outcome =
				ports.APIKeyProvisioningOutcomeReplayedDelivered
		} else {
			replay.Outcome =
				ports.APIKeyProvisioningOutcomeReplayedPending
		}
		return replay, nil
	}
	store.recordAttempt = func(
		_ context.Context,
		_ string,
		attemptedAt time.Time,
	) (domain.APIKeyProvisioning, error) {
		updated := persisted.Provisioning
		updated.DeliveryAttempts++
		updated.UpdatedAt = attemptedAt
		persisted.Provisioning = updated
		return updated, nil
	}
	store.find = func(
		_ context.Context,
		id string,
	) (*domain.APIKeyProvisioning, error) {
		if id != persisted.Provisioning.ID {
			return nil, ports.ErrNotFound
		}
		current := persisted.Provisioning
		return &current, nil
	}
	store.confirm = func(
		_ context.Context,
		id string,
		deliveredAt time.Time,
	) (domain.APIKeyProvisioning, error) {
		if id != persisted.Provisioning.ID {
			return domain.APIKeyProvisioning{},
				ports.ErrNotFound
		}
		if persisted.Provisioning.Status !=
			domain.APIKeyProvisioningStatusPendingDelivery {
			t.Fatalf(
				"unexpected status before confirm: %q",
				persisted.Provisioning.Status,
			)
		}

		delivered := persisted.Provisioning
		delivered.Status =
			domain.APIKeyProvisioningStatusDelivered
		delivered.EncryptedRawKey = nil
		delivered.EncryptionNonce = nil
		delivered.UpdatedAt = deliveredAt
		delivered.DeliveredAt = &deliveredAt
		persisted.Provisioning = delivered
		return delivered, nil
	}

	service := newServiceForTest(
		t,
		store,
		factory,
		decryptor,
	)
	input := validInput()

	provisioned, err := service.Provision(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if provisioned.APIKey != rawAPIKey {
		t.Fatalf(
			"provisioned raw key = %q",
			provisioned.APIKey,
		)
	}

	firstConfirm, err := service.ConfirmDelivery(
		context.Background(),
		provisioned.ProvisioningID,
	)
	if err != nil {
		t.Fatalf("first ConfirmDelivery: %v", err)
	}

	// The trusted caller loses the first confirm response and
	// retries the same confirmation.
	secondConfirm, err := service.ConfirmDelivery(
		context.Background(),
		provisioned.ProvisioningID,
	)
	if err != nil {
		t.Fatalf("retry ConfirmDelivery: %v", err)
	}

	if firstConfirm.Status !=
		domain.APIKeyProvisioningStatusDelivered ||
		secondConfirm.Status !=
			domain.APIKeyProvisioningStatusDelivered ||
		firstConfirm.DeliveredAt == nil ||
		secondConfirm.DeliveredAt == nil ||
		!firstConfirm.DeliveredAt.Equal(
			*secondConfirm.DeliveredAt,
		) {
		t.Fatalf(
			"first=%+v second=%+v",
			firstConfirm,
			secondConfirm,
		)
	}
	if store.confirmCalls != 1 ||
		store.findCalls != 2 {
		t.Fatalf(
			"confirm calls=%d find calls=%d",
			store.confirmCalls,
			store.findCalls,
		)
	}
	if len(
		persisted.Provisioning.EncryptedRawKey,
	) != 0 ||
		len(
			persisted.Provisioning.EncryptionNonce,
		) != 0 {
		t.Fatal(
			"delivered record retained encrypted material",
		)
	}

	replay, err := service.Provision(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf(
			"Provision after delivery: %v",
			err,
		)
	}
	if replay.Result != ResultTypeReplayed ||
		replay.ProvisioningStatus !=
			domain.APIKeyProvisioningStatusDelivered ||
		replay.APIKey != "" {
		t.Fatalf(
			"delivered replay recovered raw key: %+v",
			replay,
		)
	}
	if decryptor.calls != 1 ||
		store.recordAttemptCalls != 1 {
		t.Fatalf(
			"decrypts=%d attempts=%d",
			decryptor.calls,
			store.recordAttemptCalls,
		)
	}
}
