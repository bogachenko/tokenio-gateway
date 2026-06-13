package cryptomaterial

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const (
	rawAPIKeyPrefix = "sk_live_"
	displayBytes    = 8
	aadDomain       = "tokenio:api-key-provisioning:aad:v1"
)

var (
	ErrInvalidConfig = errors.New(
		"invalid provisioning crypto config",
	)
	ErrInvalidMaterial = errors.New(
		"invalid provisioning crypto material",
	)
	ErrCryptoUnavailable = errors.New(
		"provisioning crypto unavailable",
	)
	ErrDecryptionFailed = errors.New(
		"provisioning material decryption failed",
	)
)

type APIKeyHasher interface {
	Hash(string) string
}

type Service struct {
	aead        cipher.AEAD
	keyVersion  string
	generator   ports.APIKeyGenerator
	hasher      APIKeyHasher
	nonceReader io.Reader
}

func New(
	encryptionKey []byte,
	keyVersion string,
	generator ports.APIKeyGenerator,
	hasher APIKeyHasher,
) (*Service, error) {
	return newWithNonceReader(
		encryptionKey,
		keyVersion,
		generator,
		hasher,
		rand.Reader,
	)
}

func newWithNonceReader(
	encryptionKey []byte,
	keyVersion string,
	generator ports.APIKeyGenerator,
	hasher APIKeyHasher,
	nonceReader io.Reader,
) (*Service, error) {
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf(
			"%w: encryption key must contain 32 bytes",
			ErrInvalidConfig,
		)
	}
	if keyVersion == "" ||
		keyVersion != strings.TrimSpace(keyVersion) {
		return nil, fmt.Errorf(
			"%w: key version is required",
			ErrInvalidConfig,
		)
	}
	if generator == nil {
		return nil, fmt.Errorf(
			"%w: API-key generator is required",
			ErrInvalidConfig,
		)
	}
	if hasher == nil {
		return nil, fmt.Errorf(
			"%w: API-key hasher is required",
			ErrInvalidConfig,
		)
	}
	if nonceReader == nil {
		return nil, fmt.Errorf(
			"%w: nonce source is required",
			ErrInvalidConfig,
		)
	}

	keyCopy := append([]byte(nil), encryptionKey...)
	block, err := aes.NewCipher(keyCopy)
	clear(keyCopy)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: initialize AES-256",
			ErrInvalidConfig,
		)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: initialize AES-GCM",
			ErrInvalidConfig,
		)
	}

	return &Service{
		aead:        aead,
		keyVersion:  keyVersion,
		generator:   generator,
		hasher:      hasher,
		nonceReader: nonceReader,
	}, nil
}

func (Service) String() string {
	return "provisioning crypto material service"
}

func (Service) GoString() string {
	return "provisioning crypto material service"
}

func (s *Service) CreateProvisioningMaterial(
	ctx context.Context,
	request ports.APIKeyProvisioningMaterialRequest,
) (ports.APIKeyProvisioningMaterial, error) {
	if ctx == nil {
		return ports.APIKeyProvisioningMaterial{},
			fmt.Errorf("%w: context is nil", ErrInvalidMaterial)
	}
	if err := ctx.Err(); err != nil {
		return ports.APIKeyProvisioningMaterial{}, err
	}
	if s == nil ||
		s.aead == nil ||
		s.generator == nil ||
		s.hasher == nil ||
		s.nonceReader == nil {
		return ports.APIKeyProvisioningMaterial{},
			fmt.Errorf("%w: service is incomplete", ErrInvalidConfig)
	}
	if err := validateMaterialRequest(request); err != nil {
		return ports.APIKeyProvisioningMaterial{}, err
	}

	rawAPIKey, err := s.generator.GenerateAPIKey()
	if err != nil {
		return ports.APIKeyProvisioningMaterial{},
			fmt.Errorf("%w: generate API key", ErrCryptoUnavailable)
	}
	if !validRawAPIKey(rawAPIKey) {
		return ports.APIKeyProvisioningMaterial{},
			fmt.Errorf("%w: generated API key", ErrInvalidMaterial)
	}

	keyHash := s.hasher.Hash(rawAPIKey)
	if !validKeyHash(keyHash) {
		return ports.APIKeyProvisioningMaterial{},
			fmt.Errorf("%w: generated key hash", ErrInvalidMaterial)
	}

	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(s.nonceReader, nonce); err != nil {
		return ports.APIKeyProvisioningMaterial{},
			fmt.Errorf("%w: generate nonce", ErrCryptoUnavailable)
	}

	aad, err := authenticatedData(
		request.ProvisioningID,
		request.APIKeyID,
		request.User.ID,
	)
	if err != nil {
		return ports.APIKeyProvisioningMaterial{}, err
	}
	ciphertext := s.aead.Seal(
		nil,
		nonce,
		[]byte(rawAPIKey),
		aad,
	)

	return ports.APIKeyProvisioningMaterial{
		APIKey: domain.APIKeyRecord{
			ID:        request.APIKeyID,
			UserID:    request.User.ID,
			Name:      request.KeyName,
			KeyHash:   keyHash,
			KeyPrefix: displayPrefix(rawAPIKey),
			Enabled:   true,
			CreatedAt: request.CreatedAt,
			UpdatedAt: request.CreatedAt,
		},
		EncryptedRawKey:      ciphertext,
		EncryptionNonce:      nonce,
		EncryptionKeyVersion: s.keyVersion,
	}, nil
}

