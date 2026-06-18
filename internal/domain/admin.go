package domain

import "time"

type AuditAction string

const (
	AuditActionUserCreate            AuditAction = "user.create"
	AuditActionUserEnable            AuditAction = "user.enable"
	AuditActionUserDisable           AuditAction = "user.disable"
	AuditActionAPIKeyCreate          AuditAction = "api_key.create"
	AuditActionAPIKeyRevoke          AuditAction = "api_key.revoke"
	AuditActionResellerCreate        AuditAction = "reseller.create"
	AuditActionResellerUpdate        AuditAction = "reseller.update"
	AuditActionResellerEnable        AuditAction = "reseller.enable"
	AuditActionResellerDisable       AuditAction = "reseller.disable"
	AuditActionResellerBalanceAdjust AuditAction = "reseller_balance.adjust"
	AuditActionResellerBalanceSet    AuditAction = "reseller_balance.set"
	AuditActionRouteCreate           AuditAction = "route.create"
	AuditActionRouteUpdate           AuditAction = "route.update"
	AuditActionRouteEnable           AuditAction = "route.enable"
	AuditActionRouteDisable          AuditAction = "route.disable"
	AuditActionRouteCooldownSet      AuditAction = "route_cooldown.set"
	AuditActionRouteCooldownClear    AuditAction = "route_cooldown.clear"
	AuditActionRoutePriceUpsert      AuditAction = "route_price.upsert"
	AuditActionUsageResolveBillable  AuditAction = "usage.resolve_billable"
	AuditActionUsageResolveFailed    AuditAction = "usage.resolve_failed"
	AuditActionUsageResolveCharged   AuditAction = "usage.resolve_charged"
	AuditActionBillingChargeRetry    AuditAction = "billing_charge.retry"
)

type AuditState map[string]any

type AuditContext struct {
	ID           string
	AdminSubject string
	Action       AuditAction
	EntityType   string
	EntityID     string
	BeforeState  AuditState
	AfterState   AuditState
	Reason       string
	RequestID    string
	CreatedAt    time.Time
}

type AdminAuditEntry struct {
	ID           string      `json:"id"`
	AdminSubject string      `json:"admin_subject"`
	Action       AuditAction `json:"action"`
	EntityType   string      `json:"entity_type"`
	EntityID     string      `json:"entity_id"`
	BeforeState  AuditState  `json:"before_state"`
	AfterState   AuditState  `json:"after_state"`
	Reason       string      `json:"reason,omitempty"`
	RequestID    string      `json:"request_id"`
	CreatedAt    time.Time   `json:"created_at"`
}
