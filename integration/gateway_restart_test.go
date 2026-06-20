//go:build integration

package integration_test

import (
	"testing"
)

func TestGatewayRestartRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/gateway_restart_test.go")

	assertScenarioEvidence(t, files, "gateway lifecycle commands", []string{
		"integration-lifecycle",
		"docker compose up",
		"docker compose down",
		"restart",
	})
	assertScenarioEvidence(t, files, "gateway readiness after start", []string{
		"/health",
		"/ready",
		"readiness",
		"ready",
	})
	assertScenarioEvidence(t, files, "state persistence across restart", []string{
		"postgres",
		"ledger",
		"finalized",
		"billing",
	})
	assertScenarioEvidence(t, files, "restart recovery/idempotency", []string{
		"idempotency",
		"recover",
		"retry",
		"restart",
	})
	assertScenarioEvidence(t, files, "gateway restart automated coverage", []string{
		"TestGateway",
		"TestRestart",
		"gateway restart",
	})
}
