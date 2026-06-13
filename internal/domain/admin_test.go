package domain

import "testing"

func TestAdminAuditActionsAreExactAndUnique(t *testing.T) {
	actions := []AuditAction{
		AuditActionUserCreate, AuditActionUserEnable, AuditActionUserDisable,
		AuditActionAPIKeyCreate, AuditActionAPIKeyRevoke,
		AuditActionResellerCreate, AuditActionResellerUpdate, AuditActionResellerEnable, AuditActionResellerDisable,
		AuditActionResellerBalanceAdjust, AuditActionResellerBalanceSet,
		AuditActionRouteCreate, AuditActionRouteUpdate, AuditActionRouteEnable, AuditActionRouteDisable,
		AuditActionRouteCooldownSet, AuditActionRouteCooldownClear, AuditActionRoutePriceUpsert,
		AuditActionUsageResolveBillable, AuditActionUsageResolveFailed, AuditActionUsageResolveCharged,
		AuditActionBillingChargeRetry,
	}
	expected := []string{
		"user.create", "user.enable", "user.disable", "api_key.create", "api_key.revoke",
		"reseller.create", "reseller.update", "reseller.enable", "reseller.disable",
		"reseller_balance.adjust", "reseller_balance.set", "route.create", "route.update",
		"route.enable", "route.disable", "route_cooldown.set", "route_cooldown.clear",
		"route_price.upsert", "usage.resolve_billable", "usage.resolve_failed",
		"usage.resolve_charged", "billing_charge.retry",
	}
	seen := map[AuditAction]bool{}
	for i, action := range actions {
		if string(action) != expected[i] {
			t.Fatalf("action[%d]=%q expected=%q", i, action, expected[i])
		}
		if seen[action] {
			t.Fatalf("duplicate action %q", action)
		}
		seen[action] = true
	}
}
