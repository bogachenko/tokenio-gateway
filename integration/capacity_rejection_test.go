//go:build integration

package integration_test

import (
	"testing"
)

func TestCapacityRejectionRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/capacity_rejection_test.go")

	assertScenarioEvidence(t, files, "capacity policy/state", []string{
		"capacity",
		"Capacity",
	})
	assertScenarioEvidence(t, files, "capacity rejection reason", []string{
		"capacity",
		"rejected",
		"rejection",
		"no_capacity",
	})
	assertScenarioEvidence(t, files, "capacity HTTP rejection evidence", []string{
		"StatusTooManyRequests",
		"429",
		"StatusServiceUnavailable",
		"503",
	})
	assertScenarioEvidence(t, files, "capacity provider route scope", []string{
		"provider",
		"route",
		"model",
	})
	assertScenarioEvidence(t, files, "capacity terminal no fallback path", []string{
		"no_route",
		"no route",
		"fallback",
		"terminal",
	})
	assertScenarioEvidence(t, files, "capacity automated coverage", []string{
		"TestCapacity",
		"capacity",
	})
}
