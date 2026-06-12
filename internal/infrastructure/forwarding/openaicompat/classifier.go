package openaicompat

import "github.com/bogachenko/tokenio-gateway/internal/application/forwarding"

type ErrorClassifier interface {
	Classify(statusCode int, headers map[string][]string, body []byte, bodyTruncated bool) forwarding.Classification
}

type StatusClassifier struct{}

func (StatusClassifier) Classify(statusCode int, _ map[string][]string, _ []byte, _ bool) forwarding.Classification {
	switch {
	case statusCode == 401 || statusCode == 403:
		return forwarding.Classification{Kind: forwarding.FailureKindAuthError, RouteRetryCandidate: true}
	case statusCode == 429:
		return forwarding.Classification{Kind: forwarding.FailureKindRateLimited, RouteRetryCandidate: true}
	case statusCode >= 400 && statusCode <= 499:
		return forwarding.Classification{Kind: forwarding.FailureKindRequestError, RouteRetryCandidate: false}
	case statusCode >= 500 && statusCode <= 599:
		return forwarding.Classification{Kind: forwarding.FailureKindServerError, RouteRetryCandidate: true}
	case statusCode >= 300 && statusCode <= 399:
		return forwarding.Classification{Kind: forwarding.FailureKindUnexpectedResponse, RouteRetryCandidate: false}
	default:
		return forwarding.Classification{Kind: forwarding.FailureKindUnexpectedResponse, RouteRetryCandidate: false}
	}
}
