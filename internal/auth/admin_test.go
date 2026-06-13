package auth

import (
	"fmt"
	"strings"
	"testing"
)

func TestAdminAuthenticatorBoundaryAndRedaction(t *testing.T) {
	authenticator, err := NewAdminAuthenticator("admin-secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authenticator.Authenticate(""); err != ErrAdminAuthorizationRequired {
		t.Fatalf("missing err=%v", err)
	}
	for _, header := range []string{"Basic admin-secret-token", "Bearer wrong", "Bearer sk_live_user_key"} {
		if _, err := authenticator.Authenticate(header); err != ErrAdminAccessDenied {
			t.Fatalf("header=%q err=%v", header, err)
		}
	}
	subject, err := authenticator.Authenticate("Bearer admin-secret-token")
	if err != nil || subject != AdminSubject {
		t.Fatalf("subject=%q err=%v", subject, err)
	}
	formatted := fmt.Sprintf("%s %#v", authenticator, authenticator)
	if strings.Contains(formatted, "admin-secret-token") {
		t.Fatalf("token leaked: %s", formatted)
	}
}

func TestAdminAuthenticatorComparesDigestForDifferentTokenLengths(t *testing.T) {
	authenticator, err := NewAdminAuthenticator("1234567890")
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "1", "123456789", "12345678901", strings.Repeat("x", 4096)} {
		if _, err := authenticator.Authenticate("Bearer " + token); err != ErrAdminAccessDenied {
			t.Fatalf("token length %d err=%v", len(token), err)
		}
	}
}
