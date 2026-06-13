package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestStage10MajorAdminAuthenticatorBoundaryAndRedaction(t *testing.T) {
	const token = "admin-secret-value"
	authenticator, err := NewAdminAuthenticator(token)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := authenticator.Authenticate(""); !errors.Is(err, ErrAdminAuthorizationRequired) {
		t.Fatalf("missing token error = %v", err)
	}
	if _, err := authenticator.Authenticate("Bearer sk_live_user_key"); !errors.Is(err, ErrAdminAccessDenied) {
		t.Fatalf("user key error = %v", err)
	}
	subject, err := authenticator.Authenticate("Bearer " + token)
	if err != nil {
		t.Fatal(err)
	}
	if subject != AdminSubject {
		t.Fatalf("subject = %q", subject)
	}

	for _, rendered := range []string{fmt.Sprintf("%v", authenticator), fmt.Sprintf("%#v", authenticator)} {
		if strings.Contains(rendered, token) {
			t.Fatalf("admin token leaked by formatting: %q", rendered)
		}
	}
}

func TestStage10MajorAPIKeyHasherUsesHMACSHA256(t *testing.T) {
	const secret = "hash-secret"
	const raw = "sk_live_example"
	hasher, err := NewAPIKeyHasher(secret)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(raw))
	want := hex.EncodeToString(mac.Sum(nil))
	if got := hasher.Hash(raw); got != want {
		t.Fatalf("hash = %q, want %q", got, want)
	}
	if !hasher.Equal(want, raw) || hasher.Equal(want, raw+"x") {
		t.Fatal("constant-time HMAC comparison contract failed")
	}
}

func TestStage10MajorSecureAPIKeyGeneratorUsesThirtyTwoRandomBytes(t *testing.T) {
	source := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 32))
	generator, err := NewSecureAPIKeyGeneratorWithReader(source)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := generator.GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "sk_live_") || len(raw) <= len("sk_live_") {
		t.Fatalf("raw key format = %q", raw)
	}
}
