package app

import (
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/config"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/secrets/envresolver"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type SecurityGraph struct {
	APIKeyHasher    *auth.APIKeyHasher
	APIKeyGenerator ports.APIKeyGenerator

	AdminAuthenticator *auth.AdminAuthenticator

	ProvisioningEnabled       bool
	ProvisioningAuthenticator *auth.ProvisioningAuthenticator

	Secrets        ports.SecretResolver
	SecretPresence ports.SecretPresenceChecker
}

func NewSecurityGraph(cfg config.Config) (SecurityGraph, error) {
	hasher, err := auth.NewAPIKeyHasher(cfg.APIKeyHashSecret)
	if err != nil {
		return SecurityGraph{}, fmt.Errorf(
			"construct API-key hasher: %w",
			err,
		)
	}

	adminAuthenticator, err := auth.NewAdminAuthenticator(
		cfg.AdminToken,
	)
	if err != nil {
		return SecurityGraph{}, fmt.Errorf(
			"construct admin authenticator: %w",
			err,
		)
	}

	provisioningEnabled := cfg.ProvisioningServiceToken != ""
	var provisioningAuthenticator *auth.ProvisioningAuthenticator
	if provisioningEnabled {
		provisioningAuthenticator, err =
			auth.NewProvisioningAuthenticator(
				cfg.ProvisioningServiceToken,
			)
		if err != nil {
			return SecurityGraph{}, fmt.Errorf(
				"construct provisioning authenticator: %w",
				err,
			)
		}
	}

	secretResolver := envresolver.New()
	graph := SecurityGraph{
		APIKeyHasher:              hasher,
		APIKeyGenerator:           auth.NewSecureAPIKeyGenerator(),
		AdminAuthenticator:        adminAuthenticator,
		ProvisioningEnabled:       provisioningEnabled,
		ProvisioningAuthenticator: provisioningAuthenticator,
		Secrets:                   secretResolver,
		SecretPresence:            secretResolver,
	}
	if err := graph.Validate(); err != nil {
		return SecurityGraph{}, fmt.Errorf(
			"validate security graph: %w",
			err,
		)
	}
	return graph, nil
}

func (g SecurityGraph) Validate() error {
	switch {
	case g.APIKeyHasher == nil:
		return fmt.Errorf("API-key hasher is nil")
	case g.APIKeyGenerator == nil:
		return fmt.Errorf("API-key generator is nil")
	case g.AdminAuthenticator == nil:
		return fmt.Errorf("admin authenticator is nil")
	case g.ProvisioningEnabled &&
		g.ProvisioningAuthenticator == nil:
		return fmt.Errorf(
			"enabled provisioning authenticator is nil",
		)
	case !g.ProvisioningEnabled &&
		g.ProvisioningAuthenticator != nil:
		return fmt.Errorf(
			"disabled provisioning authenticator is non-nil",
		)
	case g.Secrets == nil:
		return fmt.Errorf("secret resolver is nil")
	case g.SecretPresence == nil:
		return fmt.Errorf("secret presence checker is nil")
	default:
		return nil
	}
}
