package provisioninghttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	application "github.com/bogachenko/tokenio-gateway/internal/application/provisioning"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type routerAuthenticator struct {
	token string
}

func (a routerAuthenticator) Authenticate(token string) error {
	if token != a.token {
		return errors.New("access denied")
	}
	return nil
}

type routerIDs struct {
	id  string
	err error
}

func (g routerIDs) NewLocalRequestID() (string, error) {
	return "", errors.New("unexpected local request ID")
}

func (g routerIDs) NewAdminRequestID() (string, error) {
	return "", errors.New("unexpected admin request ID")
}

func (g routerIDs) NewProvisioningRequestID() (string, error) {
	return g.id, g.err
}

type routerService struct {
	provision func(
		context.Context,
		application.ProvisionInput,
	) (application.ProvisionResult, error)
	confirm func(
		context.Context,
		string,
	) (application.ConfirmDeliveryResult, error)

	provisionCalls int
	confirmCalls   int
	lastInput      application.ProvisionInput
	lastID         string
}

func (s *routerService) Provision(
	ctx context.Context,
	input application.ProvisionInput,
) (application.ProvisionResult, error) {
	s.provisionCalls++
	s.lastInput = input
	if s.provision == nil {
		return application.ProvisionResult{}, nil
	}
	return s.provision(ctx, input)
}

func (s *routerService) ConfirmDelivery(
	ctx context.Context,
	id string,
) (application.ConfirmDeliveryResult, error) {
	s.confirmCalls++
	s.lastID = id
	if s.confirm == nil {
		return application.ConfirmDeliveryResult{}, nil
	}
	return s.confirm(ctx, id)
}

func newTestRouter(
	t *testing.T,
	service *routerService,
) *Router {
	t.Helper()

	router, err := NewRouter(
		service,
		routerAuthenticator{token: "service-token"},
		routerIDs{id: "provreq_test"},
		1024,
	)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return router
}

func validProvisionRequest() *http.Request {
	request := httptest.NewRequest(
		http.MethodPost,
		basePath,
		strings.NewReader(
			`{"external_billing_user_id":"billing_user_1",`+
				`"source_reference":"payment_1"}`,
		),
	)
	request.Header.Set(serviceTokenHeader, "service-token")
	request.Header.Set(idempotencyHeader, "payment-order-1")
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestProvisionEndpoint(t *testing.T) {
	expiresAt := time.Date(
		2026,
		time.June,
		14,
		12,
		0,
		0,
		0,
		time.UTC,
	)
	service := &routerService{
		provision: func(
			_ context.Context,
			input application.ProvisionInput,
		) (application.ProvisionResult, error) {
			return application.ProvisionResult{
				Result:             application.ResultTypeCreated,
				ProvisioningID:     "prov_1",
				ProvisioningStatus: domain.APIKeyProvisioningStatusPendingDelivery,
				APIKeyID:           "ak_1",
				APIKey:             "sk_live_secret",
				KeyPrefix:          "sk_live_abcd...",
				ExpiresAt:          &expiresAt,
			}, nil
		},
	}
	router := newTestRouter(t, service)
	request := validProvisionRequest()
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, body = %s",
			response.Code,
			response.Body.String(),
		)
	}
	if service.lastInput.IdempotencyKey !=
		"payment-order-1" ||
		service.lastInput.ExternalBillingUserID !=
			"billing_user_1" ||
		service.lastInput.SourceReference != "payment_1" {
		t.Fatalf("input = %+v", service.lastInput)
	}

	var envelope struct {
		Data provisionResponse `json:"data"`
	}
	if err := json.Unmarshal(
		response.Body.Bytes(),
		&envelope,
	); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.APIKey != "sk_live_secret" ||
		envelope.Data.ProvisioningID != "prov_1" {
		t.Fatalf("response = %+v", envelope.Data)
	}
}

