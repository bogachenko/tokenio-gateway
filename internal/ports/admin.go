package ports

import (
	"context"
	"errors"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

var (
	ErrAdminConflict      = errors.New("admin conflict")
	ErrAdminStateConflict = errors.New("admin state conflict")
)

type PageRequest struct{ Limit, Offset int }
type Page[T any] struct {
	Items []T
	Total int64
}

type UserListFilter struct {
	Enabled *bool
	Email   string
	Page    PageRequest
}
type APIKeyListFilter struct {
	UserID string
	Page   PageRequest
}
type ResellerListFilter struct {
	ProviderType domain.ProviderType
	Enabled      *bool
	Page         PageRequest
}
type RouteListFilter struct {
	ResellerID   string
	ProviderType domain.ProviderType
	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	ClientModel  string
	Enabled      *bool
	Page         PageRequest
}
type UsageListFilter struct {
	UserID                                           string
	Status                                           domain.UsageStatus
	ProviderType                                     domain.ProviderType
	ClientModel, SelectedRouteID, SelectedResellerID string
	CreatedFrom, CreatedTo                           *time.Time
	Page                                             PageRequest
}
type BillingChargeBatchListFilter struct {
	UserID                 string
	ProviderType           domain.ProviderType
	ClientModel            string
	Status                 domain.BillingChargeStatus
	CreatedFrom, CreatedTo *time.Time
	Page                   PageRequest
}
type AuditListFilter struct {
	AdminSubject           string
	Action                 domain.AuditAction
	EntityType, EntityID   string
	CreatedFrom, CreatedTo *time.Time
	Page                   PageRequest
}

// AdminUserRepository is the administrative capability extension of the canonical UserRepository.
type AdminUserRepository interface {
	UserRepository
	ListUsers(context.Context, UserListFilter) (Page[domain.User], error)
	CreateUserWithAudit(context.Context, domain.User, domain.AuditContext) (domain.User, error)
	CompareAndSwapUserWithAudit(context.Context, domain.User, domain.User, domain.AuditContext) (domain.User, error)
}

type AdminAPIKeyRepository interface {
	APIKeyRepository
	FindAPIKeyByID(context.Context, string) (*domain.APIKeyRecord, error)
	ListAPIKeys(context.Context, APIKeyListFilter) (Page[domain.APIKeyRecord], error)
	CreateAPIKeyWithAudit(context.Context, domain.APIKeyRecord, domain.AuditContext) error
	CompareAndSwapAPIKeyWithAudit(context.Context, domain.APIKeyRecord, domain.APIKeyRecord, domain.AuditContext) (domain.APIKeyRecord, error)
}

type ResellerRepository interface {
	FindResellerByID(context.Context, string) (*domain.Reseller, error)
	ListResellers(context.Context, ResellerListFilter) (Page[domain.Reseller], error)
	CreateResellerWithAudit(context.Context, domain.Reseller, domain.AuditContext) (domain.Reseller, error)
	CompareAndSwapResellerWithAudit(context.Context, domain.Reseller, domain.Reseller, domain.AuditContext) (domain.Reseller, error)
}

type AdminRouteRepository interface {
	RouteRepository
	FindRouteByID(context.Context, string) (*domain.Route, error)
	ListRoutes(context.Context, RouteListFilter) (Page[domain.Route], error)
	CreateRouteWithAudit(context.Context, domain.Route, domain.AuditContext) (domain.Route, error)
	CompareAndSwapRouteWithAudit(context.Context, domain.Route, domain.Route, domain.AuditContext) (domain.Route, error)
}

type AdminRoutePriceRepository interface {
	RoutePriceRepository
	FindRoutePrice(context.Context, string) (*domain.RoutePrice, error)
	UpsertRoutePriceWithAudit(context.Context, *domain.RoutePrice, domain.RoutePrice, domain.AuditContext) (domain.RoutePrice, error)
}

type AdminUsageLedger interface {
	UsageLedger
	ListUsageRecords(context.Context, UsageListFilter) (Page[domain.UsageRecord], error)
	ResolvePricingFailedWithAudit(context.Context, domain.UsageRecord, domain.UsageRecord, domain.AuditContext) (UsageTransitionResult, error)
	ListBillingChargeBatches(context.Context, BillingChargeBatchListFilter) (Page[domain.BillingChargeBatch], error)
	LoadChargeBatchByID(context.Context, string) (BillingChargeBatchSnapshot, error)
	// RecordChargeRetryAttemptWithAudit atomically verifies that the exact failed
	// persisted snapshot is still current and appends the retry audit entry before
	// any external billing side effect is attempted.
	RecordChargeRetryAttemptWithAudit(context.Context, BillingChargeBatchSnapshot, domain.AuditContext) error
	ApplyChargeRetrySuccessWithAudit(context.Context, UsageChargeSuccess, domain.AuditContext) error
	MarkChargeRetryFailedWithAudit(context.Context, string, domain.BillingChargeStatus, string, time.Time, domain.AuditContext) error
}

type AdminAuditStore interface {
	ListAuditEntries(context.Context, AuditListFilter) (Page[domain.AdminAuditEntry], error)
}
type SecretPresenceChecker interface {
	Exists(context.Context, string) (bool, error)
}
type APIKeyGenerator interface{ GenerateAPIKey() (string, error) }
