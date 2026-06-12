package app

import (
	"net/http"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/config"
)

func TestNewServerUsesConfigAndTransportRouter(t *testing.T) {
	cfg := config.Config{
		GatewayAddr:           "127.0.0.1:0",
		HTTPReadHeaderTimeout: time.Second,
		HTTPReadTimeout:       2 * time.Second,
		HTTPWriteTimeout:      3 * time.Second,
		HTTPIdleTimeout:       4 * time.Second,
	}
	server := NewServer(cfg)

	if server.Addr != cfg.GatewayAddr {
		t.Fatalf("Addr = %q", server.Addr)
	}
	if server.Handler == nil {
		t.Fatal("Handler is nil")
	}
	if server.ReadHeaderTimeout != cfg.HTTPReadHeaderTimeout || server.ReadTimeout != cfg.HTTPReadTimeout || server.WriteTimeout != cfg.HTTPWriteTimeout || server.IdleTimeout != cfg.HTTPIdleTimeout {
		t.Fatalf("timeouts not copied: %#v", server)
	}
	_ = http.Handler(server.Handler)
}
