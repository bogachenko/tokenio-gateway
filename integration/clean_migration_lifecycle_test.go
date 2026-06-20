//go:build integration

package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCleanMigrationLifecycle(t *testing.T) {
	if os.Getenv("TOKENIO_RUN_DOCKER_INTEGRATION_LIFECYCLE") != "1" {
		t.Skip("TOKENIO_RUN_DOCKER_INTEGRATION_LIFECYCLE=1 is not set")
	}

	repoRoot := integrationRepoRoot(t)
	projectName := "tokenio_gateway_clean_migration_lifecycle"

	runIntegrationScript(t, repoRoot, projectName, "./scripts/integration-postgres-down.sh")
	t.Cleanup(func() {
		runIntegrationScript(t, repoRoot, projectName, "./scripts/integration-postgres-down.sh")
	})

	runIntegrationScript(t, repoRoot, projectName, "./scripts/integration-postgres-up.sh")
	runIntegrationScript(t, repoRoot, projectName, "./scripts/docker-compose-migrate.sh")
}

func integrationRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, ".."))
}

func runIntegrationScript(t *testing.T, repoRoot string, projectName string, script string) {
	t.Helper()

	command := exec.Command(script)
	command.Dir = repoRoot
	command.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+projectName)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", script, err, string(output))
	}
}
