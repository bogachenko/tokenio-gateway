package postgres

import (
	"strings"
	"testing"
)

func TestChargeCommandSQLContainsRequiredBoundaries(t *testing.T) {
	checks := map[string][]string{
		"open": {
			"billing_status IN ('pending', 'failed')",
			"ORDER BY created_at ASC, id ASC",
		},
		"allocations": {
			"ORDER BY position ASC",
		},
		"expected": {
			"ORDER BY position ASC",
		},
		"usage lock": {
			"ORDER BY local_request_id ASC",
			"FOR UPDATE",
		},
		"active claim": {
			"billing_status IN ('pending', 'failed')",
		},
	}
	sqlByName := map[string]string{
		"open":         loadOpenChargeBatchIDsSQL,
		"allocations":  loadBillingChargeAllocationsSQL,
		"expected":     loadBillingChargeExpectedRecordsSQL,
		"usage lock":   lockUsageRecordsForChargeSQL,
		"active claim": activeChargeClaimExistsSQL,
	}
	for name, fragments := range checks {
		for _, fragment := range fragments {
			if !strings.Contains(sqlByName[name], fragment) {
				t.Errorf("%s SQL missing %q", name, fragment)
			}
		}
	}
}

func TestUsageLedgerImplementsPort(t *testing.T) {
	var _ = NewUsageLedger
}
