package auth

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestProvisioningAuthenticator(t *testing.T) {
	const token = "provisioning-service-token"

	authenticator, err := NewProvisioningAuthenticator(token)
	if err != nil {
		t.Fatalf("NewProvisioningAuthenticator: %v", err)
	}
	if err := authenticator.Authenticate(token); err != nil {
		t.Fatalf("Authenticate valid token: %v", err)
	}

	for _, invalid := range []string{
		"",
		"other-token",
		"Bearer " + token,
	} {
		if !errors.Is(
			authenticator.Authenticate(invalid),
			ErrProvisioningAccessDenied,
		) {
			t.Fatalf("token %q was accepted", invalid)
		}
	}
}

func TestProvisioningAuthenticatorRejectsInvalidConfig(
	t *testing.T,
) {
	for _, token := range []string{
		"",
		" token",
		"token ",
		" \t\n",
	} {
		authenticator, err :=
			NewProvisioningAuthenticator(token)
		if authenticator != nil ||
			!errors.Is(
				err,
				ErrProvisioningServiceTokenRequired,
			) {
			t.Fatalf(
				"token %q: authenticator=%v error=%v",
				token,
				authenticator,
				err,
			)
		}
	}
}

func TestProvisioningAuthenticatorDoesNotExposeToken(
	t *testing.T,
) {
	const token = "sensitive-provisioning-token"
	authenticator, err :=
		NewProvisioningAuthenticator(token)
	if err != nil {
		t.Fatal(err)
	}

	for _, formatted := range []string{
		fmt.Sprintf("%v", authenticator),
		fmt.Sprintf("%+v", authenticator),
		fmt.Sprintf("%#v", authenticator),
		fmt.Sprintf("%v", *authenticator),
		fmt.Sprintf("%#v", *authenticator),
	} {
		if strings.Contains(formatted, token) {
			t.Fatalf(
				"token leaked through formatting: %s",
				formatted,
			)
		}
	}
}
