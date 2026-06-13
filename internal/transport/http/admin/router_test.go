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

	application "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type testIDs struct {
	mu     sync.Mutex
	events []string
	value  string
}

func (f *testIDs) NewLocalRequestID() string         { return "llmreq_unused" }
func (f *testIDs) NewBillingChargeRequestID() string { return "billchg_unused" }
func (f *testIDs) NewAdminRequestID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "request_id")
	return f.value
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