func (s *Service) DecryptProvisioningMaterial(
	ctx context.Context,
	provisioning domain.APIKeyProvisioning,
	apiKey domain.APIKeyRecord,
) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf(
			"%w: context is nil",
			ErrInvalidMaterial,
		)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s == nil || s.aead == nil || s.hasher == nil {
		return "", fmt.Errorf(
			"%w: service is incomplete",
			ErrInvalidConfig,
		)
	}
	if err := validateEncryptedMaterial(
		s,
		provisioning,
		apiKey,
	); err != nil {
		return "", err
	}

	aad, err := authenticatedData(
		provisioning.ID,
		apiKey.ID,
		apiKey.UserID,
	)
	if err != nil {
		return "", err
	}
	plaintext, err := s.aead.Open(
		nil,
		provisioning.EncryptionNonce,
		provisioning.EncryptedRawKey,
		aad,
	)
	if err != nil {
		return "", ErrDecryptionFailed
	}

	rawAPIKey := string(plaintext)
	clear(plaintext)
	if !validRawAPIKey(rawAPIKey) {
		return "", fmt.Errorf(
			"%w: decrypted API key",
			ErrInvalidMaterial,
		)
	}
	if !hashMatches(s.hasher, apiKey.KeyHash, rawAPIKey) {
		return "", fmt.Errorf(
			"%w: API-key hash mismatch",
			ErrInvalidMaterial,
		)
	}
	if apiKey.KeyPrefix != displayPrefix(rawAPIKey) {
		return "", fmt.Errorf(
			"%w: API-key prefix mismatch",
			ErrInvalidMaterial,
		)
	}
	return rawAPIKey, nil
}

func validateMaterialRequest(
	request ports.APIKeyProvisioningMaterialRequest,
) error {
	if strings.TrimSpace(request.User.ID) == "" ||
		strings.TrimSpace(request.ProvisioningID) == "" ||
		strings.TrimSpace(request.APIKeyID) == "" ||
		strings.TrimSpace(request.KeyName) == "" ||
		!validUTCTime(request.CreatedAt) ||
		!validUTCTime(request.ExpiresAt) ||
		!request.ExpiresAt.After(request.CreatedAt) {
		return fmt.Errorf(
			"%w: material request",
			ErrInvalidMaterial,
		)
	}
	return nil
}

func validateEncryptedMaterial(
	service *Service,
	provisioning domain.APIKeyProvisioning,
	apiKey domain.APIKeyRecord,
) error {
	if provisioning.Status !=
		domain.APIKeyProvisioningStatusPendingDelivery ||
		provisioning.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
		strings.TrimSpace(provisioning.ID) == "" ||
		strings.TrimSpace(provisioning.UserID) == "" ||
		strings.TrimSpace(provisioning.APIKeyID) == "" ||
		provisioning.APIKeyID != apiKey.ID ||
		provisioning.UserID != apiKey.UserID ||
		provisioning.EncryptionKeyVersion != service.keyVersion ||
		len(provisioning.EncryptedRawKey) == 0 ||
		len(provisioning.EncryptionNonce) != service.aead.NonceSize() ||
		provisioning.ExpiresAt == nil ||
		strings.TrimSpace(apiKey.ID) == "" ||
		strings.TrimSpace(apiKey.UserID) == "" ||
		!apiKey.Enabled ||
		apiKey.RevokedAt != nil ||
		!validKeyHash(apiKey.KeyHash) ||
		apiKey.KeyPrefix == "" ||
		!validUTCTime(apiKey.CreatedAt) ||
		!validUTCTime(apiKey.UpdatedAt) {
		return fmt.Errorf(
			"%w: encrypted material",
			ErrInvalidMaterial,
		)
	}
	return nil
}

func authenticatedData(
	provisioningID string,
	apiKeyID string,
	userID string,
) ([]byte, error) {
	values := []string{
		provisioningID,
		apiKeyID,
		userID,
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" ||
			uint64(len(value)) > uint64(^uint32(0)) {
			return nil, fmt.Errorf(
				"%w: authenticated data",
				ErrInvalidMaterial,
			)
		}
	}

	var buffer bytes.Buffer
	buffer.WriteString(aadDomain)
	for _, value := range values {
		var length [4]byte
		binary.BigEndian.PutUint32(
			length[:],
			uint32(len(value)),
		)
		buffer.Write(length[:])
		buffer.WriteString(value)
	}
	return buffer.Bytes(), nil
}

func validRawAPIKey(rawAPIKey string) bool {
	if !strings.HasPrefix(rawAPIKey, rawAPIKeyPrefix) {
		return false
	}
	payload := rawAPIKey[len(rawAPIKeyPrefix):]
	if decoded, err :=
		base64.RawURLEncoding.DecodeString(payload); err == nil {
		return len(decoded) >= 32
	}
	if decoded, err := hex.DecodeString(payload); err == nil {
		return len(decoded) >= 32
	}
	return false
}

func validKeyHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func hashMatches(
	hasher APIKeyHasher,
	expectedHash string,
	rawAPIKey string,
) bool {
	expected, err := hex.DecodeString(expectedHash)
	if err != nil || len(expected) != sha256.Size {
		return false
	}
	actual, err := hex.DecodeString(hasher.Hash(rawAPIKey))
	if err != nil || len(actual) != sha256.Size {
		return false
	}
	return subtle.ConstantTimeCompare(expected, actual) == 1
}

func displayPrefix(rawAPIKey string) string {
	length := len(rawAPIKeyPrefix) + displayBytes
	if length > len(rawAPIKey) {
		length = len(rawAPIKey)
	}
	return rawAPIKey[:length] + "..."
}

func validUTCTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC
}

var _ ports.APIKeyProvisioningMaterialFactory = (*Service)(nil)
var _ ports.APIKeyProvisioningMaterialDecryptor = (*Service)(nil)
