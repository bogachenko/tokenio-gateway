package openaicompat

import (
	"net/http"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/forwarding"
)

func TestStatusClassifier(t *testing.T) {
	cases := map[int]forwarding.Classification{
		300: {Kind: forwarding.FailureKindMalformedResponse},
		307: {Kind: forwarding.FailureKindMalformedResponse},
		400: {Kind: forwarding.FailureKindRequestError},
		401: {Kind: forwarding.FailureKindAuthError, RouteRetryCandidate: true},
		403: {Kind: forwarding.FailureKindAuthError, RouteRetryCandidate: true},
		404: {Kind: forwarding.FailureKindRequestError},
		409: {Kind: forwarding.FailureKindRequestError},
		429: {Kind: forwarding.FailureKindRateLimited, RouteRetryCandidate: true},
		500: {Kind: forwarding.FailureKindProvider5XX, RouteRetryCandidate: true},
		599: {Kind: forwarding.FailureKindProvider5XX, RouteRetryCandidate: true},
		102: {Kind: forwarding.FailureKindMalformedResponse},
	}
	for status, want := range cases {
		if got := (StatusClassifier{}).Classify(status, nil, []byte("ignored"), true); got != want {
			t.Fatalf("%d got %#v want %#v", status, got, want)
		}
	}
}

func TestStatusClassifierParsesSafeRetryAfter(t *testing.T) {
	date := time.Date(
		2026,
		time.June,
		17,
		18,
		0,
		0,
		0,
		time.UTC,
	)

	tests := []struct {
		name       string
		statusCode int
		headers    map[string][]string
		wantDelay  time.Duration
		wantAt     time.Time
	}{
		{
			name:       "429 delta seconds",
			statusCode: http.StatusTooManyRequests,
			headers: map[string][]string{
				"retry-after": {"7"},
			},
			wantDelay: 7 * time.Second,
		},
		{
			name:       "503 HTTP date",
			statusCode: http.StatusServiceUnavailable,
			headers: map[string][]string{
				"Retry-After": {date.Format(http.TimeFormat)},
			},
			wantAt: date,
		},
		{
			name:       "invalid ignored",
			statusCode: http.StatusTooManyRequests,
			headers: map[string][]string{
				"Retry-After": {"not-valid"},
			},
		},
		{
			name:       "multiple ignored",
			statusCode: http.StatusTooManyRequests,
			headers: map[string][]string{
				"Retry-After": {"1", "2"},
			},
		},
		{
			name:       "header ignored for other status",
			statusCode: http.StatusInternalServerError,
			headers: map[string][]string{
				"Retry-After": {"7"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			classification := (StatusClassifier{}).Classify(
				test.statusCode,
				test.headers,
				nil,
				false,
			)
			if classification.RetryAfter.Delay() != test.wantDelay ||
				!classification.RetryAfter.At().Equal(test.wantAt) {
				t.Fatalf(
					"retry-after delay=%s at=%s",
					classification.RetryAfter.Delay(),
					classification.RetryAfter.At(),
				)
			}
		})
	}
}
