//go:build integration

package integration_test

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanMigrationLifecycle(t *testing.T) {
	if os.Getenv("TOKENIO_RUN_DOCKER_INTEGRATION_LIFECYCLE") != "1" {
		t.Skip("TOKENIO_RUN_DOCKER_INTEGRATION_LIFECYCLE=1 is not set")
	}

	repoRoot := integrationRepoRoot(t)
	projectName := "tokenio_gateway_clean_migration_lifecycle"
	postgresPort := freeTCPPort(t)
	migrationsHostDir := integrationMigrationsHostDir(t, repoRoot)

	runIntegrationScript(t, repoRoot, projectName, postgresPort, migrationsHostDir, "./scripts/integration-postgres-down.sh")
	t.Cleanup(func() {
		runIntegrationScript(t, repoRoot, projectName, postgresPort, migrationsHostDir, "./scripts/integration-postgres-down.sh")
	})

	runIntegrationScript(t, repoRoot, projectName, postgresPort, migrationsHostDir, "./scripts/integration-postgres-up.sh")
	runIntegrationScript(t, repoRoot, projectName, postgresPort, migrationsHostDir, "./scripts/docker-compose-migrate.sh")
}

func integrationRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, ".."))
}

func freeTCPPort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate free TCP port: %v", err)
	}
	defer listener.Close()

	return fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
}

func integrationMigrationsHostDir(t *testing.T, repoRoot string) string {
	t.Helper()

	var found string
	err := filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if found != "" {
			return filepath.SkipAll
		}
		if !entry.IsDir() {
			return nil
		}

		name := entry.Name()
		if name == ".git" || name == "vendor" || strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}

		matches, err := filepath.Glob(filepath.Join(path, "*.sql"))
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		found = "." + string(filepath.Separator) + rel
		return filepath.SkipAll
	})
	if err != nil {
		t.Fatalf("find migrations directory: %v", err)
	}
	if found == "" {
		t.Fatal("no SQL migrations directory found")
	}
	return found
}

func runIntegrationScript(t *testing.T, repoRoot string, projectName string, postgresPort string, migrationsHostDir string, script string) {
	t.Helper()

	command := exec.Command(script)
	command.Dir = repoRoot
	command.Env = append(
		os.Environ(),
		"COMPOSE_PROJECT_NAME="+projectName,
		"TOKENIO_POSTGRES_PORT="+postgresPort,
		"TOKENIO_MIGRATIONS_HOST_DIR="+migrationsHostDir,
	)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", script, err, string(output))
	}
}
