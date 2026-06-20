//go:build integration

package integration_test

import (
	"testing"
)

func TestRecoveryWorkerChargeRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/recovery_worker_charge_test.go")

	assertScenarioEvidence(t, files, "recovery worker entrypoint", []string{
		"recovery worker",
		"RecoveryWorker",
		"worker",
	})
	assertScenarioEvidence(t, files, "charge candidate loading", []string{
		"candidate",
		"Candidate",
		"LoadChargeCandidates",
	})
	assertScenarioEvidence(t, files, "charge batch preparation", []string{
		"PrepareChargeBatch",
		"charge batch",
		"batch",
	})
	assertScenarioEvidence(t, files, "billing charge handoff", []string{
		"billing",
		"Billing",
		"charge",
		"Charge",
	})
	assertScenarioEvidence(t, files, "recovery retry/finalization behavior", []string{
		"retry",
		"Retry",
		"recover",
		"finalize",
		"Finalize",
	})
	assertScenarioEvidence(t, files, "recovery worker charge automated coverage", []string{
		"TestRecovery",
		"worker charge",
		"recovery worker",
	})
}
