package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestNewWiresTextLoggerLevelAndRedaction(t *testing.T) {
	var buffer bytes.Buffer
	logger, _, err := New(Options{
		Environment: "development",
		Level:       "debug",
		Format:      "text",
		Output:      &buffer,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.Debug("debug event", slog.String("Authorization", "Bearer secret-token"))

	output := buffer.String()
	if !strings.Contains(output, "debug event") {
		t.Fatalf("output = %q, want debug log", output)
	}
	if strings.Contains(output, "secret-token") {
		t.Fatalf("output leaked secret: %q", output)
	}
	if !strings.Contains(output, redacted) {
		t.Fatalf("output = %q, want URL-escaped redaction marker", output)
	}
}

func TestNewWiresJSONLogger(t *testing.T) {
	var buffer bytes.Buffer
	logger, _, err := New(Options{
		Environment: "test",
		Level:       "info",
		Format:      "json",
		Output:      &buffer,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.Info("json event", slog.String("x-goog-api-key", "google-secret"))

	output := buffer.String()
	if !strings.Contains(output, `"msg":"json event"`) {
		t.Fatalf("output = %q, want json slog output", output)
	}
	if strings.Contains(output, "google-secret") {
		t.Fatalf("output leaked secret: %q", output)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	_, _, err := New(Options{Environment: "test", Level: "trace", Format: "text"})
	if !errors.Is(err, ErrInvalidLogLevel) {
		t.Fatalf("level error = %v, want ErrInvalidLogLevel", err)
	}

	_, _, err = New(Options{Environment: "test", Level: "info", Format: "xml"})
	if !errors.Is(err, ErrInvalidLogFormat) {
		t.Fatalf("format error = %v, want ErrInvalidLogFormat", err)
	}
}

func TestNewRejectsBodyLoggingInProduction(t *testing.T) {
	_, _, err := New(Options{
		Environment: "production",
		Level:       "info",
		Format:      "json",
		LogBodies:   true,
	})
	if !errors.Is(err, ErrBodyLoggingForbidden) {
		t.Fatalf("error = %v, want ErrBodyLoggingForbidden", err)
	}
}

func TestRedactorHeaderAndDSN(t *testing.T) {
	redactor := Redactor{}
	headers := http.Header{
		"X-API-Key":     {"secret-key"},
		"Authorization": {"Bearer secret-token"},
		"X-Request-ID":  {"req_123"},
	}

	redactedHeaders := redactor.Header(headers)
	if value := redactedHeaders.Get("X-API-Key"); value != redacted {
		t.Fatalf("X-API-Key = %q, want redacted", value)
	}
	if value := redactedHeaders.Get("Authorization"); value != redacted {
		t.Fatalf("Authorization = %q, want redacted", value)
	}
	if value := redactedHeaders.Get("X-Request-ID"); value != "req_123" {
		t.Fatalf("X-Request-ID = %q", value)
	}
	if got := headers["X-API-Key"][0]; got != "secret-key" {
		t.Fatalf("redactor mutated original headers: %q", got)
	}
	redactedHeaders.Set("X-Request-ID", "changed")
	if got := headers["X-Request-ID"][0]; got != "req_123" {
		t.Fatalf("redactor returned header aliases original values: %q", got)
	}

	dsn := "postgres://user:secret-password@localhost:5432/tokenio"
	redactedDSN := redactor.DSN(dsn)
	if strings.Contains(redactedDSN, "secret-password") {
		t.Fatalf("DSN leaked password: %s", redactedDSN)
	}
	if !strings.Contains(redactedDSN, "REDACTED") {
		t.Fatalf("DSN = %s, want redaction marker", redactedDSN)
	}
}
