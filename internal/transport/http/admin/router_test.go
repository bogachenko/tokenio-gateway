package adminhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	application "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type testIDs struct {
	mu     sync.Mutex
	events []string
	value  string
	err    error
}

func (f *testIDs) NewLocalRequestID() (string, error) {
	return "llmreq_unused", nil
}
func (f *testIDs) NewProvisioningRequestID() (string, error) {
	return "provreq_unused", nil
}
func (f *testIDs) NewAdminRequestID() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "request_id")
	return f.value, f.err
}

type testAuthenticator struct {
	mu      sync.Mutex
	events  *[]string
	subject string
	err     error
}

func (f *testAuthenticator) Authenticate(string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	*f.events = append(*f.events, "auth")
	return f.subject, f.err
}

type testService struct {
	Service
	mu                  sync.Mutex
	listInput           application.UserListInput
	listCalls           int
	listErr             error
	updateResellerInput application.UpdateResellerInput
	telegramInput       application.TelegramAlertListInput
	telegramCalls       int
	telegramRetryCalls  int
	telegramRetryID     string
	telegramRetryCmd    application.CommandContext
	retryCalls          int
	retryCommand        application.CommandContext
	retryBatchID        string
	retryErr            error
}

func (f *testService) ListUsers(_ context.Context, input application.UserListInput) (application.ListResult[domain.User], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	f.listInput = input
	return application.ListResult[domain.User]{Data: []domain.User{}, Pagination: application.Pagination{Limit: input.Limit, Offset: input.Offset, Total: 0}}, f.listErr
}
func (f *testService) UpdateReseller(_ context.Context, _ application.CommandContext, input application.UpdateResellerInput) (application.ResellerView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateResellerInput = input
	return application.ResellerView{ID: input.ID}, nil
}

func (f *testService) ListTelegramAlerts(
	_ context.Context,
	input application.TelegramAlertListInput,
) (application.ListResult[application.TelegramAlertView], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.telegramCalls++
	f.telegramInput = input
	return application.ListResult[application.TelegramAlertView]{
		Data: []application.TelegramAlertView{{ID: "tgalt_1"}},
		Pagination: application.Pagination{
			Limit:  input.Limit,
			Offset: input.Offset,
			Total:  1,
		},
	}, nil
}

func (f *testService) RetryTelegramAlert(
	_ context.Context,
	command application.CommandContext,
	alertID string,
) (application.TelegramAlertView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.telegramRetryCalls++
	f.telegramRetryID = alertID
	f.telegramRetryCmd = command
	return application.TelegramAlertView{ID: alertID}, nil
}

func (f *testService) RetryFailedBillingChargeBatch(
	_ context.Context,
	command application.CommandContext,
	batchID string,
) (domain.BillingChargeBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retryCalls++
	f.retryCommand = command
	f.retryBatchID = batchID
	return domain.BillingChargeBatch{ID: batchID}, f.retryErr
}

func TestAdminRequestIDIsCreatedBeforeAuthentication(t *testing.T) {
	events := []string{}
	ids := &testIDs{events: events, value: "admreq_1"}
	authenticator := &testAuthenticator{events: &ids.events, subject: "admin_token"}
	service := &testService{}
	router, err := NewRouter(service, authenticator, ids)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
	req.Header.Set("Authorization", "Bearer admin")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	ids.mu.Lock()
	got := append([]string(nil), ids.events...)
	ids.mu.Unlock()
	if len(got) != 2 || got[0] != "request_id" || got[1] != "auth" {
		t.Fatalf("events=%v", got)
	}
	if res.Header().Get(adminRequestIDHeader) != "admreq_1" {
		t.Fatalf("headers=%v", res.Header())
	}
}

func TestAdminRequestIDFailureDoesNotUseSyntheticFallback(t *testing.T) {
	tests := []struct {
		name string
		ids  *testIDs
	}{
		{
			name: "generator error",
			ids: &testIDs{
				err: errors.New("entropy unavailable"),
			},
		},
		{
			name: "invalid generated value",
			ids: &testIDs{
				value: "invalid",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := []string{}
			authenticator := &testAuthenticator{
				events:  &events,
				subject: "admin_token",
			}
			router, err := NewRouter(
				&testService{},
				authenticator,
				test.ids,
			)
			if err != nil {
				t.Fatal(err)
			}

			res := httptest.NewRecorder()
			router.ServeHTTP(
				res,
				httptest.NewRequest(
					http.MethodGet,
					"/admin/v1/users",
					nil,
				),
			)

			if res.Code != http.StatusInternalServerError {
				t.Fatalf(
					"status = %d, body = %s",
					res.Code,
					res.Body.String(),
				)
			}
			if got := res.Header().Get(adminRequestIDHeader); got != "" {
				t.Fatalf("request ID header = %q, want empty", got)
			}
			if strings.Contains(res.Body.String(), "request_id") {
				t.Fatalf(
					"body must omit unavailable request ID: %s",
					res.Body.String(),
				)
			}
			if strings.Contains(
				res.Body.String(),
				"admreq_invalid_generator",
			) {
				t.Fatalf(
					"body contains synthetic fallback: %s",
					res.Body.String(),
				)
			}
			if len(events) != 0 {
				t.Fatalf(
					"authentication ran after ID failure: %v",
					events,
				)
			}
		})
	}
}

