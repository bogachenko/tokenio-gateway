package adminhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	application "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type stage10MajorRouterIDsFake struct {
	mu sync.Mutex
	id string
}

func (f *stage10MajorRouterIDsFake) NewLocalRequestID() (string, error) {
	return "llmreq_test", nil
}
func (f *stage10MajorRouterIDsFake) NewProvisioningRequestID() (string, error) {
	return "provreq_test", nil
}
func (f *stage10MajorRouterIDsFake) NewAdminRequestID() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.id, nil
}

type stage10MajorRouterServiceFake struct {
	mu                  sync.Mutex
	listUsersCalls      int
	lastUserListInput   application.UserListInput
	lastResellerUpdate  application.UpdateResellerInput
	listUsersErr        error
	updateResellerCalls int
}

func (f *stage10MajorRouterServiceFake) ListUsers(_ context.Context, input application.UserListInput) (application.ListResult[domain.User], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listUsersCalls++
	f.lastUserListInput = input
	return application.ListResult[domain.User]{Data: []domain.User{}, Pagination: application.Pagination{Limit: input.Limit, Offset: input.Offset}}, f.listUsersErr
}
func (*stage10MajorRouterServiceFake) CreateUser(context.Context, application.CommandContext, application.CreateUserInput) (domain.User, error) {
	return domain.User{}, nil
}
func (*stage10MajorRouterServiceFake) SetUserEnabled(context.Context, application.CommandContext, string, bool) (domain.User, error) {
	return domain.User{}, nil
}
func (*stage10MajorRouterServiceFake) ListAPIKeys(context.Context, string, int, int) (application.ListResult[application.APIKeyView], error) {
	return application.ListResult[application.APIKeyView]{}, nil
}
func (*stage10MajorRouterServiceFake) ListAPIKeyProvisionings(
	context.Context,
	application.APIKeyProvisioningListInput,
) (application.ListResult[application.APIKeyProvisioningView], error) {
	return application.ListResult[application.APIKeyProvisioningView]{}, nil
}
func (*stage10MajorRouterServiceFake) CreateAPIKey(context.Context, application.CommandContext, application.CreateAPIKeyInput) (application.CreatedAPIKey, error) {
	return application.CreatedAPIKey{}, nil
}
func (*stage10MajorRouterServiceFake) RevokeAPIKey(context.Context, application.CommandContext, string) (application.APIKeyView, error) {
	return application.APIKeyView{}, nil
}
func (*stage10MajorRouterServiceFake) ListResellers(context.Context, application.ResellerListInput) (application.ListResult[application.ResellerView], error) {
	return application.ListResult[application.ResellerView]{}, nil
}
func (*stage10MajorRouterServiceFake) CreateReseller(context.Context, application.CommandContext, application.CreateResellerInput) (application.ResellerView, error) {
	return application.ResellerView{}, nil
}
func (f *stage10MajorRouterServiceFake) UpdateReseller(_ context.Context, _ application.CommandContext, input application.UpdateResellerInput) (application.ResellerView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateResellerCalls++
	f.lastResellerUpdate = input
	return application.ResellerView{ID: input.ID}, nil
}
func (*stage10MajorRouterServiceFake) SetResellerEnabled(context.Context, application.CommandContext, string, bool) (application.ResellerView, error) {
	return application.ResellerView{}, nil
}
func (*stage10MajorRouterServiceFake) GetResellerBalance(context.Context, string) (application.ResellerBalance, error) {
	return application.ResellerBalance{}, nil
}
func (*stage10MajorRouterServiceFake) AdjustResellerBalance(context.Context, application.CommandContext, string, int64, string) (application.ResellerBalance, error) {
	return application.ResellerBalance{}, nil
}
func (*stage10MajorRouterServiceFake) SetResellerBalance(context.Context, application.CommandContext, string, int64, string) (application.ResellerBalance, error) {
	return application.ResellerBalance{}, nil
}
func (*stage10MajorRouterServiceFake) ListRoutes(context.Context, application.RouteListInput) (application.ListResult[domain.Route], error) {
	return application.ListResult[domain.Route]{}, nil
}
func (*stage10MajorRouterServiceFake) CreateRoute(context.Context, application.CommandContext, domain.Route) (domain.Route, error) {
	return domain.Route{}, nil
}
func (*stage10MajorRouterServiceFake) UpdateRoute(context.Context, application.CommandContext, application.UpdateRouteInput) (domain.Route, error) {
	return domain.Route{}, nil
}
func (*stage10MajorRouterServiceFake) SetRouteEnabled(context.Context, application.CommandContext, string, bool) (domain.Route, error) {
	return domain.Route{}, nil
}
func (*stage10MajorRouterServiceFake) GetRouteCooldown(context.Context, string) (domain.Route, error) {
	return domain.Route{}, nil
}
func (*stage10MajorRouterServiceFake) SetRouteCooldown(context.Context, application.CommandContext, application.SetCooldownInput) (domain.Route, error) {
	return domain.Route{}, nil
}
func (*stage10MajorRouterServiceFake) ClearRouteCooldown(context.Context, application.CommandContext, string) (domain.Route, error) {
	return domain.Route{}, nil
}
func (*stage10MajorRouterServiceFake) GetRoutePrice(context.Context, string) (domain.RoutePrice, error) {
	return domain.RoutePrice{}, nil
}
func (*stage10MajorRouterServiceFake) UpsertRoutePrice(context.Context, application.CommandContext, domain.RoutePrice) (domain.RoutePrice, error) {
	return domain.RoutePrice{}, nil
}
func (*stage10MajorRouterServiceFake) ListUsageRecords(context.Context, application.UsageListInput) (application.ListResult[domain.UsageRecord], error) {
	return application.ListResult[domain.UsageRecord]{}, nil
}
func (*stage10MajorRouterServiceFake) GetUsageRecord(context.Context, string) (domain.UsageRecord, error) {
	return domain.UsageRecord{}, nil
}
func (*stage10MajorRouterServiceFake) ResolveUsageBillable(context.Context, application.CommandContext, application.ResolveBillableInput) (domain.UsageRecord, error) {
	return domain.UsageRecord{}, nil
}
func (*stage10MajorRouterServiceFake) ResolveUsageFailed(context.Context, application.CommandContext, application.ResolveFailedInput) (domain.UsageRecord, error) {
	return domain.UsageRecord{}, nil
}
func (*stage10MajorRouterServiceFake) ResolveUsageCharged(context.Context, application.CommandContext, application.ResolveChargedInput) (domain.UsageRecord, error) {
	return domain.UsageRecord{}, nil
}
func (*stage10MajorRouterServiceFake) ListBillingChargeBatches(context.Context, application.BillingBatchListInput) (application.ListResult[domain.BillingChargeBatch], error) {
	return application.ListResult[domain.BillingChargeBatch]{}, nil
}
func (*stage10MajorRouterServiceFake) GetBillingChargeBatch(context.Context, string) (ports.BillingChargeBatchSnapshot, error) {
	return ports.BillingChargeBatchSnapshot{}, nil
}
func (*stage10MajorRouterServiceFake) RetryFailedBillingChargeBatch(context.Context, application.CommandContext, string) (domain.BillingChargeBatch, error) {
	return domain.BillingChargeBatch{}, nil
}
func (*stage10MajorRouterServiceFake) ListAuditEntries(context.Context, application.AuditListInput) (application.ListResult[domain.AdminAuditEntry], error) {
	return application.ListResult[domain.AdminAuditEntry]{}, nil
}

