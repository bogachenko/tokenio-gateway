package app

import (
	"fmt"
	"strings"

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
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type ApplicationGraph struct {
	PublicAuthentication         authenticateapp.PublicAuthenticator
	ModelCatalog                 *modelcatalogapp.Service
	ProvisioningEnabled          bool
	Provisioning                 *provisioningapp.Service
	Ledger                       *ledgerapp.Service
	AutoCharge                   *billingapp.AutoChargeService
	BillingRecovery              *billingapp.RecoveryService
	FailedBatchRetry             *billingapp.FailedBatchRetryService
	UsageResolver                *pricingapp.UsageResolver
	LLMRequest                   *llmrequest.Service
	ForwardingAttemptRecovery    *llmrequest.ForwardingAttemptRecovery
	TelegramAlertsEnabled        bool
	TelegramAlerts               *telegramalert.Service
	TelegramAlertStore           ports.TelegramAlertStore
	TelegramDeliveryEnabled      bool
	TelegramDelivery             *telegramalert.DeliveryService
	TelegramRecovery             *telegramalert.RecoveryService
	TelegramBalanceScan          *telegramalert.BalanceScanService
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
	telegramInfrastructure TelegramInfrastructureGraph,
	loggingGraph LoggingGraph,
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
	if err := telegramInfrastructure.Validate(); err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"validate Telegram infrastructure graph: %w",
			err,
		)
	}
	if err := loggingGraph.Validate(); err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"validate logging graph: %w",
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
		modelcatalogapp.NewRoutePricePublicPricingCalculator(
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
	billingRecovery, err := billingapp.NewRecoveryService(
		repositories.BillingRecovery,
		autoCharge,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct billing recovery service: %w",
			err,
		)
	}

	routingPolicy, err := assembleRoutingPolicy(cfg)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"assemble routing policy: %w",
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
		repositories.RouteCooldowns,
		primitives.Clock,
		forwardingExecutor,
		routingPolicy,
		contextRetryWaiter{},
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM-request forwarding stage: %w",
			err,
		)
	}

	llmRequest, err := llmrequest.NewService(
		llmrequest.Dependencies{
			Authenticator:      publicAuthentication,
			RequestParser:      requestmetaopenaicompat.NewAdapter(),
			CapabilityDetector: requestmetaopenaicompat.NewAdapter(),
			RoutePlanner: NewLLMRequestRoutePlanner(
				repositories.Routes,
				repositories.RoutePrices,
				repositories.Resellers,
				security.Secrets,
			),
			BillingAdmitter: NewLLMRequestBillingAdmitter(
				billingInfrastructure.Identity,
				billingInfrastructure.Balance,
			),
			Forwarding:      llmRequestForwarding,
			UsageResolver:   pricingapp.NewUsageResolver(),
			Finalizer:       NewLLMRequestFinalizer(repositories.UsageLedger),
			AutoCharger:     autoCharge,
		},
		llmrequest.Config{RequestTimeout: cfg.LLMRequestTimeout},
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct LLM request service: %w",
			err,
		)
	}

	forwardingAttemptRecovery, err := llmrequest.NewForwardingAttemptRecovery(
		repositories.ForwardingAttempts,
		repositories.ForwardingAttemptRecovery,
		llmRequestForwarding,
		primitives.Clock,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct forwarding attempt recovery service: %w",
			err,
		)
	}

	telegramAlertsEnabled :=
		strings.TrimSpace(cfg.Telegram.BotToken) != "" &&
			strings.TrimSpace(cfg.Telegram.ChatID) != ""

	var telegramAlerts *telegramalert.Service
	var telegramDelivery *telegramalert.DeliveryService
	var telegramRecovery *telegramalert.RecoveryService
	var telegramBalanceScan *telegramalert.BalanceScanService
	var telegramStaleAttemptRecovery *telegramalert.StaleAttemptRecoveryService
	if telegramAlertsEnabled {
		telegramAlerts, err = NewTelegramAlertService(
			cfg,
			primitives,
			telegramInfrastructure,
			repositories,
		)
		if err != nil {
			return ApplicationGraph{}, fmt.Errorf(
				"construct Telegram alert service: %w",
				err,
			)
		}
		telegramDelivery, err = NewTelegramAlertDeliveryService(
			primitives,
			telegramInfrastructure,
			repositories,
		)
		if err != nil {
			return ApplicationGraph{}, fmt.Errorf(
				"construct Telegram delivery service: %w",
				err,
			)
		}
		telegramRecovery, err = telegramalert.NewRecoveryService(
			repositories.TelegramAlertDeliveryAttempts,
			telegramDelivery,
			primitives.Clock,
		)
		if err != nil {
			return ApplicationGraph{}, fmt.Errorf(
				"construct Telegram recovery service: %w",
				err,
			)
		}
		telegramBalanceScan, err = telegramalert.NewBalanceScanService(
			repositories.TelegramAlertStore,
			telegramAlerts,
			primitives.Clock,
		)
		if err != nil {
			return ApplicationGraph{}, fmt.Errorf(
				"construct Telegram balance scan service: %w",
				err,
			)
		}
		telegramStaleAttemptRecovery, err = telegramalert.NewStaleAttemptRecoveryService(
			repositories.TelegramAlertDeliveryAttempts,
			telegramDelivery,
			primitives.Clock,
			cfg.Telegram.StaleAttemptTimeout,
		)
		if err != nil {
			return ApplicationGraph{}, fmt.Errorf(
				"construct Telegram stale attempt recovery service: %w",
				err,
			)
		}
	}

	admin, err := NewAdminService(
		cfg,
		primitives,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		telegramInfrastructure,
		repositories,
	)
	if err != nil {
		return ApplicationGraph{}, fmt.Errorf(
			"construct admin service: %w",
			err,
		)
	}

	return ApplicationGraph{
		PublicAuthentication:         publicAuthentication,
		ModelCatalog:                 modelCatalog,
		ProvisioningEnabled:          provisioningInfrastructure.Enabled,
		Provisioning:                 provisioningService,
		Ledger:                       ledgerService,
		AutoCharge:                   autoCharge,
		BillingRecovery:              billingRecovery,
		FailedBatchRetry:             billingapp.NewFailedBatchRetryService(repositories.BillingRecovery, autoCharge),
		UsageResolver:                pricingapp.NewUsageResolver(),
		LLMRequest:                   llmRequest,
		ForwardingAttemptRecovery:    forwardingAttemptRecovery,
		TelegramAlertsEnabled:        telegramAlertsEnabled,
		TelegramAlerts:               telegramAlerts,
		TelegramAlertStore:           repositories.TelegramAlertStore,
		TelegramDeliveryEnabled:      telegramAlertsEnabled,
		TelegramDelivery:             telegramDelivery,
		TelegramRecovery:             telegramRecovery,
		TelegramBalanceScan:          telegramBalanceScan,
		TelegramStaleAttemptRecovery: telegramStaleAttemptRecovery,
		Admin:                        admin,
	}, nil
}
