package envresolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestResolveExistingValueExactName(t *testing.T) {
	var received string
	resolver, err := NewWithLookup(func(name string) (string, bool) {
		received = name
		if name == " Mixed_Name " {
			return "secret-value", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := resolver.Resolve(context.Background(), " Mixed_Name ")
	if err != nil || secret != "secret-value" {
		t.Fatalf("Resolve = %q, %v", secret, err)
	}
	if received != " Mixed_Name " {
		t.Fatalf("lookup received %q", received)
	}
}

func TestResolveErrors(t *testing.T) {
	tests := []struct {
		name        string
		lookupValue string
		lookupOK    bool
		want        error
	}{
		{name: "MISSING", want: ErrSecretNotAvailable},
		{name: "EMPTY", lookupOK: true, lookupValue: "", want: ErrSecretNotAvailable},
		{name: " \t\n", want: ErrInvalidSecretName},
	}
	for _, tt := range tests {
		resolver, err := NewWithLookup(func(string) (string, bool) { return tt.lookupValue, tt.lookupOK })
		if err != nil {
			t.Fatal(err)
		}
		_, err = resolver.Resolve(context.Background(), tt.name)
		if !errors.Is(err, tt.want) {
			t.Fatalf("Resolve(%q) error = %v want %v", tt.name, err, tt.want)
		}
	}
}

func TestResolveCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	resolver, err := NewWithLookup(func(string) (string, bool) { called = true; return "secret", true })
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(ctx, "SECRET")
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("err=%v called=%t", err, called)
	}
}

func TestExistsReportsOnlyNonEmptyConfiguredValue(t *testing.T) {
	tests := []struct {
		name        string
		lookupValue string
		lookupOK    bool
		want        bool
	}{
		{
			name:        "present",
			lookupValue: "secret-value",
			lookupOK:    true,
			want:        true,
		},
		{
			name:     "missing",
			lookupOK: false,
			want:     false,
		},
		{
			name:        "empty",
			lookupValue: "",
			lookupOK:    true,
			want:        false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver, err := NewWithLookup(
				func(string) (string, bool) {
					return test.lookupValue, test.lookupOK
				},
			)
			if err != nil {
				t.Fatal(err)
			}

			got, err := resolver.Exists(
				context.Background(),
				"RESELLER_API_KEY",
			)
			if err != nil {
				t.Fatalf("Exists: %v", err)
			}
			if got != test.want {
				t.Fatalf("Exists = %t, want %t", got, test.want)
			}
		})
	}
}

func TestExistsRejectsInvalidNameAndCancelledContext(t *testing.T) {
	resolver, err := NewWithLookup(
		func(string) (string, bool) {
			return "secret", true
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := resolver.Exists(
		context.Background(),
		" \t\n",
	); !errors.Is(err, ErrInvalidSecretName) {
		t.Fatalf("invalid name error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.Exists(
		ctx,
		"RESELLER_API_KEY",
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
}

func TestSecretAbsentFromErrorsAndFormatting(t *testing.T) {
	resolver, err := NewWithLookup(func(string) (string, bool) { return "super-secret", false })
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(context.Background(), "SECRET")
	if err == nil || strings.Contains(fmt.Sprint(err), "super-secret") || strings.Contains(fmt.Sprintf("%#v", resolver), "super-secret") {
		t.Fatalf("secret leaked: err=%v resolver=%#v", err, resolver)
	}
}
