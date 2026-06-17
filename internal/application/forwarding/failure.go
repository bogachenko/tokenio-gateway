package forwarding

import (
	"errors"
	"fmt"
	"time"
)

var ErrInvalidRetryAfter = errors.New("invalid retry-after directive")

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

type RetryAfter struct {
	present bool
	delay   time.Duration
	at      time.Time
}

func NewRetryAfterDelay(delay time.Duration) (RetryAfter, error) {
	if delay < 0 {
		return RetryAfter{}, ErrInvalidRetryAfter
	}
	return RetryAfter{present: true, delay: delay}, nil
}

func NewRetryAfterTime(at time.Time) (RetryAfter, error) {
	if at.IsZero() {
		return RetryAfter{}, ErrInvalidRetryAfter
	}
	return RetryAfter{present: true, at: at.UTC()}, nil
}

func (value RetryAfter) Delay() time.Duration {
	return value.delay
}

func (value RetryAfter) At() time.Time {
	return value.at
}

func (value RetryAfter) IsZero() bool {
	return !value.present
}

type Classification struct {
	Kind FailureKind

	RouteRetryCandidate bool
	RetryAfter          RetryAfter
}

type Failure struct {
	Kind FailureKind

	StatusCode   int
	AttemptState AttemptState

	RouteRetryCandidate bool
	retryAfter          RetryAfter

	cause error
}

func NewFailure(
	kind FailureKind,
	statusCode int,
	attemptState AttemptState,
	routeRetryCandidate bool,
	cause error,
) *Failure {
	return NewFailureWithRetryAfter(
		kind,
		statusCode,
		attemptState,
		routeRetryCandidate,
		RetryAfter{},
		cause,
	)
}

func NewFailureWithRetryAfter(
	kind FailureKind,
	statusCode int,
	attemptState AttemptState,
	routeRetryCandidate bool,
	retryAfter RetryAfter,
	cause error,
) *Failure {
	return &Failure{
		Kind:                kind,
		StatusCode:          statusCode,
		AttemptState:        attemptState,
		RouteRetryCandidate: routeRetryCandidate,
		retryAfter:          retryAfter,
		cause:               cause,
	}
}

func (f *Failure) Error() string {
	if f == nil {
		return "forwarding failure"
	}
	if f.StatusCode > 0 {
		return fmt.Sprintf(
			"forwarding failure: kind=%s status=%d attempt_state=%s route_retry_candidate=%t",
			f.Kind,
			f.StatusCode,
			f.AttemptState,
			f.RouteRetryCandidate,
		)
	}
	return fmt.Sprintf(
		"forwarding failure: kind=%s attempt_state=%s route_retry_candidate=%t",
		f.Kind,
		f.AttemptState,
		f.RouteRetryCandidate,
	)
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

func (f *Failure) FailureRetryAfter() RetryAfter {
	if f == nil {
		return RetryAfter{}
	}
	return f.retryAfter
}

func (f *Failure) FailureRetryAfterPresent() bool {
	return f != nil && !f.retryAfter.IsZero()
}

func (f *Failure) FailureRetryAfterDelay() time.Duration {
	if f == nil {
		return 0
	}
	return f.retryAfter.Delay()
}

func (f *Failure) FailureRetryAfterTime() time.Time {
	if f == nil {
		return time.Time{}
	}
	return f.retryAfter.At()
}

func (f *Failure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.cause
}
