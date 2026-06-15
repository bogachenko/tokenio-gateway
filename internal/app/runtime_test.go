package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/postgres"
)

func TestNewRepositoryGraphRejectsNilDatabase(t *testing.T) {
	_, err := NewRepositoryGraph(
		nil,
		applicationGraphClock{now: time.Now().UTC()},
	)
	if !errors.Is(
		err,
		postgres.ErrInvalidDatabaseConfig,
	) {
		t.Fatalf(
			"error = %v, want invalid database config",
			err,
		)
	}
}

func TestRepositoryGraphValidateRejectsMissingCapability(
	t *testing.T,
) {
	var graph RepositoryGraph
	if err := graph.Validate(); err == nil {
		t.Fatal(
			"expected incomplete graph validation error",
		)
	}
}

func TestNewRuntimePrimitives(t *testing.T) {
	primitives, err := NewRuntimePrimitives()
	if err != nil {
		t.Fatalf("NewRuntimePrimitives: %v", err)
	}
	if err := primitives.Validate(); err != nil {
		t.Fatalf("runtime primitives: %v", err)
	}
}

func TestRuntimePrimitivesValidateRejectsMissingDependency(
	t *testing.T,
) {
	var primitives RuntimePrimitives
	if err := primitives.Validate(); err == nil {
		t.Fatal(
			"expected incomplete primitives validation error",
		)
	}
}

type runtimeTestHandler struct{}

func (*runtimeTestHandler) ServeHTTP(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	writer.WriteHeader(http.StatusNoContent)
}

func TestNewServerWithHandlerUsesExactHandler(
	t *testing.T,
) {
	handler := &runtimeTestHandler{}
	cfg := config.Config{
		GatewayAddr:           "127.0.0.1:0",
		HTTPReadHeaderTimeout: time.Second,
		HTTPReadTimeout:       2 * time.Second,
		HTTPWriteTimeout:      3 * time.Second,
		HTTPIdleTimeout:       4 * time.Second,
	}

	server := NewServer(cfg, handler)
	if server.Handler == nil {
		t.Fatal("Handler is nil")
	}
	if server.Handler != handler {
		t.Fatal(
			"server did not preserve exact composed handler",
		)
	}
}

func TestNewRuntimeIntegration(t *testing.T) {
	dsn := os.Getenv("TOKENIO_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip(
			"TOKENIO_TEST_DATABASE_DSN is not set",
		)
	}

	cfg := config.Config{
		DatabaseDSN:              dsn,
		AdminToken:               "integration-admin-token",
		APIKeyHashSecret:         "integration-api-key-hash-secret",
		ProvisioningServiceToken: "integration-provisioning-service-token",
		APIKeyProvisioningEncryptionKey: bytes.Repeat(
			[]byte{0x42},
			32,
		),
		APIKeyProvisioningKeyVersion:           "v1",
		APIKeyProvisioningTTL:                  24 * time.Hour,
		APIKeyProvisioningExpirationInterval:   time.Minute,
		APIKeyProvisioningExpirationBatchSize:  100,
		ForwardingAttemptRecoveryStaleAfter:    5 * time.Minute,
		ForwardingAttemptRecoveryInterval:      time.Minute,
		ForwardingAttemptRecoveryBatchSize:     100,
		TelegramStaleAttemptRecoveryStaleAfter: 5 * time.Minute,
		TelegramStaleAttemptRecoveryInterval:   time.Minute,
		TelegramStaleAttemptRecoveryBatchSize:  100,
		BillingBaseURL:                         "https://billing.example",
		BillingServiceToken:                    "integration-billing-service-token",
		BillingJWTSigningKey:                   "integration-billing-jwt-signing-key",
		BillingJWTTTL:                          15 * time.Minute,
		BillingTimeout:                         30 * time.Second,
		CostCurrency:                           "RUB",
		AutoChargeThresholdCents:               1000,
		MinChargeAmountCents:                   100,
		RequestBodyMaxBytes:                    1024,
		GatewayAddr:                            "127.0.0.1:0",
		HTTPReadHeaderTimeout:                  time.Second,
		HTTPReadTimeout:                        2 * time.Second,
		HTTPWriteTimeout:                       3 * time.Second,
		HTTPIdleTimeout:                        4 * time.Second,
		HTTPShutdownTimeout:                    5 * time.Second,
	}

	observer, err :=
		NewProvisioningExpirationLogObserver(
			log.New(io.Discard, "", 0),
		)
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := NewRuntime(
		t.Context(),
		cfg,
		observer,
	)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(runtime.Close)

	if err := runtime.Primitives.Validate(); err != nil {
		t.Fatalf("runtime primitives: %v", err)
	}
	if err := runtime.Security.Validate(); err != nil {
		t.Fatalf("security graph: %v", err)
	}
	if !runtime.Security.ProvisioningEnabled ||
		runtime.Security.ProvisioningAuthenticator ==
			nil {
		t.Fatal(
			"runtime provisioning authenticator is disabled",
		)
	}
	if err := runtime.Provisioning.Validate(); err != nil {
		t.Fatalf(
			"provisioning infrastructure graph: %v",
			err,
		)
	}
	if !runtime.Provisioning.Enabled {
		t.Fatal(
			"runtime provisioning graph is disabled",
		)
	}
	if err := runtime.Billing.Validate(); err != nil {
		t.Fatalf(
			"billing infrastructure graph: %v",
			err,
		)
	}
	if err := runtime.Forwarding.Validate(); err != nil {
		t.Fatalf(
			"forwarding infrastructure graph: %v",
			err,
		)
	}
	if err := runtime.Repositories.Validate(); err != nil {
		t.Fatalf("repository graph: %v", err)
	}
	if err := runtime.Applications.Validate(); err != nil {
		t.Fatalf("application graph: %v", err)
	}
	if runtime.Applications.ModelCatalog == nil {
		t.Fatal("runtime model catalog service is nil")
	}
	if err := runtime.Workers.Validate(); err != nil {
		t.Fatalf("worker graph: %v", err)
	}
	if !runtime.Workers.
		ProvisioningExpirationEnabled ||
		runtime.Workers.
			ProvisioningExpiration == nil {
		t.Fatal(
			"runtime provisioning expiration worker is disabled",
		)
	}
	if err := runtime.Transports.Validate(); err != nil {
		t.Fatalf("transport graph: %v", err)
	}
	if !runtime.Transports.ProvisioningEnabled ||
		runtime.Transports.Provisioning == nil {
		t.Fatal(
			"runtime provisioning transport is disabled",
		)
	}
	if runtime.Handler == nil {
		t.Fatal("runtime handler is nil")
	}
	if runtime.Handler != runtime.Transports.Root {
		t.Fatal(
			"runtime handler is not transport graph root",
		)
	}
	if err := runtime.Ping(
		context.Background(),
	); err != nil {
		t.Fatalf("runtime ping: %v", err)
	}

	runtime.Close()
	runtime.Close()
}
