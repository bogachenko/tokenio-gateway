package jwtidentity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestTokenForSubjectHS256SignatureAndExactClaims(t *testing.T) {
	svc, err := New(Config{SigningKey: []byte("secret"), TTL: 2 * time.Minute, Clock: fixedClock{t: time.Unix(100, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	token, err := svc.TokenForSubject(t.Context(), "bill-user")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("parts=%d", len(parts))
	}
	var header map[string]any
	var claims map[string]any
	decodePart(t, parts[0], &header)
	decodePart(t, parts[1], &claims)
	wantHeader := map[string]any{"alg": "HS256", "typ": "JWT"}
	wantClaims := map[string]any{"user_id": "bill-user", "iss": "tokenio-gateway", "aud": "billing-service", "iat": float64(100), "exp": float64(220)}
	if fmt.Sprint(header) != fmt.Sprint(wantHeader) {
		t.Fatalf("header=%v", header)
	}
	if fmt.Sprint(claims) != fmt.Sprint(wantClaims) {
		t.Fatalf("claims=%v", claims)
	}
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	wantSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != wantSig {
		t.Fatalf("signature mismatch")
	}
}

func TestSecretSafety(t *testing.T) {
	secret := "super-secret-signing-key"
	_, err := New(Config{SigningKey: []byte(secret), TTL: time.Minute})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(fmt.Sprintf("%+v", err), secret) {
		t.Fatal("secret leaked")
	}
	svc, err := New(Config{SigningKey: []byte(secret), TTL: time.Minute, Clock: fixedClock{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.TokenForSubject(t.Context(), " ")
	if err == nil {
		t.Fatal("expected subject error")
	}
	if strings.Contains(fmt.Sprintf("%+v", err), secret) {
		t.Fatal("secret leaked from token error")
	}
}

func decodePart(t *testing.T, part string, dst any) {
	t.Helper()
	body, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatal(err)
	}
}

func TestServiceFormattingDoesNotLeakSigningKey(t *testing.T) {
	secret := "format-secret"
	svc, err := New(Config{SigningKey: []byte(secret), TTL: time.Minute, Clock: fixedClock{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, formatted := range []string{fmt.Sprintf("%+v", svc), fmt.Sprintf("%#v", svc)} {
		if strings.Contains(formatted, secret) {
			t.Fatalf("secret leaked through formatting: %s", formatted)
		}
	}
}
