package cryptomaterial

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type fixedGenerator struct {
	raw   string
	err   error
	calls int
}

func (g *fixedGenerator) GenerateAPIKey() (string, error) {
	g.calls++
	return g.raw, g.err
}

type hmacHasher struct {
	secret []byte
}

func (h hmacHasher) Hash(raw string) string {
	mac := hmac.New(sha256.New, h.secret)
	_, _ = mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}

func testRawAPIKey(fill byte) string {
	return rawAPIKeyPrefix +
		base64.RawURLEncoding.EncodeToString(
			bytes.Repeat([]byte{fill}, 32),
		)
}

func testMaterialRequest() ports.APIKeyProvisioningMaterialRequest {
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
	return ports.APIKeyProvisioningMaterialRequest{
		User: domain.User{
			ID:                    "usr_1",
			ExternalBillingUserID: "billing_usr_1",
			Enabled:               true,
			CreatedAt:             createdAt,
			UpdatedAt:             createdAt,
		},
		ProvisioningID: "prov_1",
		APIKeyID:       "ak_1",
		KeyName:        "Telegram payment key",
		CreatedAt:      createdAt,
		ExpiresAt:      createdAt.Add(24 * time.Hour),
	}
}

func newTestService(
	t *testing.T,
	generator ports.APIKeyGenerator,
	nonceSource []byte,
) *Service {
	t.Helper()
	service, err := newWithNonceReader(
		bytes.Repeat([]byte{0x42}, 32),
		"v1",
		generator,
		hmacHasher{secret: []byte("hash-secret")},
		bytes.NewReader(nonceSource),
	)
	if err != nil {
		t.Fatalf("newWithNonceReader: %v", err)
	}
	return service
}

func materialEnvelope(
	material ports.APIKeyProvisioningMaterial,
	request ports.APIKeyProvisioningMaterialRequest,
) (domain.APIKeyProvisioning, domain.APIKeyRecord) {
	expiresAt := request.ExpiresAt
	return domain.APIKeyProvisioning{
		ID:                    request.ProvisioningID,
		IdempotencyKey:        "payment-1",
		SourceReferenceHash:   strings.Repeat("a", 64),
		ExternalBillingUserID: request.User.ExternalBillingUserID,
		UserID:                request.User.ID,
		APIKeyID:              request.APIKeyID,
		ResultType:            domain.APIKeyProvisioningResultTypeKeyCreated,
		Status:                domain.APIKeyProvisioningStatusPendingDelivery,
		EncryptedRawKey:       material.EncryptedRawKey,
		EncryptionNonce:       material.EncryptionNonce,
		EncryptionKeyVersion:  material.EncryptionKeyVersion,
		CreatedAt:             request.CreatedAt,
		UpdatedAt:             request.CreatedAt,
		ExpiresAt:             &expiresAt,
	}, material.APIKey
}

func TestCreateAndDecryptProvisioningMaterial(t *testing.T) {
	rawAPIKey := testRawAPIKey(0x7f)
	generator := &fixedGenerator{raw: rawAPIKey}
	service := newTestService(
		t,
		generator,
		bytes.Repeat([]byte{0x11}, 12),
	)
	request := testMaterialRequest()

	material, err := service.CreateProvisioningMaterial(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf("CreateProvisioningMaterial: %v", err)
	}
	if generator.calls != 1 {
		t.Fatalf("generator calls = %d, want 1", generator.calls)
	}
	if material.APIKey.ID != request.APIKeyID ||
		material.APIKey.UserID != request.User.ID ||
		material.APIKey.Name != request.KeyName ||
		!material.APIKey.Enabled ||
		material.APIKey.KeyHash == "" ||
		material.APIKey.KeyPrefix !=
			displayPrefix(rawAPIKey) {
		t.Fatalf("API key record = %+v", material.APIKey)
	}
	if bytes.Contains(
		material.EncryptedRawKey,
		[]byte(rawAPIKey),
	) {
		t.Fatal("ciphertext contains plaintext API key")
	}
	if len(material.EncryptionNonce) != service.aead.NonceSize() {
		t.Fatalf(
			"nonce length = %d, want %d",
			len(material.EncryptionNonce),
			service.aead.NonceSize(),
		)
	}
	if material.EncryptionKeyVersion != "v1" {
		t.Fatalf(
			"key version = %q",
			material.EncryptionKeyVersion,
		)
	}

	provisioning, apiKey := materialEnvelope(material, request)
	decrypted, err := service.DecryptProvisioningMaterial(
		context.Background(),
		provisioning,
		apiKey,
	)
	if err != nil {
		t.Fatalf("DecryptProvisioningMaterial: %v", err)
	}
	if decrypted != rawAPIKey {
		t.Fatalf("decrypted API key does not match generated key")
	}
}

