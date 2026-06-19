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
		"Authorization": []string{"Bearer secret"},
		"Content-Type":  []string{"application/json"},
	}

	redactedHeaders := redactor.Header(headers)
	if got := redactedHeaders.Get("Authorization"); got != redacted {
		t.Fatalf("Authorization = %q, want redacted", got)
	}
	if got := redactedHeaders.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if headers.Get("Authorization") != "Bearer secret" {
		t.Fatal("Header mutated original headers")
	}

	redactedDSN := redactor.DSN("postgres://user:password@localhost:5432/tokenio")
	if strings.Contains(redactedDSN, "password") {
		t.Fatalf("DSN leaked password: %s", redactedDSN)
	}
	if !strings.Contains(redactedDSN, redacted) {
		t.Fatalf("DSN = %s, want URL-escaped redaction marker", redactedDSN)
	}
}
