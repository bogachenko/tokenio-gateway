package app

import (
	"fmt"

	adminapp "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	ledgerapp "github.com/bogachenko/tokenio-gateway/internal/application/ledger"
	provisioningapp "github.com/bogachenko/tokenio-gateway/internal/application/provisioning"
	"github.com/bogachenko/tokenio-gateway/internal/config"
)

type ApplicationGraph struct {
	PublicAuthentication *authenticateapp.UseCase
	ProvisioningEnabled  bool
	Provisioning         *provisioningapp.Service
	Ledger               *ledgerapp.Service
	AutoCharge           *billingapp.AutoChargeService
	FailedBatchRetry     *billingapp.FailedBatchRetryService
	Admin                *adminapp.Service
}

func NewApplicationGraph(
	cfg config.Config,
	primitives RuntimePrimitives,
	security SecurityGraph,
	provisioningInfrastructure ProvisioningInfrastructureGraph,
	billingInfrastructure BillingInfrastructureGraph,
	repositories RepositoryGraph,
) (ApplicationGraph, error) {
	if err := primitives.Validate(); err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"validate runtime primitives: %w",
			err,
		)
	}
	if err := security.Validate(); err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"validate security graph: %w",
			err,
		)
	}
	if err := provisioningInfrastructure.Validate(); err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"validate provisioning infrastructure graph: %w",
			err,
		)
	}
	if err := billingInfrastructure.Validate(); err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"validate billing infrastructure graph: %w",
			err,
		)
	}
	if err := repositories.Validate(); err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"validate repository graph: %w",
			err,
		)
	}

	publicAuthentication, err := authenticateapp.NewUseCase(
		security.APIKeyHasher,
		repositories.APIKeys,
		repositories.Users,
		primitives.Clock,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct public authentication use case: %w",
			err,
		)
	}

	var provisioningService *provisioningapp.Service
	if provisioningInfrastructure.Enabled {
		provisioningService, err = provisioningapp.NewService(
			provisioningapp.Dependencies{
				Store:             repositories.APIKeyProvisioning,
				MaterialFactory:   provisioningInfrastructure.MaterialFactory,
				MaterialDecryptor: provisioningInfrastructure.MaterialDecryptor,
				Clock:             primitives.Clock,
				TTL:               cfg.APIKeyProvisioningTTL,
			},
		)
		if err != nil {
			return ApplicationGraph{}, fmt.Errorf(
				"construct provisioning service: %w",
				err,
			)
		}
	}

	ledgerService, err := ledgerapp.NewService(
		repositories.UsageLedger,
		primitives.Clock,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct ledger service: %w",
			err,
		)
	}

	autoCharge, err := billingapp.NewAutoChargeService(
		billingInfrastructure.Identity,
		billingInfrastructure.Balance,
		billingInfrastructure.Charge,
		repositories.UsageLedger,
		primitives.Clock,
		billingapp.AutoChargeConfig{
			ThresholdCents:     cfg.AutoChargeThresholdCents,
			MinimumChargeCents: cfg.MinChargeAmountCents,
		},
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct auto-charge service: %w",
			err,
		)
	}

	failedBatchRetry, err := billingapp.NewFailedBatchRetryService(
		billingInfrastructure.Charge,
		repositories.AdminUsage,
		primitives.Clock,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct failed billing batch retry service: %w",
			err,
		)
	}

	adminService, err := adminapp.NewService(adminapp.Dependencies{
		Users:         repositories.AdminUsers,
		APIKeys:       repositories.AdminAPIKeys,
		Provisionings: repositories.AdminProvisioning,
		Resellers:     repositories.AdminResellers,
		Routes:        repositories.AdminRoutes,
		Prices:        repositories.AdminRoutePrices,
		Ledger:        repositories.AdminUsage,
		Audit:         repositories.AdminAudit,
		Secrets:       security.SecretPresence,
		KeyGenerator:  security.APIKeyGenerator,
		Hasher:        security.APIKeyHasher,
		Clock:         primitives.Clock,
		BatchRetrier:  failedBatchRetry,
	})
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct admin service: %w",
			err,
		)
	}

	graph := ApplicationGraph{
		PublicAuthentication: publicAuthentication,
		ProvisioningEnabled:  provisioningInfrastructure.Enabled,
		Provisioning:         provisioningService,
		Ledger:               ledgerService,
		AutoCharge:           autoCharge,
		FailedBatchRetry:     failedBatchRetry,
		Admin:                adminService,
	}
	if err := graph.Validate(); err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"validate application graph: %w",
			err,
		)
	}
	return graph, nil
}

func (g ApplicationGraph) Validate() error {
	switch {
	case g.PublicAuthentication == nil:
		return fmt.Errorf("public authentication use case is nil")
	case g.ProvisioningEnabled && g.Provisioning == nil:
		return fmt.Errorf("enabled provisioning service is nil")
	case !g.ProvisioningEnabled && g.Provisioning != nil:
		return fmt.Errorf("disabled provisioning service is non-nil")
	case g.Ledger == nil:
		return fmt.Errorf("ledger service is nil")
	case g.AutoCharge == nil:
		return fmt.Errorf("auto-charge service is nil")
	case g.FailedBatchRetry == nil:
		return fmt.Errorf("failed billing batch retry service is nil")
	case g.Admin == nil:
		return fmt.Errorf("admin service is nil")
	default:
		return nil
	}
}
