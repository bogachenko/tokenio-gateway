package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrProvisioningServiceTokenRequired = errors.New(
		"provisioning service token is required",
	)
	ErrProvisioningAccessDenied = errors.New(
		"provisioning access denied",
	)
)

type ProvisioningAuthenticator struct {
	tokenDigest [sha256.Size]byte
}

func NewProvisioningAuthenticator(
	token string,
) (*ProvisioningAuthenticator, error) {
	if token == "" || token != strings.TrimSpace(token) {
		return nil, ErrProvisioningServiceTokenRequired
	}
	return &ProvisioningAuthenticator{
		tokenDigest: sha256.Sum256([]byte(token)),
	}, nil
}

func (a *ProvisioningAuthenticator) Authenticate(
	providedToken string,
) error {
	if a == nil || providedToken == "" {
		return ErrProvisioningAccessDenied
	}
	providedDigest := sha256.Sum256([]byte(providedToken))
	if subtle.ConstantTimeCompare(
		providedDigest[:],
		a.tokenDigest[:],
	) != 1 {
		return ErrProvisioningAccessDenied
	}
	return nil
}

func (*ProvisioningAuthenticator) String() string {
	return "ProvisioningAuthenticator{token:<redacted>}"
}

func (*ProvisioningAuthenticator) GoString() string {
	return "ProvisioningAuthenticator{token:<redacted>}"
}

var _ fmt.Stringer = (*ProvisioningAuthenticator)(nil)
var _ fmt.GoStringer = (*ProvisioningAuthenticator)(nil)