func TestMissingAndInvalidAdminAuthorizationUseSeparatedErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		authErr error
		status  int
		code    string
	}{
		{name: "missing", authErr: auth.ErrAdminAuthorizationRequired, status: http.StatusUnauthorized, code: "admin_unauthorized"},
		{name: "invalid", authErr: auth.ErrAdminAccessDenied, status: http.StatusForbidden, code: "admin_forbidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := []string{}
			ids := &testIDs{value: "admreq_auth"}
			router, _ := NewRouter(&testService{}, &testAuthenticator{events: &events, err: tc.authErr}, ids)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil))
			if res.Code != tc.status || !strings.Contains(res.Body.String(), tc.code) {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			if res.Header().Get(adminRequestIDHeader) != "admreq_auth" {
				t.Fatalf("headers=%v", res.Header())
			}
		})
	}
}

func TestSuccessListEnvelopeAndPaginationDefaults(t *testing.T) {
	events := []string{}
	service := &testService{}
	router, _ := NewRouter(service, &testAuthenticator{events: &events, subject: "admin_token"}, &testIDs{value: "admreq_page"})
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
	req.Header.Set("Authorization", "Bearer admin")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var body struct {
		Data       []domain.User          `json:"data"`
		Pagination application.Pagination `json:"pagination"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data == nil || body.Pagination.Limit != 50 || body.Pagination.Offset != 0 {
		t.Fatalf("body=%+v", body)
	}
	service.mu.Lock()
	input := service.listInput
	service.mu.Unlock()
	if input.Limit != 50 || input.Offset != 0 {
		t.Fatalf("input=%+v", input)
	}
}

func TestPaginationRejectsValuesAboveMaximumBeforeUseCase(t *testing.T) {
	events := []string{}
	service := &testService{}
	router, _ := NewRouter(service, &testAuthenticator{events: &events, subject: "admin_token"}, &testIDs{value: "admreq_page"})
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/users?limit=501", nil)
	req.Header.Set("Authorization", "Bearer admin")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "admin_validation_error") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	service.mu.Lock()
	calls := service.listCalls
	service.mu.Unlock()
	if calls != 0 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestPatchDistinguishesAbsentFromExplicitFalseAndZero(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		present    bool
	}{
		{name: "absent", body: `{}`, present: false},
		{name: "explicit", body: `{"enabled":false,"minimum_balance_cents":0}`, present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := []string{}
			service := &testService{}
			router, _ := NewRouter(service, &testAuthenticator{events: &events, subject: "admin_token"}, &testIDs{value: "admreq_patch"})
			req := httptest.NewRequest(http.MethodPatch, "/admin/v1/resellers/r1", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer admin")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			service.mu.Lock()
			input := service.updateResellerInput
			service.mu.Unlock()
			if tc.present {
				if input.Enabled == nil || *input.Enabled || input.MinimumBalanceCents == nil || *input.MinimumBalanceCents != 0 {
					t.Fatalf("input=%+v", input)
				}
			} else if input.Enabled != nil || input.MinimumBalanceCents != nil {
				t.Fatalf("input=%+v", input)
			}
		})
	}
}

func TestAdminChargeBatchRetryDispatchesCommandContext(t *testing.T) {
	t.Run("post dispatches once", func(t *testing.T) {
		events := []string{}
		service := &testService{}
		router, err := NewRouter(
			service,
			&testAuthenticator{
				events:  &events,
				subject: "admin_token",
			},
			&testIDs{value: "admreq_retry_http"},
		)
		if err != nil {
			t.Fatal(err)
		}

		request := httptest.NewRequest(
			http.MethodPost,
			"/admin/v1/billing-charge-batches/batch_1/retry",
			nil,
		)
		request.Header.Set("Authorization", "Bearer admin")
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf(
				"status=%d body=%s",
				response.Code,
				response.Body.String(),
			)
		}
		if got := response.Header().Get(adminRequestIDHeader); got != "admreq_retry_http" {
			t.Fatalf("request id header=%q", got)
		}

		service.mu.Lock()
		defer service.mu.Unlock()
		if service.retryCalls != 1 {
			t.Fatalf("retry calls=%d, want 1", service.retryCalls)
		}
		if service.retryBatchID != "batch_1" {
			t.Fatalf("retry batch id=%q", service.retryBatchID)
		}
		if service.retryCommand.RequestID != "admreq_retry_http" ||
			service.retryCommand.AdminSubject != "admin_token" {
			t.Fatalf("retry command=%+v", service.retryCommand)
		}
	})

	t.Run("wrong method stops before service", func(t *testing.T) {
		events := []string{}
		service := &testService{}
		router, err := NewRouter(
			service,
			&testAuthenticator{
				events:  &events,
				subject: "admin_token",
			},
			&testIDs{value: "admreq_retry_method"},
		)
		if err != nil {
			t.Fatal(err)
		}

		request := httptest.NewRequest(
			http.MethodGet,
			"/admin/v1/billing-charge-batches/batch_1/retry",
			nil,
		)
		request.Header.Set("Authorization", "Bearer admin")
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf(
				"status=%d body=%s",
				response.Code,
				response.Body.String(),
			)
		}

		service.mu.Lock()
		defer service.mu.Unlock()
		if service.retryCalls != 0 {
			t.Fatalf("retry calls=%d, want 0", service.retryCalls)
		}
	})
}

func TestAdminEndpointsAreNotRegisteredUnderPublicV1(t *testing.T) {
	events := []string{}
	service := &testService{}
	ids := &testIDs{value: "admreq_public"}
	router, _ := NewRouter(service, &testAuthenticator{events: &events, subject: "admin_token"}, ids)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/v1/users", nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	service.mu.Lock()
	calls := service.listCalls
	service.mu.Unlock()
	if calls != 0 {
		t.Fatalf("calls=%d", calls)
	}
	ids.mu.Lock()
	idEvents := len(ids.events)
	ids.mu.Unlock()
	if idEvents != 0 {
		t.Fatalf("admin middleware ran for public path")
	}
}

func TestUnderlyingErrorsAreNeverReturned(t *testing.T) {
	events := []string{}
	service := &testService{listErr: errors.New("SELECT secret FROM credentials")}
	router, _ := NewRouter(service, &testAuthenticator{events: &events, subject: "admin_token"}, &testIDs{value: "admreq_error"})
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
	req.Header.Set("Authorization", "Bearer admin")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusInternalServerError || strings.Contains(res.Body.String(), "SELECT") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestAdminApplicationErrorMappingUsesNormalizedContract(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   domain.ErrorCode
	}{
		{"invalid", application.ErrInvalidRequest, http.StatusBadRequest, domain.ErrorCodeAdminValidationError},
		{"not found", application.ErrNotFound, http.StatusNotFound, domain.ErrorCodeAdminNotFound},
		{"conflict", application.ErrConflict, http.StatusConflict, domain.ErrorCodeAdminConflict},
		{"state conflict", application.ErrStateConflict, http.StatusConflict, domain.ErrorCodeAdminStateConflict},
		{"secret", application.ErrSecretNotAvailable, http.StatusConflict, domain.ErrorCodeAdminSecretNotAvailable},
		{"store", application.ErrStoreUnavailable, http.StatusServiceUnavailable, domain.ErrorCodeStoreUnavailable},
		{"internal", application.ErrInternal, http.StatusInternalServerError, domain.ErrorCodeInternalError},
		{"plain", errors.New("postgres secret"), http.StatusInternalServerError, domain.ErrorCodeInternalError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeApplicationError(response, "admreq_mapping", test.err)
			if response.Code != test.status {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					response.Code,
					test.status,
					response.Body.String(),
				)
			}
			if !strings.Contains(response.Body.String(), string(test.code)) {
				t.Fatalf(
					"body = %s, want code %q",
					response.Body.String(),
					test.code,
				)
			}
			if strings.Contains(response.Body.String(), "postgres secret") {
				t.Fatalf("raw error leaked: %s", response.Body.String())
			}
		})
	}
}

func TestAdminTransportValidationDoesNotRequireApplicationError(t *testing.T) {
	response := httptest.NewRecorder()
	writeAdminValidationError(response, "admreq_transport")

	if response.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d; body = %s",
			response.Code,
			http.StatusBadRequest,
			response.Body.String(),
		)
	}
	if !strings.Contains(
		response.Body.String(),
		string(domain.ErrorCodeAdminValidationError),
	) {
		t.Fatalf(
			"body = %s, want code %q",
			response.Body.String(),
			domain.ErrorCodeAdminValidationError,
		)
	}
	if !strings.Contains(response.Body.String(), "admreq_transport") {
		t.Fatalf("request ID missing: %s", response.Body.String())
	}
}

func TestAdminUnknownPathInsideNamespaceIncludesRequestID(t *testing.T) {
	events := []string{}
	ids := &testIDs{events: events, value: "admreq_unknown"}
	authenticator := &testAuthenticator{
		events:  &ids.events,
		subject: "admin_token",
	}
	router, err := NewRouter(&testService{}, authenticator, ids)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		basePath+"/unknown",
		nil,
	)
	request.Header.Set("Authorization", "Bearer admin")
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
	if response.Header().Get(adminRequestIDHeader) != "admreq_unknown" {
		t.Fatalf("headers = %v", response.Header())
	}
	if !strings.Contains(
		response.Body.String(),
		`"request_id":"admreq_unknown"`,
	) {
		t.Fatalf("request ID missing from body: %s", response.Body.String())
	}
}

func TestAdminTelegramAlertsListDispatchesFilters(t *testing.T) {
	events := []string{}
	service := &testService{}
	router, err := NewRouter(
		service,
		&testAuthenticator{events: &events, subject: "admin_token"},
		&testIDs{value: "admreq_tg_alerts"},
	)
	if err != nil {
		t.Fatal(err)
	}
	from := "2026-06-18T10:00:00Z"
	to := "2026-06-18T11:00:00Z"
	request := httptest.NewRequest(
		http.MethodGet,
		"/admin/v1/telegram-alerts?alert_type=reseller_balance_low&reseller_id=reseller_1&status=failed&created_from="+from+"&created_to="+to+"&limit=25&offset=50",
		nil,
	)
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get(adminRequestIDHeader) != "admreq_tg_alerts" {
		t.Fatalf("headers=%v", response.Header())
	}
	service.mu.Lock()
	calls := service.telegramCalls
	input := service.telegramInput
	service.mu.Unlock()
	if calls != 1 {
		t.Fatalf("telegram calls=%d, want 1", calls)
	}
	createdFrom, err := time.Parse(time.RFC3339, from)
	if err != nil {
		t.Fatal(err)
	}
	createdTo, err := time.Parse(time.RFC3339, to)
	if err != nil {
		t.Fatal(err)
	}
	if input.AlertType != "reseller_balance_low" ||
		input.ResellerID != "reseller_1" ||
		input.Status != domain.TelegramAlertStatusFailed ||
		input.CreatedFrom == nil ||
		!input.CreatedFrom.Equal(createdFrom) ||
		input.CreatedTo == nil ||
		!input.CreatedTo.Equal(createdTo) ||
		input.Limit != 25 ||
		input.Offset != 50 {
		t.Fatalf("input=%+v", input)
	}
	if !strings.Contains(response.Body.String(), `"id":"tgalt_1"`) {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestAdminTelegramAlertRetryDispatchesCommandContext(t *testing.T) {
	events := []string{}
	service := &testService{}
	router, err := NewRouter(
		service,
		&testAuthenticator{events: &events, subject: "admin_token"},
		&testIDs{value: "admreq_tg_retry"},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/admin/v1/telegram-alerts/tgalt_1/retry",
		nil,
	)
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	service.mu.Lock()
	calls := service.telegramRetryCalls
	alertID := service.telegramRetryID
	command := service.telegramRetryCmd
	service.mu.Unlock()
	if calls != 1 {
		t.Fatalf("retry calls=%d, want 1", calls)
	}
	if alertID != "tgalt_1" {
		t.Fatalf("alert id=%q", alertID)
	}
	if command.RequestID != "admreq_tg_retry" ||
		command.AdminSubject != "admin_token" {
		t.Fatalf("command=%+v", command)
	}
}

func TestAdminTelegramAlertRetryRejectsWrongMethodBeforeService(t *testing.T) {
	events := []string{}
	service := &testService{}
	router, err := NewRouter(
		service,
		&testAuthenticator{events: &events, subject: "admin_token"},
		&testIDs{value: "admreq_tg_retry_method"},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/admin/v1/telegram-alerts/tgalt_1/retry",
		nil,
	)
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	service.mu.Lock()
	calls := service.telegramRetryCalls
	service.mu.Unlock()
	if calls != 0 {
		t.Fatalf("retry calls=%d, want 0", calls)
	}
}
