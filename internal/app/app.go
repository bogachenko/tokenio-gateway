package app

import (
	"errors"
	"log"
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	"github.com/bogachenko/tokenio-gateway/internal/transport/httptransport"
)

func NewServer(cfg config.Config) *http.Server {
	return &http.Server{
		Addr:              cfg.GatewayAddr,
		Handler:           httptransport.NewRouter(),
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}
}

func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	server := NewServer(cfg)
	log.Printf("tokenio-gateway listening on %s", cfg.GatewayAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
