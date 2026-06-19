package app

import (
	"fmt"
	"io"
	"log"
	"log/slog"

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
