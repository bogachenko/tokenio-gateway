package logging

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

const redacted = "[REDACTED]"

type Options struct {
	Environment string
	Level       string
	Format      string
	LogBodies   bool
	Output      io.Writer
}

type Redactor struct{}

func New(options Options) (*slog.Logger, Redactor, error) {
	level, err := parseLevel(options.Level)
	if err != nil {
		return nil, Redactor{}, err
	}
	if options.Format != "text" && options.Format != "json" {
		return nil, Redactor{}, ErrInvalidLogFormat
	}
	if options.Environment == "production" && options.LogBodies {
		return nil, Redactor{}, ErrBodyLoggingForbidden
	}

	output := options.Output
	if output == nil {
		output = io.Discard
	}

	redactor := Redactor{}
	handlerOptions := &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactor.Attr,
	}

	var handler slog.Handler
	if options.Format == "json" {
		handler = slog.NewJSONHandler(output, handlerOptions)
	} else {
		handler = slog.NewTextHandler(output, handlerOptions)
	}

	return slog.New(handler), redactor, nil
}

func parseLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, ErrInvalidLogLevel
	}
}

func (Redactor) Attr(_ []string, attr slog.Attr) slog.Attr {
	if shouldRedactKey(attr.Key) {
		return slog.String(attr.Key, redacted)
	}
	if attr.Value.Kind() == slog.KindString {
		value := attr.Value.String()
		if looksSensitive(value) {
			return slog.String(attr.Key, redacted)
		}
	}
	return attr
}

func (r Redactor) String(value string) string {
	if looksSensitive(value) {
		return redacted
	}
	return value
}

func (r Redactor) Header(headers http.Header) http.Header {
	copy := make(http.Header, len(headers))
	for key, values := range headers {
		if shouldRedactKey(key) {
			copy[key] = []string{redacted}
			continue
		}
		copy[key] = append([]string(nil), values...)
	}
	return copy
}

func (Redactor) DSN(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	username := parsed.User.Username()
	if _, ok := parsed.User.Password(); ok {
		parsed.User = url.UserPassword(username, redacted)
	}
	return parsed.String()
}

func shouldRedactKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "_", "-"))
	switch normalized {
	case "authorization", "x-api-key", "x-goog-api-key":
		return true
	}
	return strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "jwt") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "credential") ||
		strings.Contains(normalized, "api-key") ||
		strings.Contains(normalized, "dsn") ||
		strings.Contains(normalized, "provisioning")
}

func looksSensitive(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "bearer ") ||
		strings.HasPrefix(lower, "basic ") ||
		strings.Contains(lower, "://") && strings.Contains(lower, "@") ||
		strings.Count(value, ".") == 2 && len(value) > 20
}
