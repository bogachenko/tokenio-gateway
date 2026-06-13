package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStage10MajorAdminAuditActionContractValues(t *testing.T) {
	values := map[AuditAction]string{
		AuditActionUserCreate:            "user.create",
		AuditActionUserEnable:            "user.enable",
		AuditActionUserDisable:           "user.disable",
		AuditActionAPIKeyCreate:          "api_key.create",
		AuditActionAPIKeyRevoke:          "api_key.revoke",
		AuditActionResellerCreate:        "reseller.create",
		AuditActionResellerUpdate:        "reseller.update",
		AuditActionResellerEnable:        "reseller.enable",
		AuditActionResellerDisable:       "reseller.disable",
		AuditActionResellerBalanceAdjust: "reseller_balance.adjust",
		AuditActionResellerBalanceSet:    "reseller_balance.set",
		AuditActionRouteCreate:           "route.create",
		AuditActionRouteUpdate:           "route.update",
		AuditActionRouteEnable:           "route.enable",
		AuditActionRouteDisable:          "route.disable",
		AuditActionRouteCooldownSet:      "route_cooldown.set",
		AuditActionRouteCooldownClear:    "route_cooldown.clear",
		AuditActionRoutePriceUpsert:      "route_price.upsert",
		AuditActionUsageResolveBillable:  "usage.resolve_billable",
		AuditActionUsageResolveFailed:    "usage.resolve_failed",
		AuditActionUsageResolveCharged:   "usage.resolve_charged",
		AuditActionBillingChargeRetry:    "billing_charge.retry",
	}
	for action, want := range values {
		if string(action) != want {
			t.Fatalf("action = %q, want %q", action, want)
		}
	}
}

func TestStage10MajorAuditEntryJSONDoesNotInventSecretFields(t *testing.T) {
	entry := AdminAuditEntry{
		ID:           "audit_1",
		AdminSubject: "admin_token",
		Action:       AuditActionAPIKeyCreate,
		EntityType:   "api_key",
		EntityID:     "ak_1",
		BeforeState:  AuditState{},
		AfterState:   AuditState{"key_prefix": "sk_live_abcd..."},
		RequestID:    "admreq_1",
		CreatedAt:    time.Unix(1, 0).UTC(),
	}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"key_hash", "raw_api_key", "authorization", "service_token"} {
		if strings.Contains(strings.ToLower(string(body)), forbidden) {
			t.Fatalf("forbidden audit field %q in %s", forbidden, body)
		}
	}
}
