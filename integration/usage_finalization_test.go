//go:build integration

package integration_test

import (
	"testing"
)

func TestUsageFinalizationRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/usage_finalization_test.go")

	assertScenarioEvidence(t, files, "usage extraction", []string{
		"usage",
		"Usage",
		"prompt_tokens",
		"input_tokens",
		"total_tokens",
	})
	assertScenarioEvidence(t, files, "usage finalization state", []string{
		"finalize",
		"Finalize",
		"finalized",
		"Finalized",
	})
	assertScenarioEvidence(t, files, "usage persistence", []string{
		"usage",
		"ledger",
		"Ledger",
		"record",
	})
	assertScenarioEvidence(t, files, "billing handoff", []string{
		"billing",
		"Billing",
		"charge",
		"Charge",
	})
	assertScenarioEvidence(t, files, "usage finalization failure-safe behavior", []string{
		"missing usage",
		"malformed usage",
		"unknown",
		"failure",
	})
	assertScenarioEvidence(t, files, "usage finalization automated coverage", []string{
		"TestUsage",
		"usage finalization",
		"finalization",
	})
}
