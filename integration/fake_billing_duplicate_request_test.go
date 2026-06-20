//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	fb "github.com/bogachenko/tokenio-gateway/integration/fakes/billing"
)

func TestFakeBillingDuplicateRequestScenario(t *testing.T) {
	t.Parallel()

	server := fb.New()
	defer server.Close()

	server.SetResponse(fb.Response{
		Status: http.StatusOK,
		Header: duplicateBillingJSONHeader(),
		Body: []byte(`{
			"status":"duplicate",
			"idempotency_key":"charge-duplicate-1",
			"charged_amount":"10.00"
		}`),
	})

	body := `{"idempotency_key":"charge-duplicate-1","requested_amount":"10.00"}`
	first, err := http.Post(server.URL()+"/v1/charges", "application/json", strings.NewReader(body))
	assertDuplicateBillingResponse(t, first, err)

	second, err := http.Post(server.URL()+"/v1/charges", "application/json", strings.NewReader(body))
	assertDuplicateBillingResponse(t, second, err)

	requests := server.Requests()
	if len(requests) != 2 {
		t.Fatalf("requests=%d", len(requests))
	}
	for index, request := range requests {
		if request.Method != http.MethodPost || request.Path != "/v1/charges" {
			t.Fatalf("request[%d]=%s %s", index, request.Method, request.Path)
		}
		if string(request.Body) != body {
			t.Fatalf("request[%d] body=%s", index, string(request.Body))
		}
	}
}

func duplicateBillingJSONHeader() http.Header {
	return http.Header{
		"Content-Type": []string{"application/json"},
	}
}

func assertDuplicateBillingResponse(t *testing.T, response *http.Response, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("post duplicate charge: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, string(body))
	}
	compact := compactDuplicateBillingJSON(string(body))
	for _, want := range []string{`"status":"duplicate"`, `"idempotency_key":"charge-duplicate-1"`} {
		if !strings.Contains(compact, want) {
			t.Fatalf("body=%s does not contain %s", string(body), want)
		}
	}
}

func compactDuplicateBillingJSON(value string) string {
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "\n", "")
	value = strings.ReplaceAll(value, "\t", "")
	return value
}
