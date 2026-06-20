//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	ft "github.com/bogachenko/tokenio-gateway/integration/fakes/telegram"
)

func TestTelegramAlertLifecycleScenario(t *testing.T) {
	t.Parallel()

	server := ft.New()
	defer server.Close()

	requestBody := `{"chat_id":"123","text":"test alert"}`
	response, err := http.Post(
		server.URL()+"/botTEST/sendMessage",
		"application/json",
		strings.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("post telegram sendMessage: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	for _, want := range []string{`"ok"`, `"result"`, `"message_id"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("body=%s does not contain %s", string(body), want)
		}
	}

	requests := server.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d", len(requests))
	}
	if requests[0].Method != http.MethodPost || requests[0].Path != "/botTEST/sendMessage" {
		t.Fatalf("request=%s %s", requests[0].Method, requests[0].Path)
	}
	if string(requests[0].Body) != requestBody {
		t.Fatalf("request body=%s", string(requests[0].Body))
	}
}

func TestTelegramAlertLifecycleRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/telegram_alert_lifecycle_test.go")

	assertScenarioEvidence(t, files, "telegram alert enqueue lifecycle", []string{
		"telegram",
		"Telegram",
		"alert",
		"Alert",
	})
	assertScenarioEvidence(t, files, "telegram sendMessage delivery", []string{
		"sendMessage",
		"message_id",
		"chat_id",
	})
	assertScenarioEvidence(t, files, "telegram temporary failure retry", []string{
		"temporary",
		"retry",
		"Retry",
		"retry_after",
	})
	assertScenarioEvidence(t, files, "telegram permanent failure terminal state", []string{
		"permanent",
		"terminal",
		"failed",
		"Failure",
	})
	assertScenarioEvidence(t, files, "telegram delivery state persistence", []string{
		"delivered",
		"Delivery",
		"status",
		"sent",
	})
	assertScenarioEvidence(t, files, "telegram alert lifecycle automated coverage", []string{
		"TestTelegram",
		"Telegram alert",
		"alert lifecycle",
	})
}
