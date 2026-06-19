//go:build integration

package integration_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFakeServiceTimeoutScenario(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		time.Sleep(100 * time.Millisecond)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"status":"too_late"}`))
	}))
	defer server.Close()

	client := &http.Client{
		Timeout: 10 * time.Millisecond,
	}

	response, err := client.Get(server.URL + "/timeout")
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) &&
		!strings.Contains(err.Error(), "Client.Timeout") {
		t.Fatalf("error=%v, want timeout", err)
	}
}
