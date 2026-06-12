package jwtidentity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var ErrInvalidConfig = errors.New("invalid billing jwt identity config")
var ErrInvalidSubject = errors.New("invalid billing subject")

type Config struct {
	SigningKey []byte
	TTL        time.Duration
	Clock      ports.Clock
}

type Service struct {
	signingKey []byte
	ttl        time.Duration
	clock      ports.Clock
}

func New(cfg Config) (*Service, error) {
	if len(cfg.SigningKey) == 0 {
		return nil, fmt.Errorf("%w: signing key is required", ErrInvalidConfig)
	}
	if cfg.TTL <= 0 {
		return nil, fmt.Errorf("%w: ttl is required", ErrInvalidConfig)
	}
	if cfg.Clock == nil {
		return nil, fmt.Errorf("%w: clock is required", ErrInvalidConfig)
	}
	key := make([]byte, len(cfg.SigningKey))
	copy(key, cfg.SigningKey)
	return &Service{signingKey: key, ttl: cfg.TTL, clock: cfg.Clock}, nil
}

func (s *Service) String() string { return "billing jwt identity service" }

func (s *Service) GoString() string { return "billing jwt identity service" }

func (s *Service) TokenForSubject(ctx context.Context, billingSubjectUserID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(billingSubjectUserID) == "" {
		return "", ErrInvalidSubject
	}
	now := s.clock.Now().UTC()
	header := jwtHeader{Alg: "HS256", Typ: "JWT"}
	claims := jwtClaims{
		UserID:    billingSubjectUserID,
		Issuer:    "tokenio-gateway",
		Audience:  "billing-service",
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(s.ttl).Unix(),
	}
	headerPart, err := encodeJSON(header)
	if err != nil {
		return "", fmt.Errorf("encode billing jwt header: %w", err)
	}
	claimsPart, err := encodeJSON(claims)
	if err != nil {
		return "", fmt.Errorf("encode billing jwt claims: %w", err)
	}
	signingInput := headerPart + "." + claimsPart
	mac := hmac.New(sha256.New, s.signingKey)
	_, _ = mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + signature, nil
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type jwtClaims struct {
	UserID    string `json:"user_id"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func encodeJSON(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

var _ ports.BillingIdentityService = (*Service)(nil)