func TestProvisionResponseOmitsRawKeyWhenUnavailable(
	t *testing.T,
) {
	service := &routerService{
		provision: func(
			context.Context,
			application.ProvisionInput,
		) (application.ProvisionResult, error) {
			return application.ProvisionResult{
				Result:             application.ResultTypeAlreadyProvisioned,
				ProvisioningID:     "prov_1",
				ProvisioningStatus: domain.APIKeyProvisioningStatusDelivered,
				APIKeyID:           "ak_existing",
				KeyPrefix:          "sk_live_abcd...",
			}, nil
		},
	}
	response := httptest.NewRecorder()
	newTestRouter(t, service).ServeHTTP(
		response,
		validProvisionRequest(),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if strings.Contains(
		response.Body.String(),
		`"api_key"`,
	) {
		t.Fatalf(
			"raw API-key field was returned: %s",
			response.Body.String(),
		)
	}
}

func TestConfirmDeliveryEndpoint(t *testing.T) {
	deliveredAt := time.Date(
		2026,
		time.June,
		13,
		12,
		1,
		0,
		0,
		time.UTC,
	)
	service := &routerService{
		confirm: func(
			_ context.Context,
			id string,
		) (application.ConfirmDeliveryResult, error) {
			return application.ConfirmDeliveryResult{
				ProvisioningID: id,
				Status:         domain.APIKeyProvisioningStatusDelivered,
				DeliveredAt:    &deliveredAt,
			}, nil
		},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		basePath+"/prov_1/confirm-delivery",
		nil,
	)
	request.Header.Set(serviceTokenHeader, "service-token")
	response := httptest.NewRecorder()

	newTestRouter(t, service).ServeHTTP(response, request)

	if response.Code != http.StatusOK ||
		service.confirmCalls != 1 ||
		service.lastID != "prov_1" {
		t.Fatalf(
			"status=%d calls=%d id=%q body=%s",
			response.Code,
			service.confirmCalls,
			service.lastID,
			response.Body.String(),
		)
	}
}

func TestAuthenticationAndValidationStopBeforeService(
	t *testing.T,
) {
	tests := []struct {
		name       string
		mutate     func(*http.Request)
		wantStatus int
		wantCode   domain.ErrorCode
	}{
		{
			name: "missing service token",
			mutate: func(request *http.Request) {
				request.Header.Del(serviceTokenHeader)
			},
			wantStatus: http.StatusUnauthorized,
			wantCode:   domain.ErrorCodeProvisioningUnauthorized,
		},
		{
			name: "missing idempotency",
			mutate: func(request *http.Request) {
				request.Header.Del(idempotencyHeader)
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   domain.ErrorCodeProvisioningInvalidRequest,
		},
		{
			name: "invalid content type",
			mutate: func(request *http.Request) {
				request.Header.Set(
					"Content-Type",
					"text/plain",
				)
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   domain.ErrorCodeProvisioningInvalidRequest,
		},
		{
			name: "unknown JSON field",
			mutate: func(request *http.Request) {
				request.Body = http.NoBody
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   domain.ErrorCodeProvisioningInvalidRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &routerService{}
			request := validProvisionRequest()
			test.mutate(request)
			response := httptest.NewRecorder()

			newTestRouter(t, service).ServeHTTP(
				response,
				request,
			)

			assertError(
				t,
				response,
				test.wantStatus,
				test.wantCode,
			)
			if service.provisionCalls != 0 {
				t.Fatal("invalid request reached service")
			}
		})
	}
}

func TestProvisioningApplicationErrorMapping(
	t *testing.T,
) {
	tests := []struct {
		err        error
		wantStatus int
		wantCode   domain.ErrorCode
	}{
		{
			err:        application.ErrInvalidRequest,
			wantStatus: http.StatusBadRequest,
			wantCode:   domain.ErrorCodeProvisioningInvalidRequest,
		},
		{
			err:        application.ErrConflict,
			wantStatus: http.StatusConflict,
			wantCode:   domain.ErrorCodeProvisioningConflict,
		},
		{
			err:        application.ErrExpired,
			wantStatus: http.StatusGone,
			wantCode:   domain.ErrorCodeProvisioningExpired,
		},
		{
			err:        application.ErrStoreUnavailable,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   domain.ErrorCodeProvisioningStoreUnavailable,
		},
		{
			err:        application.ErrCryptoUnavailable,
			wantStatus: http.StatusInternalServerError,
			wantCode:   domain.ErrorCodeProvisioningCryptoUnavailable,
		},
		{
			err:        application.ErrInternal,
			wantStatus: http.StatusInternalServerError,
			wantCode:   domain.ErrorCodeInternalError,
		},
	}

	for _, test := range tests {
		service := &routerService{
			provision: func(
				context.Context,
				application.ProvisionInput,
			) (application.ProvisionResult, error) {
				return application.ProvisionResult{},
					test.err
			},
		}
		response := httptest.NewRecorder()
		newTestRouter(t, service).ServeHTTP(
			response,
			validProvisionRequest(),
		)
		assertError(
			t,
			response,
			test.wantStatus,
			test.wantCode,
		)
	}
}

func assertError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantStatus int,
	wantCode domain.ErrorCode,
) {
	t.Helper()

	if response.Code != wantStatus {
		t.Fatalf(
			"status = %d, want %d; body = %s",
			response.Code,
			wantStatus,
			response.Body.String(),
		)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(
		response.Body.Bytes(),
		&envelope,
	); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if envelope.Error.Code != wantCode {
		t.Fatalf(
			"code = %q, want %q",
			envelope.Error.Code,
			wantCode,
		)
	}
	if envelope.Error.RequestID != "provreq_test" {
		t.Fatalf(
			"request_id = %q",
			envelope.Error.RequestID,
		)
	}
}

func TestProvisioningUnknownPathInsideNamespaceIncludesRequestID(
	t *testing.T,
) {
	router := newTestRouter(t, &routerService{})
	request := httptest.NewRequest(
		http.MethodGet,
		basePath+"/unknown",
		nil,
	)
	request.Header.Set(serviceTokenHeader, "service-token")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf(
			"status = %d, want %d; body = %s",
			response.Code,
			http.StatusNotFound,
			response.Body.String(),
		)
	}
	if !strings.Contains(
		response.Body.String(),
		`"request_id":"provreq_test"`,
	) {
		t.Fatalf(
			"request ID missing from body: %s",
			response.Body.String(),
		)
	}
}