func stage10MajorNewTestRouter(t *testing.T, service *stage10MajorRouterServiceFake) *Router {
	t.Helper()
	authenticator, err := auth.NewAdminAuthenticator("admin-secret")
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(service, authenticator, &stage10MajorRouterIDsFake{id: "admreq_test"})
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func TestStage10MajorAdminRequestIDExistsOnAuthErrorsAndUserKeyIsRejected(t *testing.T) {
	router := stage10MajorNewTestRouter(t, &stage10MajorRouterServiceFake{})
	cases := []struct {
		name       string
		authHeader string
		status     int
		code       string
	}{
		{"missing", "", http.StatusUnauthorized, "admin_unauthorized"},
		{"user api key", "Bearer sk_live_user", http.StatusForbidden, "admin_forbidden"},
		{"invalid admin token", "Bearer wrong", http.StatusForbidden, "admin_forbidden"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
			if tc.authHeader != "" {
				request.Header.Set("Authorization", tc.authHeader)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if got := response.Header().Get(adminRequestIDHeader); got != "admreq_test" {
				t.Fatalf("request id header = %q", got)
			}
			if !strings.Contains(response.Body.String(), `"request_id":"admreq_test"`) || !strings.Contains(response.Body.String(), tc.code) {
				t.Fatalf("body = %s", response.Body.String())
			}
			if strings.Contains(response.Body.String(), "admin-secret") {
				t.Fatal("admin token leaked into error")
			}
		})
	}
}

func TestStage10MajorAdminRoutesStayUnderAdminPrefixAndSuccessUsesListEnvelope(t *testing.T) {
	service := &stage10MajorRouterServiceFake{}
	router := stage10MajorNewTestRouter(t, service)

	publicRequest := httptest.NewRequest(http.MethodGet, "/v1/users", nil)
	publicRequest.Header.Set("Authorization", "Bearer admin-secret")
	publicResponse := httptest.NewRecorder()
	router.ServeHTTP(publicResponse, publicRequest)
	if publicResponse.Code != http.StatusNotFound {
		t.Fatalf("public path status = %d", publicResponse.Code)
	}
	service.mu.Lock()
	calls := service.listUsersCalls
	service.mu.Unlock()
	if calls != 0 {
		t.Fatalf("service called for public path: %d", calls)
	}

	request := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
	request.Header.Set("Authorization", "Bearer admin-secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get(adminRequestIDHeader) != "admreq_test" {
		t.Fatalf("request id header = %q", response.Header().Get(adminRequestIDHeader))
	}
	var envelope struct {
		Data       []domain.User          `json:"data"`
		Pagination application.Pagination `json:"pagination"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Pagination.Limit != 50 || envelope.Pagination.Offset != 0 {
		t.Fatalf("pagination = %+v", envelope.Pagination)
	}
}

func TestStage10MajorPaginationValidationAndPatchZeroValuesRemainPresent(t *testing.T) {
	service := &stage10MajorRouterServiceFake{}
	router := stage10MajorNewTestRouter(t, service)

	invalid := httptest.NewRequest(http.MethodGet, "/admin/v1/users?limit=501", nil)
	invalid.Header.Set("Authorization", "Bearer admin-secret")
	invalidResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest || !strings.Contains(invalidResponse.Body.String(), "admin_validation_error") {
		t.Fatalf("invalid pagination response = %d %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	body := bytes.NewBufferString(`{"enabled":false,"minimum_balance_cents":0}`)
	request := httptest.NewRequest(http.MethodPatch, "/admin/v1/resellers/reseller_1", body)
	request.Header.Set("Authorization", "Bearer admin-secret")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	input := service.lastResellerUpdate
	if service.updateResellerCalls != 1 || input.Enabled == nil || *input.Enabled || input.MinimumBalanceCents == nil || *input.MinimumBalanceCents != 0 {
		t.Fatalf("PATCH input = %+v", input)
	}
}
