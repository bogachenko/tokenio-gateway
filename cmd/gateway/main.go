package main

import (
	"log"
	"net/http"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	"github.com/bogachenko/tokenio-gateway/internal/httpapi"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpapi.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed", "")
			return
		}
		_, _ = w.Write([]byte("OK"))
	})

	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpapi.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed", "")
			return
		}
		httpapi.WriteJSON(w, http.StatusNotImplemented, map[string]any{
			"error": map[string]any{
				"code":    "not_implemented",
				"message": "model registry is not wired yet",
			},
		})
	})

	server := &http.Server{
		Addr:              cfg.GatewayAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("tokenio-gateway listening on %s", cfg.GatewayAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
