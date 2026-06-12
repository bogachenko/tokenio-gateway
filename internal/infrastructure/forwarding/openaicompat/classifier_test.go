package openaicompat

import (
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/forwarding"
)

func TestStatusClassifier(t *testing.T) {
	cases := map[int]forwarding.Classification{
		300: {Kind: forwarding.FailureKindUnexpectedResponse},
		307: {Kind: forwarding.FailureKindUnexpectedResponse},
		400: {Kind: forwarding.FailureKindRequestError},
		401: {Kind: forwarding.FailureKindAuthError, RouteRetryCandidate: true},
		403: {Kind: forwarding.FailureKindAuthError, RouteRetryCandidate: true},
		404: {Kind: forwarding.FailureKindRequestError},
		409: {Kind: forwarding.FailureKindRequestError},
		429: {Kind: forwarding.FailureKindRateLimited, RouteRetryCandidate: true},
		500: {Kind: forwarding.FailureKindServerError, RouteRetryCandidate: true},
		599: {Kind: forwarding.FailureKindServerError, RouteRetryCandidate: true},
		102: {Kind: forwarding.FailureKindUnexpectedResponse},
	}
	for status, want := range cases {
		if got := (StatusClassifier{}).Classify(status, nil, []byte("ignored"), true); got != want {
			t.Fatalf("%d got %#v want %#v", status, got, want)
		}
	}
}
