package app

import (
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/provisioning/cryptomaterial"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type ProvisioningInfrastructureGraph struct {
	Enabled bool

	MaterialFactory   ports.APIKeyProvisioningMaterialFactory
	MaterialDecryptor ports.APIKeyProvisioningMaterialDecryptor
}

func NewProvisioningInfrastructureGraph(
	cfg config.Config,
	security SecurityGraph,
) (ProvisioningInfrastructureGraph, error) {
	if err := security.Validate(); err != nil {
		return ProvisioningInfrastructureGraph{}, fmt.Errorf(
			"validate security graph: %w",
			err,
		)
	}

	encryptionEnabled := len(cfg.APIKeyProvisioningEncryptionKey) > 0
	if encryptionEnabled != security.ProvisioningEnabled {
		return ProvisioningInfrastructureGraph{}, fmt.Errorf(
			"provisioning service token and encryption key must be configured together",
		)
	}

	if !encryptionEnabled {
		graph := ProvisioningInfrastructureGraph{}
		if err := graph.Validate(); err != nil {
			return ProvisioningInfrastructureGraph{}, fmt.Errorf(
				"validate disabled provisioning infrastructure graph: %w",
				err,
			)
		}
		return graph, nil
	}

	cryptoService, err := cryptomaterial.New(
		cfg.APIKeyProvisioningEncryptionKey,
		cfg.APIKeyProvisioningKeyVersion,
		security.APIKeyGenerator,
		security.APIKeyHasher,
	)
	if err != nil {
		return ProvisioningInfrastructureGraph{}, fmt.Errorf(
			"construct provisioning crypto material service: %w",
			err,
		)
	}

	graph := ProvisioningInfrastructureGraph{
		Enabled:           true,
		MaterialFactory:   cryptoService,
		MaterialDecryptor: cryptoService,
	}
	if err := graph.Validate(); err != nil {
		return ProvisioningInfrastructureGraph{}, fmt.Errorf(
			"validate provisioning infrastructure graph: %w",
			err,
		)
	}
	return graph, nil
}

func (g ProvisioningInfrastructureGraph) Validate() error {
	if !g.Enabled {
		if g.MaterialFactory != nil || g.MaterialDecryptor != nil {
			return fmt.Errorf("disabled provisioning graph contains crypto capabilities")
		}
		return nil
	}

	switch {
	case g.MaterialFactory == nil:
		return fmt.Errorf("provisioning material factory is nil")
	case g.MaterialDecryptor == nil:
		return fmt.Errorf("provisioning material decryptor is nil")
	default:
		return nil
	}
}
