package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	gatewaylogging "github.com/bogachenko/tokenio-gateway/internal/infrastructure/logging"
)

type LoggingGraph struct {
	Logger             *slog.Logger
	StdLogger          *log.Logger
	Redactor           gatewaylogging.Redactor
	BodyLoggingEnabled bool
}

func NewLoggingGraph(
	cfg config.Config,
	output io.Writer,
) (LoggingGraph, error) {
	logger, redactor, err := gatewaylogging.New(
		gatewaylogging.Options{
			Environment: cfg.Environment,
			Level:       cfg.LogLevel,
			Format:      cfg.LogFormat,
			LogBodies:   cfg.LogBodies,
			Output:      output,
		},
	)
	if err != nil {
		return LoggingGraph{}, fmt.Errorf(
			"construct logging graph: %w",
			err,
		)
	}

	graph := LoggingGraph{
		Logger:             logger,
		StdLogger:          slog.NewLogLogger(logger.Handler(), slog.LevelInfo),
		Redactor:           redactor,
		BodyLoggingEnabled: cfg.LogBodies,
	}
	if err := graph.Validate(); err != nil {
		return LoggingGraph{}, err
	}
	return graph, nil
}

func (g LoggingGraph) Validate() error {
	if g.Logger == nil {
		return fmt.Errorf("logging graph logger is nil")
	}
	if g.StdLogger == nil {
		return fmt.Errorf("logging graph stdlib bridge logger is nil")
	}
	return nil
}

func NewProcessLoggingGraph(output io.Writer) (LoggingGraph, error) {
	logBodies := false
	if raw := strings.TrimSpace(os.Getenv("TOKENIO_LOG_BODIES")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return LoggingGraph{}, fmt.Errorf(
				"TOKENIO_LOG_BODIES must be bool: %w",
				err,
			)
		}
		logBodies = parsed
	}

	cfg := config.Config{
		Environment: envOr("TOKENIO_ENV", "production"),
		LogLevel:    envOr("TOKENIO_LOG_LEVEL", "info"),
		LogFormat:   envOr("TOKENIO_LOG_FORMAT", "text"),
		LogBodies:   logBodies,
	}
	return NewLoggingGraph(cfg, output)
}

func GatewayMain() int {
	if err := Run(); err != nil {
		return logProcessError("gateway error", err)
	}
	return 0
}

func MigrateMain(ctx context.Context, databaseDSN string) int {
	if err := RunMigrations(ctx, databaseDSN); err != nil {
		return logProcessError("migration error", err)
	}
	return 0
}

func logProcessError(message string, err error) int {
	graph, graphErr := NewProcessLoggingGraph(os.Stderr)
	if graphErr == nil {
		graph.Logger.Error(message, "error", err)
	}
	return 1
}

func envOr(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
