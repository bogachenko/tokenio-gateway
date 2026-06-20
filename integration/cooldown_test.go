//go:build integration

package integration_test

import (
	"testing"
)

func TestCooldownRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/cooldown_test.go")

	assertScenarioEvidence(t, files, "cooldown policy/state", []string{
		"cooldown",
		"Cooldown",
	})
	assertScenarioEvidence(t, files, "cooldown rejection path", []string{
		"cooldown",
		"retry_after",
		"Retry-After",
		"too many",
	})
	assertScenarioEvidence(t, files, "cooldown time/window semantics", []string{
		"until",
		"expires",
		"duration",
		"window",
	})
	assertScenarioEvidence(t, files, "cooldown provider or route scope", []string{
		"provider",
		"route",
		"model",
		"api_key",
	})
	assertScenarioEvidence(t, files, "cooldown automated coverage", []string{
		"TestCooldown",
		"cooldown",
	})
}
