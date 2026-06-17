package openaicompat

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/forwarding"
)

type ErrorClassifier interface {
	Classify(
		statusCode int,
		headers map[string][]string,
		body []byte,
		bodyTruncated bool,
	) forwarding.Classification
}

type StatusClassifier struct{}

func (StatusClassifier) Classify(
	statusCode int,
	headers map[string][]string,
	_ []byte,
	_ bool,
) forwarding.Classification {
	switch {
	case statusCode == 401 || statusCode == 403:
		return forwarding.Classification{
			Kind:                forwarding.FailureKindAuthError,
			RouteRetryCandidate: true,
		}
	case statusCode == http.StatusTooManyRequests:
		return forwarding.Classification{
			Kind:                forwarding.FailureKindRateLimited,
			RouteRetryCandidate: true,
			RetryAfter:          parseRetryAfter(headers),
		}
	case statusCode >= 400 && statusCode <= 499:
		return forwarding.Classification{
			Kind: forwarding.FailureKindRequestError,
		}
	case statusCode >= 500 && statusCode <= 599:
		classification := forwarding.Classification{
			Kind:                forwarding.FailureKindProvider5XX,
			RouteRetryCandidate: true,
		}
		if statusCode == http.StatusServiceUnavailable {
			classification.RetryAfter = parseRetryAfter(headers)
		}
		return classification
	case statusCode >= 300 && statusCode <= 399:
		return forwarding.Classification{
			Kind: forwarding.FailureKindMalformedResponse,
		}
	default:
		return forwarding.Classification{
			Kind: forwarding.FailureKindMalformedResponse,
		}
	}
}

func parseRetryAfter(
	headers map[string][]string,
) forwarding.RetryAfter {
	values := retryAfterHeaderValues(headers)
	if len(values) != 1 {
		return forwarding.RetryAfter{}
	}
	value := strings.TrimSpace(values[0])
	if value == "" {
		return forwarding.RetryAfter{}
	}

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 || seconds > int64(^uint64(0)>>1)/int64(time.Second) {
			return forwarding.RetryAfter{}
		}
		result, directiveErr := forwarding.NewRetryAfterDelay(
			time.Duration(seconds) * time.Second,
		)
		if directiveErr != nil {
			return forwarding.RetryAfter{}
		}
		return result
	}

	at, err := http.ParseTime(value)
	if err != nil {
		return forwarding.RetryAfter{}
	}
	result, directiveErr := forwarding.NewRetryAfterTime(at)
	if directiveErr != nil {
		return forwarding.RetryAfter{}
	}
	return result
}

func retryAfterHeaderValues(
	headers map[string][]string,
) []string {
	var result []string
	for name, values := range headers {
		if strings.EqualFold(name, "Retry-After") {
			result = append(result, values...)
		}
	}
	return result
}
