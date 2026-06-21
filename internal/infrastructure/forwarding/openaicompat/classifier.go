package openaicompat

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	failure "github.com/bogachenko/tokenio-gateway/internal/ports/forwardingfailure"
)

type ErrorClassifier interface {
	Classify(
		statusCode int,
		headers map[string][]string,
		body []byte,
		bodyTruncated bool,
	) failure.Classification
}

type StatusClassifier struct{}

func (StatusClassifier) Classify(
	statusCode int,
	headers map[string][]string,
	_ []byte,
	_ bool,
) failure.Classification {
	switch {
	case statusCode == 401 || statusCode == 403:
		return failure.Classification{
			Kind:                failure.FailureKindAuthError,
			RouteRetryCandidate: true,
		}
	case statusCode == http.StatusTooManyRequests:
		return failure.Classification{
			Kind:                failure.FailureKindRateLimited,
			RouteRetryCandidate: true,
			RetryAfter:          parseRetryAfter(headers),
		}
	case statusCode >= 400 && statusCode <= 499:
		return failure.Classification{
			Kind: failure.FailureKindRequestError,
		}
	case statusCode >= 500 && statusCode <= 599:
		classification := failure.Classification{
			Kind:                failure.FailureKindProvider5XX,
			RouteRetryCandidate: true,
		}
		if statusCode == http.StatusServiceUnavailable {
			classification.RetryAfter = parseRetryAfter(headers)
		}
		return classification
	case statusCode >= 300 && statusCode <= 399:
		return failure.Classification{
			Kind: failure.FailureKindMalformedResponse,
		}
	default:
		return failure.Classification{
			Kind: failure.FailureKindMalformedResponse,
		}
	}
}

func parseRetryAfter(
	headers map[string][]string,
) failure.RetryAfter {
	values := retryAfterHeaderValues(headers)
	if len(values) != 1 {
		return failure.RetryAfter{}
	}
	value := strings.TrimSpace(values[0])
	if value == "" {
		return failure.RetryAfter{}
	}

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 || seconds > int64(^uint64(0)>>1)/int64(time.Second) {
			return failure.RetryAfter{}
		}
		result, directiveErr := failure.NewRetryAfterDelay(
			time.Duration(seconds) * time.Second,
		)
		if directiveErr != nil {
			return failure.RetryAfter{}
		}
		return result
	}

	at, err := http.ParseTime(value)
	if err != nil {
		return failure.RetryAfter{}
	}
	result, directiveErr := failure.NewRetryAfterTime(at)
	if directiveErr != nil {
		return failure.RetryAfter{}
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
