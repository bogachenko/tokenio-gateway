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

func TestTokenClaimsCarryBillingIssuerAudienceAndTTL(t *testing.T) {
	const (
		issuer   = "tokenio-gateway"
		audience = "billing-service"
		subject  = "billing-user-42"
	)
	issuedAt := time.Unix(1700000000, 0).UTC()
	ttl := 37 * time.Minute

	svc, err := New(Config{
		SigningKey: []byte("billing-jwt-signing-key"),
		TTL:        ttl,
		Clock:      fixedClock{t: issuedAt},
	})
	if err != nil {
		t.Fatal(err)
	}

	token, err := svc.TokenForSubject(t.Context(), subject)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("parts=%d", len(parts))
	}

	var claims struct {
		UserID    string `json:"user_id"`
		Issuer    string `json:"iss"`
		Audience  string `json:"aud"`
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
	}
	decodePart(t, parts[1], &claims)

	if claims.UserID != subject {
		t.Fatalf("user_id = %q, want %q", claims.UserID, subject)
	}
	if claims.Issuer != issuer {
		t.Fatalf("iss = %q, want %q", claims.Issuer, issuer)
	}
	if claims.Audience != audience {
		t.Fatalf("aud = %q, want %q", claims.Audience, audience)
	}
	if claims.IssuedAt != issuedAt.Unix() {
		t.Fatalf("iat = %d, want %d", claims.IssuedAt, issuedAt.Unix())
	}
	if got, want := claims.ExpiresAt-claims.IssuedAt, int64(ttl.Seconds()); got != want {
		t.Fatalf("exp-iat = %d, want %d", got, want)
	}
}

func TestBillingJWTConfigRejectsMissingSigningKeyAndTTL(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "missing signing key",
			cfg: Config{
				TTL:   time.Minute,
				Clock: fixedClock{},
			},
			want: "signing key is required",
		},
		{
			name: "missing ttl",
			cfg: Config{
				SigningKey: []byte("billing-jwt-signing-key"),
				Clock:      fixedClock{},
			},
			want: "ttl is required",
		},
		{
			name: "missing clock",
			cfg: Config{
				SigningKey: []byte("billing-jwt-signing-key"),
				TTL:        time.Minute,
			},
			want: "clock is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.cfg)
			if err == nil {
				t.Fatal("expected New() error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
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
