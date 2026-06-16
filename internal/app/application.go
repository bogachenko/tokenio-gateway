package app

import (
	"fmt"

	adminapp "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	ledgerapp "github.com/bogachenko/tokenio-gateway/internal/application/ledger"
	llmrequest "github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	modelcatalogapp "github.com/bogachenko/tokenio-gateway/internal/application/modelcatalog"
	pricingapp "github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	provisioningapp "github.com/bogachenko/tokenio-gateway/internal/application/provisioning"
	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
	"github.com/bogachenko/tokenio-gateway/internal/config"
	requestmetaopenaicompat "github.com/bogachenko/tokenio-gateway/internal/infrastructure/requestmeta/openaicompat"
)

type ApplicationGraph struct {
	PublicAuthentication         authenticateapp.PublicAuthenticator
	ModelCatalog                 *modelcatalogapp.Service
	ProvisioningEnabled          bool
	Provisioning                 *provisioningapp.Service
	Ledger                       *ledgerapp.Service
	AutoCharge                   *billingapp.AutoChargeService
	FailedBatchRetry             *billingapp.FailedBatchRetryService
	UsageResolver                *pricingapp.UsageResolver
	LLMRequest                   *llmrequest.Service
	ForwardingAttemptRecovery    *llmrequest.ForwardingAttemptRecovery
	TelegramStaleAttemptRecovery *telegramalert.StaleAttemptRecoveryService
	Admin                        *adminapp.Service
}

