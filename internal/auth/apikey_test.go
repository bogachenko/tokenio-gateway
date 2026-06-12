package auth

import (
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestExtractBearerAPIKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		header    string
		wantKey   string
		wantError error
	}{
		{name: "missing Authorization header", header: "", wantError: ErrAuthorizationHeaderRequired},
		{name: "whitespace-only header", header: " \t\n ", wantError: ErrAuthorizationHeaderRequired},
		{name: "wrong scheme", header: "Basic sk_test_key", wantError: ErrAuthorizationHeaderScheme},
		{name: "empty Bearer value", header: "Bearer   ", wantError: ErrBearerAPIKeyEmpty},
		{name: "value without sk_ prefix", header: "Bearer pk_test_key", wantError: ErrBearerAPIKeyPrefix},
		{name: "valid sk_ key", header: "Bearer sk_synthetic", wantKey: "sk_synthetic"},
		{name: "valid sk_live_ key", header: "Bearer sk_live_synthetic", wantKey: "sk_live_synthetic"},
		{name: "valid sk_test_ key", header: "Bearer sk_test_synthetic", wantKey: "sk_test_synthetic"},
		{name: "surrounding header whitespace", header: " \tBearer sk_test_surrounded \n", wantKey: "sk_test_surrounded"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotKey, err := ExtractBearerAPIKey(tt.header)
			if tt.wantError != nil {
				if err == nil {
					t.Fatalf("expected error %q", tt.wantError)
				}
				if err != tt.wantError {
					t.Fatalf("expected error %q, got %q", tt.wantError, err)
				}
				if gotKey != "" {
					t.Fatalf("expected empty key for error case")
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %q", err)
			}
			if gotKey != tt.wantKey {
				t.Fatalf("expected key %q, got %q", tt.wantKey, gotKey)
			}
		})
	}
}

func TestAPIKeyHasherRejectsEmptySecret(t *testing.T) {
	t.Parallel()

	for _, secret := range []string{"", " \t\n "} {
		if _, err := NewAPIKeyHasher(secret); err != ErrAPIKeyHashSecretRequired {
			t.Fatalf("expected ErrAPIKeyHashSecretRequired for empty secret, got %v", err)
		}
	}
}

func TestAPIKeyHasherHashProperties(t *testing.T) {
	t.Parallel()

	hasher, err := NewAPIKeyHasher("test-secret")
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}

	first := hasher.Hash("sk_test_synthetic")
	second := hasher.Hash("sk_test_synthetic")
	if first != second {
		t.Fatalf("expected deterministic hash")
	}
	if len(first) != 64 {
		t.Fatalf("expected 64 hex characters, got %d", len(first))
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("expected valid hex hash: %v", err)
	}
	if first != strings.ToLower(first) {
		t.Fatalf("expected lowercase hex hash")
	}
}

func TestAPIKeyHasherKnownVector(t *testing.T) {
	t.Parallel()

	hasher, err := NewAPIKeyHasher("test-secret")
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}

	const expected = "4c86012896318a65f8511c87449fd31ab260946b408deb617b75afe61650b550"
	if got := hasher.Hash("sk_test_vector"); got != expected {
		t.Fatalf("expected known HMAC-SHA256 vector %q, got %q", expected, got)
	}
}

func TestAPIKeyHasherDifferentInputs(t *testing.T) {
	t.Parallel()

	firstSecret, err := NewAPIKeyHasher("test-secret-one")
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}
	secondSecret, err := NewAPIKeyHasher("test-secret-two")
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}

	if firstSecret.Hash("sk_test_synthetic") == secondSecret.Hash("sk_test_synthetic") {
		t.Fatalf("expected different secrets to produce different hashes")
	}
	if firstSecret.Hash("sk_test_synthetic_one") == firstSecret.Hash("sk_test_synthetic_two") {
		t.Fatalf("expected different API keys to produce different hashes")
	}
}

func TestAPIKeyHasherEqual(t *testing.T) {
	t.Parallel()

	hasher, err := NewAPIKeyHasher("test-secret")
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}

	expectedHash := hasher.Hash("sk_test_synthetic")
	if !hasher.Equal(expectedHash, "sk_test_synthetic") {
		t.Fatalf("expected matching API key to compare equal")
	}
	if hasher.Equal(expectedHash, "sk_test_other") {
		t.Fatalf("expected different API key to compare unequal")
	}
	if hasher.Equal("not-hex", "sk_test_synthetic") {
		t.Fatalf("expected invalid hex hash to compare unequal")
	}
	if hasher.Equal(expectedHash[:32], "sk_test_synthetic") {
		t.Fatalf("expected truncated hash to compare unequal")
	}
	if hasher.Equal(hasher.Hash(""), "") {
		t.Fatalf("expected empty raw API key to compare unequal")
	}
}

func TestAPIKeyPrincipalContract(t *testing.T) {
	t.Parallel()

	principalType := reflect.TypeOf(APIKeyPrincipal{})
	for _, field := range []string{"UserID", "APIKeyID", "BillingSubjectUserID"} {
		if _, ok := principalType.FieldByName(field); !ok {
			t.Fatalf("APIKeyPrincipal must contain %s", field)
		}
	}
	if _, ok := principalType.FieldByName("Billing" + "JWT"); ok {
		t.Fatalf("APIKeyPrincipal must not contain billing token field")
	}
}
