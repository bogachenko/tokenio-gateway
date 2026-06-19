package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	gatewaylogging "github.com/bogachenko/tokenio-gateway/internal/infrastructure/logging"
)

func TestNewLoggingGraphConsumesConfig(t *testing.T) {
	var buffer bytes.Buffer
	cfg := config.Config{
		Environment: "development",
		LogLevel:    "debug",
		LogFormat:   "json",
		LogBodies:   true,
	}

	graph, err := NewLoggingGraph(cfg, &buffer)
	if err != nil {
		t.Fatalf("NewLoggingGraph: %v", err)
	}
	if err := graph.Validate(); err != nil {
		t.Fatalf("logging graph: %v", err)
	}
	if !graph.BodyLoggingEnabled {
		t.Fatal("LogBodies was not consumed by logging graph")
	}

	graph.Logger.Debug("configured debug event", "Authorization", "Bearer secret")
	output := buffer.String()
	if !strings.Contains(output, `"msg":"configured debug event"`) {
		t.Fatalf("output = %q, want json debug event", output)
	}
	if strings.Contains(output, "secret") {
		t.Fatalf("output leaked secret: %q", output)
	}
}

func TestNewLoggingGraphRejectsProductionBodyLogging(t *testing.T) {
	_, err := NewLoggingGraph(
		config.Config{
			Environment: "production",
			LogLevel:    "info",
			LogFormat:   "json",
			LogBodies:   true,
		},
		nil,
	)
	if !errors.Is(err, gatewaylogging.ErrBodyLoggingForbidden) {
		t.Fatalf("error = %v, want ErrBodyLoggingForbidden", err)
	}
}

func TestLoggingGraphValidateRejectsMissingLogger(t *testing.T) {
	var graph LoggingGraph
	if err := graph.Validate(); err == nil {
		t.Fatal("expected invalid logging graph")
	}
}
