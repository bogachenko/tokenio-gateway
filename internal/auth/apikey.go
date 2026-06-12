package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

var (
	ErrAuthorizationHeaderRequired = errors.New("authorization header is required")
	ErrAuthorizationHeaderScheme   = errors.New("authorization header format must be Bearer {api_key}")
	ErrBearerAPIKeyEmpty           = errors.New("bearer api key is empty")
	ErrBearerAPIKeyPrefix          = errors.New("api key must start with sk_")
	ErrAPIKeyHashSecretRequired    = errors.New("api key hash secret is required")
)

type APIKeyPrincipal struct {
	UserID               string
	APIKeyID             string
	BillingSubjectUserID string
}

type APIKeyAuthenticator interface {
	ValidateAPIKey(rawAPIKey string) (*APIKeyPrincipal, error)
}

type APIKeyHasher struct {
	secret []byte
}

func NewAPIKeyHasher(secret string) (*APIKeyHasher, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, ErrAPIKeyHashSecretRequired
	}

	secretBytes := []byte(secret)
	secretCopy := make([]byte, len(secretBytes))
	copy(secretCopy, secretBytes)

	return &APIKeyHasher{secret: secretCopy}, nil
}

func (h *APIKeyHasher) Hash(rawAPIKey string) string {
	mac := hmac.New(sha256.New, h.secret)
	_, _ = mac.Write([]byte(rawAPIKey))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *APIKeyHasher) Equal(expectedHash string, rawAPIKey string) bool {
	if rawAPIKey == "" {
		return false
	}

	expected, err := hex.DecodeString(expectedHash)
	if err != nil || len(expected) != sha256.Size {
		return false
	}

	actualHash := h.Hash(rawAPIKey)
	actual, err := hex.DecodeString(actualHash)
	if err != nil || len(actual) != sha256.Size {
		return false
	}

	return hmac.Equal(expected, actual)
}

func ExtractBearerAPIKey(header string) (string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", ErrAuthorizationHeaderRequired
	}

	const scheme = "Bearer"
	if header == scheme {
		return "", ErrBearerAPIKeyEmpty
	}

	const prefix = scheme + " "
	if !strings.HasPrefix(header, prefix) {
		return "", ErrAuthorizationHeaderScheme
	}

	key := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if key == "" {
		return "", ErrBearerAPIKeyEmpty
	}
	if !strings.HasPrefix(key, "sk_") {
		return "", ErrBearerAPIKeyPrefix
	}

	return key, nil
}
