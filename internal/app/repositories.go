package app

import (
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/postgres"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type RepositoryGraph struct {
	Users                    ports.UserRepository
	APIKeys                  ports.APIKeyRepository
	APIKeyUsageRecorder      ports.APIKeyUsageRecorder
	Resellers                ports.ResellerQueryRepository
	Routes                   ports.RouteRepository
	ModelCatalogRoutes       ports.ModelCatalogRouteRepository
	RoutePrices              ports.RoutePriceRepository
	UsageLedger              ports.UsageLedger
	BillingRecovery          ports.BillingRecoveryStore
	ForwardingAttempts       ports.ForwardingAttemptStore
	TelegramDeliveryAttempts ports.TelegramDeliveryAttemptStore

	LLMRequestAtomicReservation        llmrequest.AtomicReservation
	LLMRequestRouteReservationTransfer llmrequest.RouteReservationTransfer

	AdminUsers        ports.AdminUserRepository
	AdminAPIKeys      ports.AdminAPIKeyRepository
	AdminProvisioning ports.AdminAPIKeyProvisioningRepository
	AdminAudit        ports.AdminAuditStore
	AdminResellers    ports.ResellerRepository
	AdminRoutes       ports.AdminRouteRepository
	AdminRoutePrices  ports.AdminRoutePriceRepository
	AdminUsage        ports.AdminUsageLedger

	BillingSessions ports.BillingSessionStore
	RouteEvents     ports.RouteEventStore
	TelegramAlerts  ports.TelegramAlertStore

	APIKeyProvisioning ports.APIKeyProvisioningStore
}

func NewRepositoryGraph(
	db *postgres.DB,
	clock ports.Clock,
) (RepositoryGraph, error) {
	users, err := postgres.NewUserRepository(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct user repository: %w",
			err,
		)
	}
	apiKeys, err := postgres.NewAPIKeyRepository(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct API-key repository: %w",
			err,
		)
	}
	resellers, err := postgres.NewResellerRepository(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct reseller query repository: %w",
			err,
		)
	}
	routes, err := postgres.NewRouteRepository(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct route repository: %w",
			err,
		)
	}
	routePrices, err := postgres.NewRoutePriceRepository(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct route-price repository: %w",
			err,
		)
	}
	usageLedger, err := postgres.NewUsageLedger(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct usage ledger: %w",
			err,
		)
	}
	forwardingAttempts, err :=
		postgres.NewForwardingAttemptStore(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct forwarding-attempt store: %w",
			err,
		)
	}
	telegramDeliveryAttempts, err :=
		postgres.NewTelegramDeliveryAttemptStore(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct Telegram delivery-attempt store: %w",
			err,
		)
	}
	atomicReservation, err :=
		postgres.NewLLMRequestAtomicReservation(db, clock)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct LLM-request atomic reservation: %w",
			err,
		)
	}
	routeReservationTransfer, err :=
		postgres.NewLLMRequestRouteReservationTransfer(db, clock)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct LLM-request route reservation transfer: %w",
			err,
		)
	}

	adminUsers, err := postgres.NewAdminUserRepository(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct admin user repository: %w",
			err,
		)
	}
	adminAPIKeys, err := postgres.NewAdminAPIKeyRepository(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct admin API-key repository: %w",
			err,
		)
	}
	adminProvisioning, err :=
		postgres.NewAdminAPIKeyProvisioningRepository(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct admin API-key provisioning repository: %w",
			err,
		)
	}
	adminAudit, err := postgres.NewAdminAuditStore(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct admin audit store: %w",
			err,
		)
	}
	adminResellers, err := postgres.NewAdminResellerRepository(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct admin reseller repository: %w",
			err,
		)
	}
	adminRoutes, err := postgres.NewAdminRouteRepository(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct admin route repository: %w",
			err,
		)
	}
	adminRoutePrices, err :=
		postgres.NewAdminRoutePriceRepository(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct admin route-price repository: %w",
			err,
		)
	}
	adminUsage, err := postgres.NewAdminUsageLedger(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct admin usage ledger: %w",
			err,
		)
	}

	billingSessions, err := postgres.NewBillingSessionStore(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct billing-session store: %w",
			err,
		)
	}
	routeEvents, err := postgres.NewRouteEventStore(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct route-event store: %w",
			err,
		)
	}
	telegramAlerts, err := postgres.NewTelegramAlertStore(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct Telegram-alert store: %w",
			err,
		)
	}
	apiKeyProvisioning, err :=
		postgres.NewAPIKeyProvisioningStore(db)
	if err != nil {
		return RepositoryGraph{}, fmt.Errorf(
			"construct API-key provisioning store: %w",
			err,
		)
	}

	return RepositoryGraph{
		Users:                    users,
		APIKeys:                  apiKeys,
		APIKeyUsageRecorder:      apiKeys,
		Resellers:                resellers,
		Routes:                   routes,
		ModelCatalogRoutes:       routes,
		RoutePrices:              routePrices,
		UsageLedger:              usageLedger,
		BillingRecovery:          usageLedger,
		ForwardingAttempts:       forwardingAttempts,
		TelegramDeliveryAttempts: telegramDeliveryAttempts,

		LLMRequestAtomicReservation:        atomicReservation,
		LLMRequestRouteReservationTransfer: routeReservationTransfer,

		AdminUsers:        adminUsers,
		AdminAPIKeys:      adminAPIKeys,
		AdminProvisioning: adminProvisioning,
		AdminAudit:        adminAudit,
		AdminResellers:    adminResellers,
		AdminRoutes:       adminRoutes,
		AdminRoutePrices:  adminRoutePrices,
		AdminUsage:        adminUsage,

		BillingSessions: billingSessions,
		RouteEvents:     routeEvents,
		TelegramAlerts:  telegramAlerts,

		APIKeyProvisioning: apiKeyProvisioning,
	}, nil
}

