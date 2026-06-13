package app

import (
	"bytes"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/config"
)

func provisioningGraphSecurity(t *testing.T) SecurityGraph {
	t.Helper()

	security, err := NewSecurityGraph(config.Config{
		AdminToken:       "admin-token",
		APIKeyHashSecret: "api-key-hash-secret",
	})
	if err != nil {
		t.Fatalf("NewSecurityGraph: %v", err)
	}
	return security
}

func TestNewProvisioningInfrastructureGraphDisabledWithoutKey(
	t *testing.T,
) {
	graph, err := NewProvisioningInfrastructureGraph(
		config.Config{},
		provisioningGraphSecurity(t),
	)
	if err != nil {
		t.Fatalf(
			"NewProvisioningInfrastructureGraph: %v",
			err,
		)
	}
	if graph.Enabled {
		t.Fatal("provisioning graph unexpectedly enabled")
	}
	if graph.MaterialFactory != nil ||
		graph.MaterialDecryptor != nil {
		t.Fatal("disabled graph contains crypto capabilities")
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("disabled provisioning graph: %v", err)
	}
}

func TestNewProvisioningInfrastructureGraphConstructsCapabilities(
	t *testing.T,
) {
	graph, err := NewProvisioningInfrastructureGraph(
		config.Config{
			APIKeyProvisioningEncryptionKey: bytes.Repeat(
				[]byte{0x42},
				32,
			),
			APIKeyProvisioningKeyVersion: "v1",
		},
		provisioningGraphSecurity(t),
	)
	if err != nil {
		t.Fatalf(
			"NewProvisioningInfrastructureGraph: %v",
			err,
		)
	}
	if !graph.Enabled {
		t.Fatal("provisioning graph is disabled")
	}
	if graph.MaterialFactory == nil ||
		graph.MaterialDecryptor == nil {
		t.Fatal("provisioning crypto capabilities are missing")
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("provisioning graph: %v", err)
	}
}

func TestNewProvisioningInfrastructureGraphRejectsInvalidKey(
	t *testing.T,
) {
	graph, err := NewProvisioningInfrastructureGraph(
		config.Config{
			APIKeyProvisioningEncryptionKey: bytes.Repeat(
				[]byte{0x42},
				31,
			),
			APIKeyProvisioningKeyVersion: "v1",
		},
		provisioningGraphSecurity(t),
	)
	if err == nil {
		t.Fatal("expected invalid encryption key error")
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf(
			"zero graph after construction failure must remain valid: %v",
			err,
		)
	}
}

func TestProvisioningInfrastructureGraphValidateRejectsMissingCapability(
	t *testing.T,
) {
	graph := ProvisioningInfrastructureGraph{
		Enabled: true,
	}
	if err := graph.Validate(); err == nil {
		t.Fatal("expected incomplete enabled graph error")
	}
}
