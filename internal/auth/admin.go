package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrAdminTokenRequired         = errors.New("admin token is required")
	ErrAdminAuthorizationRequired = errors.New("admin authorization is required")
	ErrAdminAccessDenied          = errors.New("admin access denied")
)

const AdminSubject = "admin_token"

type AdminAuthenticator struct{ tokenDigest [sha256.Size]byte }

func NewAdminAuthenticator(token string) (*AdminAuthenticator, error) {
	if token == "" {
		return nil, ErrAdminTokenRequired
	}
	return &AdminAuthenticator{tokenDigest: sha256.Sum256([]byte(token))}, nil
}

func (a *AdminAuthenticator) Authenticate(header string) (string, error) {
	if header == "" {
		return "", ErrAdminAuthorizationRequired
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) || len(header) == len(prefix) {
		return "", ErrAdminAccessDenied
	}
	provided := sha256.Sum256([]byte(header[len(prefix):]))
	if subtle.ConstantTimeCompare(provided[:], a.tokenDigest[:]) != 1 {
		return "", ErrAdminAccessDenied
	}
	return AdminSubject, nil
}

func (*AdminAuthenticator) String() string   { return "AdminAuthenticator{token:<redacted>}" }
func (*AdminAuthenticator) GoString() string { return "AdminAuthenticator{token:<redacted>}" }

var _ fmt.Stringer = (*AdminAuthenticator)(nil)
var _ fmt.GoStringer = (*AdminAuthenticator)(nil)
