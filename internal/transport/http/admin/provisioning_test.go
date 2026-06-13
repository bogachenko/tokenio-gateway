package adminhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	application "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type provisioningRouterService struct {
	Service

	input  application.APIKeyProvisioningListInput
	result application.ListResult[application.APIKeyProvisioningView]
	err    error
	calls  int
}

func (s *provisioningRouterService) ListAPIKeyProvisionings(
	_ context.Context,
	input application.APIKeyProvisioningListInput,
) (application.ListResult[application.APIKeyProvisioningView], error) {
	s.calls++
	s.input = input
	return s.result, s.err
}

func TestAdminAPIKeyProvisioningsEndpoint(t *testing.T) {
	createdAt := time.Date(
		2026,
		time.June,
		13,
		12,
		0,
		0,
		0,
		time.UTC,
	)
	expiresAt := createdAt.Add(24 * time.Hour)
	service := &provisioningRouterService{
		result: application.ListResult[application.APIKeyProvisioningView]{
			Data: []application.APIKeyProvisioningView{
				{
					ProvisioningID:        "prov_1",
					ExternalBillingUserID: "billing_1",
					UserID:                "usr_1",
					APIKeyID:              "ak_1",
					KeyPrefix:             "sk_live_abcd...",
					ResultType:            domain.APIKeyProvisioningResultTypeKeyCreated,
					Status:                domain.APIKeyProvisioningStatusPendingDelivery,
					SourceReferenceHash:   strings.Repeat("a", 64),
					CreatedAt:             createdAt,
					ExpiresAt:             &expiresAt,
				},
			},
			Pagination: application.Pagination{
				Limit:  25,
				Offset: 5,
				Total:  1,
			},
		},
	}
	events := []string{}
	router, err := NewRouter(
		service,
		&testAuthenticator{
			events:  &events,
			subject: "admin_token",
		},
		&testIDs{value: "admreq_provisionings"},
	)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/admin/v1/api-key-provisionings"+
			"?external_billing_user_id=billing_1"+
			"&user_id=usr_1"+
			"&api_key_id=ak_1"+
			"&status=pending_delivery"+
			"&result_type=key_created"+
			"&created_from=2026-06-01T00:00:00Z"+
			"&created_to=2026-07-01T00:00:00Z"+
			"&limit=25&offset=5",
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
	if service.calls != 1 ||
		service.input.ExternalBillingUserID != "billing_1" ||
		service.input.UserID != "usr_1" ||
		service.input.APIKeyID != "ak_1" ||
		service.input.Status !=
			domain.APIKeyProvisioningStatusPendingDelivery ||
		service.input.ResultType !=
			domain.APIKeyProvisioningResultTypeKeyCreated ||
		service.input.CreatedFrom == nil ||
		service.input.CreatedTo == nil ||
		service.input.Limit != 25 ||
		service.input.Offset != 5 {
		t.Fatalf(
			"calls=%d input=%+v",
			service.calls,
			service.input,
		)
	}

	var body struct {
		Data       []application.APIKeyProvisioningView `json:"data"`
		Pagination application.Pagination               `json:"pagination"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 ||
		body.Data[0].ProvisioningID != "prov_1" ||
		body.Data[0].KeyPrefix != "sk_live_abcd..." ||
		body.Pagination.Total != 1 {
		t.Fatalf("body=%+v", body)
	}

	responseText := response.Body.String()
	for _, forbidden := range []string{
		`"api_key":`,
		`"encrypted_raw_key":`,
		`"encryption_nonce":`,
		`"encryption_key_version":`,
		`"key_hash":`,
		`"idempotency_key":`,
		`"delivery_attempts":`,
	} {
		if strings.Contains(responseText, forbidden) {
			t.Fatalf(
				"response contains forbidden field %s: %s",
				forbidden,
				responseText,
			)
		}
	}
}

func TestAdminAPIKeyProvisioningsRejectsInvalidRequestBeforeService(
	t *testing.T,
) {
	tests := []struct {
		name   string
		method string
		target string
		status int
	}{
		{
			name:   "wrong method",
			method: http.MethodPost,
			target: "/admin/v1/api-key-provisionings",
			status: http.StatusMethodNotAllowed,
		},
		{
			name:   "invalid created from",
			method: http.MethodGet,
			target: "/admin/v1/api-key-provisionings?created_from=invalid",
			status: http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &provisioningRouterService{}
			events := []string{}
			router, err := NewRouter(
				service,
				&testAuthenticator{
					events:  &events,
					subject: "admin_token",
				},
				&testIDs{value: "admreq_provisionings"},
			)
			if err != nil {
				t.Fatal(err)
			}

			request := httptest.NewRequest(
				test.method,
				test.target,
				nil,
			)
			request.Header.Set("Authorization", "Bearer admin")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf(
					"status=%d want=%d body=%s",
					response.Code,
					test.status,
					response.Body.String(),
				)
			}
			if service.calls != 0 {
				t.Fatalf(
					"application calls=%d, want 0",
					service.calls,
				)
			}
		})
	}
}
