package app

import (
	"net/http"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/config"
)

type appTestHandler struct{}

func (*appTestHandler) ServeHTTP(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	writer.WriteHeader(http.StatusNoContent)
}

func TestNewServerUsesConfigAndExactHandler(t *testing.T) {
	cfg := config.Config{
		GatewayAddr:           "127.0.0.1:0",
		HTTPReadHeaderTimeout: time.Second,
		HTTPReadTimeout:       2 * time.Second,
		HTTPWriteTimeout:      3 * time.Second,
		HTTPIdleTimeout:       4 * time.Second,
	}
	handler := &appTestHandler{}

	server := NewServer(cfg, handler)

	if server.Addr != cfg.GatewayAddr {
		t.Fatalf("Addr = %q", server.Addr)
	}
	if server.Handler != handler {
		t.Fatal("server did not preserve exact composed handler")
	}
	if server.ReadHeaderTimeout != cfg.HTTPReadHeaderTimeout ||
		server.ReadTimeout != cfg.HTTPReadTimeout ||
		server.WriteTimeout != cfg.HTTPWriteTimeout ||
		server.IdleTimeout != cfg.HTTPIdleTimeout {
		t.Fatalf("timeouts not copied: %#v", server)
	}
}

func TestNewServerRejectsNilHandler(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected nil handler panic")
		}
	}()
	NewServer(config.Config{}, nil)
}
