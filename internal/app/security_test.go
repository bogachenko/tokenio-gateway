package app

import (
	"context"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/config"
)

func TestNewSecurityGraphConstructsRequiredCapabilities(t *testing.T) {
	t.Setenv("TOKENIO_TEST_RESELLER_API_KEY", "secret-value")

	graph, err := NewSecurityGraph(config.Config{
		AdminToken:               "admin-token",
		APIKeyHashSecret:         "api-key-hash-secret",
		ProvisioningServiceToken: "provisioning-service-token",
	})
	if err != nil {
		t.Fatalf("NewSecurityGraph: %v", err)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("security graph: %v", err)
	}

	rawAPIKey, err := graph.APIKeyGenerator.GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if !strings.HasPrefix(rawAPIKey, "sk_live_") {
		t.Fatalf("API key = %q, want sk_live_ prefix", rawAPIKey)
	}
	if graph.APIKeyHasher.Hash(rawAPIKey) == "" {
		t.Fatal("API-key hash is empty")
	}
	if !graph.ProvisioningEnabled || graph.ProvisioningAuthenticator == nil {
		t.Fatal("provisioning authenticator is not enabled")
	}
	if err := graph.ProvisioningAuthenticator.Authenticate("provisioning-service-token"); err != nil {
		t.Fatalf("Authenticate provisioning token: %v", err)
	}

	secret, err := graph.Secrets.Resolve(context.Background(), "TOKENIO_TEST_RESELLER_API_KEY")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if secret != "secret-value" {
		t.Fatalf("secret = %q", secret)
	}
	present, err := graph.SecretPresence.Exists(context.Background(), "TOKENIO_TEST_RESELLER_API_KEY")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !present {
		t.Fatal("configured secret reported absent")
	}
}

func TestNewSecurityGraphAllowsProvisioningDisabled(t *testing.T) {
	graph, err := NewSecurityGraph(config.Config{
		AdminToken:       "admin-token",
		APIKeyHashSecret: "api-key-hash-secret",
	})
	if err != nil {
		t.Fatalf("NewSecurityGraph: %v", err)
	}
	if graph.ProvisioningEnabled || graph.ProvisioningAuthenticator != nil {
		t.Fatal("disabled provisioning authenticator was created")
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("security graph: %v", err)
	}
}

func TestNewSecurityGraphFailsFastOnInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
	}{
		{
			name: "API-key hash secret",
			cfg:  config.Config{AdminToken: "admin-token"},
		},
		{
			name: "admin token",
			cfg:  config.Config{APIKeyHashSecret: "api-key-hash-secret"},
		},
		{
			name: "provisioning token whitespace",
			cfg: config.Config{
				AdminToken:               "admin-token",
				APIKeyHashSecret:         "api-key-hash-secret",
				ProvisioningServiceToken: " provisioning-token",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph, err := NewSecurityGraph(test.cfg)
			if err == nil {
				t.Fatal("expected startup construction error")
			}
			if err := graph.Validate(); err == nil {
				t.Fatal("invalid graph unexpectedly validated")
			}
		})
	}
}

func TestSecurityGraphValidateRejectsInconsistentState(t *testing.T) {
	graph, err := NewSecurityGraph(config.Config{
		AdminToken:       "admin-token",
		APIKeyHashSecret: "api-key-hash-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	graph.ProvisioningEnabled = true
	if err := graph.Validate(); err == nil {
		t.Fatal("expected missing provisioning authenticator error")
	}

	var zero SecurityGraph
	if err := zero.Validate(); err == nil {
		t.Fatal("expected incomplete graph validation error")
	}
}
