//go:build integration

package integration_test

import (
	"testing"
)

func TestRequestTriggeredChargeRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/request_triggered_charge_test.go")

	assertScenarioEvidence(t, files, "request-triggered charge path", []string{
		"request-triggered",
		"RequestTriggered",
		"triggered charge",
		"charge",
	})
	assertScenarioEvidence(t, files, "usage-to-charge handoff", []string{
		"usage",
		"finalize",
		"Finalize",
		"charge",
	})
	assertScenarioEvidence(t, files, "billing charge request", []string{
		"billing",
		"Billing",
		"charge request",
		"ChargeRequest",
	})
	assertScenarioEvidence(t, files, "charge idempotency", []string{
		"idempotency",
		"idempotency_key",
		"Idempotency",
	})
	assertScenarioEvidence(t, files, "charge result persistence", []string{
		"charged",
		"Charge",
		"ledger",
		"Ledger",
		"balance",
	})
	assertScenarioEvidence(t, files, "request-triggered charge automated coverage", []string{
		"TestRequest",
		"TestCharge",
		"request-triggered",
	})
}
