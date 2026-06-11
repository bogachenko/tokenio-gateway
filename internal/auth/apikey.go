package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

type APIKeyPrincipal struct {
	UserID     string
	APIKeyID   string
	BillingJWT string
}

type APIKeyAuthenticator interface {
	ValidateAPIKey(rawAPIKey string) (*APIKeyPrincipal, error)
}

func ExtractBearerAPIKey(header string) (string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", fmt.Errorf("authorization header is required")
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", fmt.Errorf("authorization header format must be Bearer {api_key}")
	}

	key := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if key == "" {
		return "", fmt.Errorf("bearer api key is empty")
	}
	if !strings.HasPrefix(key, "sk_") {
		return "", fmt.Errorf("api key must start with sk_")
	}

	return key, nil
}

func HashAPIKey(rawAPIKey string) string {
	sum := sha256.Sum256([]byte(rawAPIKey))
	return hex.EncodeToString(sum[:])
}

func ConstantTimeEqualHash(expectedHash string, rawAPIKey string) bool {
	actual := HashAPIKey(rawAPIKey)
	return subtle.ConstantTimeCompare([]byte(expectedHash), []byte(actual)) == 1
}