func (g RepositoryGraph) Validate() error {
	switch {
	case g.Users == nil:
		return fmt.Errorf("user repository is nil")
	case g.APIKeys == nil:
		return fmt.Errorf("API-key repository is nil")
	case g.APIKeyUsageRecorder == nil:
		return fmt.Errorf("API-key usage recorder is nil")
	case g.Resellers == nil:
		return fmt.Errorf("reseller query repository is nil")
	case g.Routes == nil:
		return fmt.Errorf("route repository is nil")
	case g.ModelCatalogRoutes == nil:
		return fmt.Errorf(
			"model catalog route repository is nil",
		)
	case g.RoutePrices == nil:
		return fmt.Errorf("route-price repository is nil")
	case g.UsageLedger == nil:
		return fmt.Errorf("usage ledger is nil")
	case g.BillingRecovery == nil:
		return fmt.Errorf("billing recovery store is nil")
	case g.ForwardingAttempts == nil:
		return fmt.Errorf("forwarding-attempt store is nil")
	case g.TelegramDeliveryAttempts == nil:
		return fmt.Errorf(
			"Telegram delivery-attempt store is nil",
		)
	case g.LLMRequestAtomicReservation == nil:
		return fmt.Errorf(
			"LLM-request atomic reservation is nil",
		)
	case g.LLMRequestRouteReservationTransfer == nil:
		return fmt.Errorf(
			"LLM-request route reservation transfer is nil",
		)
	case g.AdminUsers == nil:
		return fmt.Errorf("admin user repository is nil")
	case g.AdminAPIKeys == nil:
		return fmt.Errorf("admin API-key repository is nil")
	case g.AdminProvisioning == nil:
		return fmt.Errorf(
			"admin API-key provisioning repository is nil",
		)
	case g.AdminAudit == nil:
		return fmt.Errorf("admin audit store is nil")
	case g.AdminResellers == nil:
		return fmt.Errorf("admin reseller repository is nil")
	case g.AdminRoutes == nil:
		return fmt.Errorf("admin route repository is nil")
	case g.AdminRoutePrices == nil:
		return fmt.Errorf("admin route-price repository is nil")
	case g.AdminUsage == nil:
		return fmt.Errorf("admin usage ledger is nil")
	case g.BillingSessions == nil:
		return fmt.Errorf("billing-session store is nil")
	case g.RouteEvents == nil:
		return fmt.Errorf("route-event store is nil")
	case g.TelegramAlerts == nil:
		return fmt.Errorf("Telegram-alert store is nil")
	case g.APIKeyProvisioning == nil:
		return fmt.Errorf("API-key provisioning store is nil")
	default:
		return nil
	}
}
