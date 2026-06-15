package forwarding

import "fmt"

type FailureKind string

const (
	FailureKindRequestError                FailureKind = "request_error"
	FailureKindAuthError                   FailureKind = "auth_error"
	FailureKindRateLimited                 FailureKind = "rate_limited"
	FailureKindQuotaExceeded               FailureKind = "quota_exceeded"
	FailureKindInsufficientResellerBalance FailureKind = "insufficient_reseller_balance"
	FailureKindServerError                 FailureKind = "server_error"
	FailureKindUnavailable                 FailureKind = "unavailable"
	FailureKindUnexpectedResponse          FailureKind = "unexpected_response"
	FailureKindResponseTooLarge            FailureKind = "response_too_large"
	FailureKindInvalidAdapterInput         FailureKind = "invalid_adapter_input"
)

type AttemptState string

const (
	AttemptStateNotSent          AttemptState = "not_sent"
	AttemptStateSentNoResponse   AttemptState = "sent_no_response"
	AttemptStateResponseReceived AttemptState = "response_received"
)

type Classification struct {
	Kind FailureKind

	RouteRetryCandidate bool
}

type Failure struct {
	Kind FailureKind

	StatusCode   int
	AttemptState AttemptState

	RouteRetryCandidate bool

	cause error
}

func NewFailure(kind FailureKind, statusCode int, attemptState AttemptState, routeRetryCandidate bool, cause error) *Failure {
	return &Failure{
		Kind:                kind,
		StatusCode:          statusCode,
		AttemptState:        attemptState,
		RouteRetryCandidate: routeRetryCandidate,
		cause:               cause,
	}
}

func (f *Failure) Error() string {
	if f == nil {
		return "forwarding failure"
	}
	if f.StatusCode > 0 {
		return fmt.Sprintf("forwarding failure: kind=%s status=%d attempt_state=%s route_retry_candidate=%t", f.Kind, f.StatusCode, f.AttemptState, f.RouteRetryCandidate)
	}
	return fmt.Sprintf("forwarding failure: kind=%s attempt_state=%s route_retry_candidate=%t", f.Kind, f.AttemptState, f.RouteRetryCandidate)
}

func (f *Failure) FailureKindValue() string {
	if f == nil {
		return ""
	}
	return string(f.Kind)
}

func (f *Failure) FailureStatusCode() int {
	if f == nil {
		return 0
	}
	return f.StatusCode
}

func (f *Failure) FailureAttemptStateValue() string {
	if f == nil {
		return ""
	}
	return string(f.AttemptState)
}

func (f *Failure) FailureRouteRetryCandidate() bool {
	return f != nil && f.RouteRetryCandidate
}

func (f *Failure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.cause
}
