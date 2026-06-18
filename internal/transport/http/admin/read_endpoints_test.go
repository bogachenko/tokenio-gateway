package adminhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	application "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type adminReadEndpointsService struct {
	Service
	provisioning application.APIKeyProvisioningView
	routeInput   application.RouteEventListInput
}

func (s *adminReadEndpointsService) GetAPIKeyProvisioning(
	context.Context,
	string,
) (application.APIKeyProvisioningView, error) {
	return s.provisioning, nil
}

func (s *adminReadEndpointsService) ListRouteEvents(
	_ context.Context,
	input application.RouteEventListInput,
) (application.ListResult[domain.RouteEvent], error) {
	s.routeInput = input
	return application.ListResult[domain.RouteEvent]{
		Data: []domain.RouteEvent{},
		Pagination: application.Pagination{
			Limit:  input.Limit,
			Offset: input.Offset,
		},
	}, nil
}

func TestAdminReadEndpointsDispatchAndSafeProvisioningDTO(t *testing.T) {
	eventsLog := []string{}
	service := &adminReadEndpointsService{
		provisioning: application.APIKeyProvisioningView{
			ProvisioningID: "prov_1",
			KeyPrefix:      "sk_live_abcd",
		},
	}
	router, err := NewRouter(
		service,
		&testAuthenticator{
			events:  &eventsLog,
			subject: "admin_token",
		},
		&testIDs{value: "admreq_read"},
	)
	if err != nil {
		t.Fatal(err)
	}

	provisioning := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/admin/v1/api-key-provisionings/prov_1",
		nil,
	)
	req.Header.Set("Authorization", "Bearer admin")
	router.ServeHTTP(provisioning, req)
	if provisioning.Code != http.StatusOK ||
		!strings.Contains(provisioning.Body.String(), `"provisioning_id":"prov_1"`) ||
		strings.Contains(provisioning.Body.String(), "encrypted_raw_key") ||
		strings.Contains(provisioning.Body.String(), "encryption_nonce") ||
		strings.Contains(provisioning.Body.String(), `"api_key"`) {
		t.Fatalf("status=%d body=%s", provisioning.Code, provisioning.Body.String())
	}

	events := httptest.NewRecorder()
	req = httptest.NewRequest(
		http.MethodGet,
		"/admin/v1/route-events?route_id=route_1&limit=25&offset=5",
		nil,
	)
	req.Header.Set("Authorization", "Bearer admin")
	router.ServeHTTP(events, req)
	if events.Code != http.StatusOK ||
		service.routeInput.RouteID != "route_1" ||
		service.routeInput.Limit != 25 ||
		service.routeInput.Offset != 5 {
		t.Fatalf(
			"status=%d input=%+v body=%s",
			events.Code,
			service.routeInput,
			events.Body.String(),
		)
	}
}