func TestCreateProvisioningMaterialUsesUniqueNonce(t *testing.T) {
	rawAPIKey := testRawAPIKey(0x31)
	firstNonce := bytes.Repeat([]byte{0x01}, 12)
	secondNonce := bytes.Repeat([]byte{0x02}, 12)
	service := newTestService(
		t,
		&fixedGenerator{raw: rawAPIKey},
		append(firstNonce, secondNonce...),
	)
	request := testMaterialRequest()

	first, err := service.CreateProvisioningMaterial(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateProvisioningMaterial(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(
		first.EncryptionNonce,
		second.EncryptionNonce,
	) {
		t.Fatal("provisioning nonces are equal")
	}
	if bytes.Equal(
		first.EncryptedRawKey,
		second.EncryptedRawKey,
	) {
		t.Fatal("ciphertexts are equal for different nonces")
	}
}

func TestAuthenticatedDataRejectsRecordSubstitution(t *testing.T) {
	rawAPIKey := testRawAPIKey(0x41)
	service := newTestService(
		t,
		&fixedGenerator{raw: rawAPIKey},
		bytes.Repeat([]byte{0x03}, 12),
	)
	request := testMaterialRequest()
	material, err := service.CreateProvisioningMaterial(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}
	originalProvisioning, originalAPIKey :=
		materialEnvelope(material, request)

	tests := []struct {
		name         string
		provisioning domain.APIKeyProvisioning
		apiKey       domain.APIKeyRecord
	}{
		{
			name: "provisioning ID",
			provisioning: func() domain.APIKeyProvisioning {
				value := originalProvisioning
				value.ID = "prov_other"
				return value
			}(),
			apiKey: originalAPIKey,
		},
		{
			name: "API key ID",
			provisioning: func() domain.APIKeyProvisioning {
				value := originalProvisioning
				value.APIKeyID = "ak_other"
				return value
			}(),
			apiKey: func() domain.APIKeyRecord {
				value := originalAPIKey
				value.ID = "ak_other"
				return value
			}(),
		},
		{
			name: "user ID",
			provisioning: func() domain.APIKeyProvisioning {
				value := originalProvisioning
				value.UserID = "usr_other"
				return value
			}(),
			apiKey: func() domain.APIKeyRecord {
				value := originalAPIKey
				value.UserID = "usr_other"
				return value
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.DecryptProvisioningMaterial(
				context.Background(),
				test.provisioning,
				test.apiKey,
			)
			if !errors.Is(err, ErrDecryptionFailed) {
				t.Fatalf(
					"error = %v, want ErrDecryptionFailed",
					err,
				)
			}
		})
	}
}

func TestDecryptRejectsPersistedContractMismatch(t *testing.T) {
	rawAPIKey := testRawAPIKey(0x55)
	service := newTestService(
		t,
		&fixedGenerator{raw: rawAPIKey},
		bytes.Repeat([]byte{0x04}, 12),
	)
	request := testMaterialRequest()
	material, err := service.CreateProvisioningMaterial(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}
	originalProvisioning, originalAPIKey :=
		materialEnvelope(material, request)

	tests := []struct {
		name         string
		provisioning domain.APIKeyProvisioning
		apiKey       domain.APIKeyRecord
	}{
		{
			name: "terminal status",
			provisioning: func() domain.APIKeyProvisioning {
				value := originalProvisioning
				value.Status =
					domain.APIKeyProvisioningStatusDelivered
				return value
			}(),
			apiKey: originalAPIKey,
		},
		{
			name: "wrong key version",
			provisioning: func() domain.APIKeyProvisioning {
				value := originalProvisioning
				value.EncryptionKeyVersion = "v2"
				return value
			}(),
			apiKey: originalAPIKey,
		},
		{
			name:         "hash mismatch",
			provisioning: originalProvisioning,
			apiKey: func() domain.APIKeyRecord {
				value := originalAPIKey
				value.KeyHash = strings.Repeat("0", 64)
				return value
			}(),
		},
		{
			name:         "prefix mismatch",
			provisioning: originalProvisioning,
			apiKey: func() domain.APIKeyRecord {
				value := originalAPIKey
				value.KeyPrefix = "sk_live_wrong..."
				return value
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.DecryptProvisioningMaterial(
				context.Background(),
				test.provisioning,
				test.apiKey,
			)
			if !errors.Is(err, ErrInvalidMaterial) {
				t.Fatalf(
					"error = %v, want ErrInvalidMaterial",
					err,
				)
			}
		})
	}
}

func TestCreateProvisioningMaterialPropagatesSafeFailures(
	t *testing.T,
) {
	rawAPIKey := testRawAPIKey(0x61)
	request := testMaterialRequest()

	t.Run("cancelled context", func(t *testing.T) {
		generator := &fixedGenerator{raw: rawAPIKey}
		service := newTestService(
			t,
			generator,
			bytes.Repeat([]byte{0x05}, 12),
		)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := service.CreateProvisioningMaterial(ctx, request)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
		if generator.calls != 0 {
			t.Fatalf(
				"generator calls = %d, want 0",
				generator.calls,
			)
		}
	})

	t.Run("API-key generator", func(t *testing.T) {
		service := newTestService(
			t,
			&fixedGenerator{
				err: errors.New("raw secret in generator error"),
			},
			bytes.Repeat([]byte{0x06}, 12),
		)
		_, err := service.CreateProvisioningMaterial(
			context.Background(),
			request,
		)
		if !errors.Is(err, ErrCryptoUnavailable) {
			t.Fatalf("error = %v", err)
		}
		if strings.Contains(err.Error(), "raw secret") {
			t.Fatalf("generator error leaked: %v", err)
		}
	})

	t.Run("nonce source", func(t *testing.T) {
		service := newTestService(
			t,
			&fixedGenerator{raw: rawAPIKey},
			[]byte{0x01},
		)
		_, err := service.CreateProvisioningMaterial(
			context.Background(),
			request,
		)
		if !errors.Is(err, ErrCryptoUnavailable) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestConstructorAndFormattingDoNotExposeKeyMaterial(t *testing.T) {
	generator := &fixedGenerator{raw: testRawAPIKey(0x71)}
	encryptionKey := bytes.Repeat([]byte{0x72}, 32)
	service, err := New(
		encryptionKey,
		"v1",
		generator,
		hmacHasher{secret: []byte("hash-secret")},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, formatted := range []string{
		fmt.Sprintf("%v", service),
		fmt.Sprintf("%+v", service),
		fmt.Sprintf("%#v", service),
		fmt.Sprintf("%v", *service),
		fmt.Sprintf("%#v", *service),
	} {
		if strings.Contains(
			formatted,
			hex.EncodeToString(encryptionKey),
		) ||
			strings.Contains(formatted, generator.raw) ||
			strings.Contains(formatted, "hash-secret") {
			t.Fatalf("secret leaked through formatting: %s", formatted)
		}
	}
}

func TestConstructorRejectsInvalidConfiguration(t *testing.T) {
	validGenerator := &fixedGenerator{raw: testRawAPIKey(0x21)}
	validHasher := hmacHasher{secret: []byte("hash-secret")}

	tests := []struct {
		name      string
		key       []byte
		version   string
		generator ports.APIKeyGenerator
		hasher    APIKeyHasher
	}{
		{
			name:      "short encryption key",
			key:       bytes.Repeat([]byte{1}, 31),
			version:   "v1",
			generator: validGenerator,
			hasher:    validHasher,
		},
		{
			name:      "blank key version",
			key:       bytes.Repeat([]byte{1}, 32),
			generator: validGenerator,
			hasher:    validHasher,
		},
		{
			name:    "nil generator",
			key:     bytes.Repeat([]byte{1}, 32),
			version: "v1",
			hasher:  validHasher,
		},
		{
			name:      "nil hasher",
			key:       bytes.Repeat([]byte{1}, 32),
			version:   "v1",
			generator: validGenerator,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := New(
				test.key,
				test.version,
				test.generator,
				test.hasher,
			)
			if service != nil {
				t.Fatal("service must be nil")
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf(
					"error = %v, want ErrInvalidConfig",
					err,
				)
			}
		})
	}
}