func NewApplicationGraph(
	cfg config.Config,
	primitives RuntimePrimitives,
	security SecurityGraph,
	provisioningInfrastructure ProvisioningInfrastructureGraph,
	billingInfrastructure BillingInfrastructureGraph,
	forwardingInfrastructure ForwardingInfrastructureGraph,
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
	if err := forwardingInfrastructure.Validate(); err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"validate forwarding infrastructure graph: %w",
			err,
		)
	}
	if err := repositories.Validate(); err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"validate repository graph: %w",
			err,
		)
	}

	publicAuthenticationUseCase, err := authenticateapp.NewUseCase(
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
	publicAuthentication, err :=
		authenticateapp.NewUsageRecordingAuthenticator(
			publicAuthenticationUseCase,
			repositories.APIKeyUsageRecorder,
			primitives.Clock,
			cfg.APIKeyLastUsedTimeout,
		)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct public authentication usage recorder: %w",
			err,
		)
	}

	pricingCalculator, err := pricingapp.NewCalculator(
		cfg.TokenEstimationSafetyFactor,
		cfg.CostEstimationSafetyFactor,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct shared pricing calculator: %w",
			err,
		)
	}
	modelCatalogPricing, err :=
		NewModelCatalogPublicPricingCalculator(
			pricingCalculator,
		)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct model catalog pricing adapter: %w",
			err,
		)
	}

	modelCatalog, err := modelcatalogapp.NewService(
		modelcatalogapp.Dependencies{
			Routes:         repositories.ModelCatalogRoutes,
			Resellers:      repositories.Resellers,
			Prices:         repositories.RoutePrices,
			Secrets:        security.SecretPresence,
			AdapterSupport: forwardingInfrastructure.AdapterSupport,
			RewriteSupport: forwardingInfrastructure.ModelRewriteSupport,
			PublicPricing:  modelCatalogPricing,
			Clock:          primitives.Clock,
			Currency:       cfg.CostCurrency,
		},
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct model catalog service: %w",
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

	forwardingExecutor, err :=
		NewLLMRequestForwardingExecutor(
			security.Secrets,
			forwardingInfrastructure.AdapterFactory,
			cfg.UpstreamResponseBodyMaxBytes,
		)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request forwarding executor: %w",
			err,
		)
	}
	llmRequestForwarding, err := llmrequest.NewForwardingStage(
		primitives.RouteCapacity,
		repositories.LLMRequestAtomicReservation,
		repositories.LLMRequestRouteReservationTransfer,
		repositories.ForwardingAttempts,
		primitives.Clock,
		forwardingExecutor,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request forwarding stage: %w",
			err,
		)
	}

	forwardingAttemptRecovery, err :=
		llmrequest.NewForwardingAttemptRecovery(
			repositories.ForwardingAttempts,
			primitives.Clock,
			cfg.ForwardingAttemptRecoveryStaleAfter,
		)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct forwarding attempt recovery service: %w",
			err,
		)
	}
	telegramStaleAttemptRecovery, err :=
		telegramalert.NewStaleAttemptRecoveryService(
			repositories.TelegramDeliveryAttempts,
			primitives.Clock,
			cfg.TelegramStaleAttemptRecoveryStaleAfter,
		)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct Telegram stale-attempt recovery service: %w",
			err,
		)
	}

	requestMetadata := requestmetaopenaicompat.NewAdapter()
	tokenEstimator := requestmetaopenaicompat.NewTokenEstimator()
	pricingUsageResolver, err := pricingapp.NewUsageResolver(
		forwardingInfrastructure.UsageExtractor,
		tokenEstimator,
		pricingCalculator,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request pricing usage resolver: %w",
			err,
		)
	}
	usageResolver, err := NewLLMRequestUsageResolver(
		pricingUsageResolver,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request usage resolver adapter: %w",
			err,
		)
	}
	requestFinalizer, err := NewLLMRequestFinalizer(
		ledgerService,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request finalizer: %w",
			err,
		)
	}
	requestAutoCharger, err := NewLLMRequestAutoCharger(
		autoCharge,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request auto charger: %w",
			err,
		)
	}

	preflightPricer, err := pricingapp.NewPreflightPricer(
		tokenEstimator,
		pricingCalculator,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request preflight pricer: %w",
			err,
		)
	}
	routePreflighter, err := NewLLMRequestRoutePreflighter(
		security.SecretPresence,
		preflightPricer,
		primitives.RouteCapacity,
		forwardingInfrastructure.AdapterSupport,
		forwardingInfrastructure.ModelRewriteSupport,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request route preflighter: %w",
			err,
		)
	}
	routeSelector, err := NewLLMRequestRouteSelector(
		primitives.Clock,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request route selector: %w",
			err,
		)
	}
	routePlanner, err := llmrequest.NewRepositoryRoutePlanner(
		repositories.Routes,
		repositories.Resellers,
		repositories.RoutePrices,
		routePreflighter,
		routeSelector,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request route planner: %w",
			err,
		)
	}
	requestAuthenticator, err := NewLLMRequestAuthenticator(
		publicAuthentication,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request authenticator: %w",
			err,
		)
	}
	billingAdmission, err := billingapp.NewAdmissionService(
		billingInfrastructure.Identity,
		billingInfrastructure.Balance,
		repositories.UsageLedger,
		billingapp.AdmissionConfig{
			MinimumRequestBalanceCents: cfg.MinRequestBalanceCents,
		},
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request billing admission service: %w",
			err,
		)
	}
	billingAdmitter, err := NewLLMRequestBillingAdmitter(
		billingAdmission,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request billing admitter: %w",
			err,
		)
	}
	llmRequestService, err := llmrequest.NewService(
		llmrequest.Dependencies{
			Authenticator:      requestAuthenticator,
			RequestParser:      requestMetadata,
			CapabilityDetector: requestMetadata,
			RoutePlanner:       routePlanner,
			BillingAdmitter:    billingAdmitter,
			Forwarding:         llmRequestForwarding,
			UsageResolver:      usageResolver,
			Finalizer:          requestFinalizer,
			AutoCharger:        requestAutoCharger,
		},
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request service: %w",
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

	adminBatchRetrier := newAdminFailedBatchRetrier(
		failedBatchRetry,
	)

	adminService, err := adminapp.NewService(adminapp.Dependencies{
		Users:          repositories.AdminUsers,
		APIKeys:        repositories.AdminAPIKeys,
		Provisionings:  repositories.AdminProvisioning,
		Resellers:      repositories.AdminResellers,
		Routes:         repositories.AdminRoutes,
		Prices:         repositories.AdminRoutePrices,
		PriceValidator: adminRoutePriceValidator{},
		UsagePolicy:    adminUsagePolicy{},
		Ledger:         repositories.AdminUsage,
		Audit:          repositories.AdminAudit,
		Secrets:        security.SecretPresence,
		AdapterSupport: forwardingInfrastructure.AdapterSupport,
		KeyGenerator:   security.APIKeyGenerator,
		Hasher:         security.APIKeyHasher,
		Clock:          primitives.Clock,
		BatchRetrier:   adminBatchRetrier,
	})
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct admin service: %w",
			err,
		)
	}

	graph := ApplicationGraph{
		PublicAuthentication:         publicAuthentication,
		ModelCatalog:                 modelCatalog,
		ProvisioningEnabled:          provisioningInfrastructure.Enabled,
		Provisioning:                 provisioningService,
		Ledger:                       ledgerService,
		AutoCharge:                   autoCharge,
		FailedBatchRetry:             failedBatchRetry,
		UsageResolver:                pricingUsageResolver,
		LLMRequest:                   llmRequestService,
		ForwardingAttemptRecovery:    forwardingAttemptRecovery,
		TelegramStaleAttemptRecovery: telegramStaleAttemptRecovery,
		Admin:                        adminService,
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
	case g.ModelCatalog == nil:
		return fmt.Errorf("model catalog service is nil")
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
	case g.UsageResolver == nil:
		return fmt.Errorf("LLM-request usage resolver is nil")
	case g.LLMRequest == nil:
		return fmt.Errorf("LLM-request service is nil")
	case g.ForwardingAttemptRecovery == nil:
		return fmt.Errorf(
			"forwarding attempt recovery service is nil",
		)
	case g.TelegramStaleAttemptRecovery == nil:
		return fmt.Errorf(
			"Telegram stale-attempt recovery service is nil",
		)
	case g.Admin == nil:
		return fmt.Errorf("admin service is nil")
	default:
		return nil
	}
}
