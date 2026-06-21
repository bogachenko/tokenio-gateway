package forwarding

import "github.com/bogachenko/tokenio-gateway/internal/ports/forwardingfailure"

var (
	ErrInvalidRetryAfter      = forwardingfailure.ErrInvalidRetryAfter
	NewFailure                = forwardingfailure.NewFailure
	NewFailureWithRetryAfter  = forwardingfailure.NewFailureWithRetryAfter
	NewRetryAfterDelay        = forwardingfailure.NewRetryAfterDelay
	NewRetryAfterTime         = forwardingfailure.NewRetryAfterTime
)

type FailureKind = forwardingfailure.FailureKind

const (
	FailureKindRequestError                = forwardingfailure.FailureKindRequestError
	FailureKindAuthError                   = forwardingfailure.FailureKindAuthError
	FailureKindRateLimited                 = forwardingfailure.FailureKindRateLimited
	FailureKindQuotaExceeded               = forwardingfailure.FailureKindQuotaExceeded
	FailureKindInsufficientResellerBalance = forwardingfailure.FailureKindInsufficientResellerBalance
	FailureKindProvider5XX                 = forwardingfailure.FailureKindProvider5XX
	FailureKindTimeout                     = forwardingfailure.FailureKindTimeout
	FailureKindConnectionError             = forwardingfailure.FailureKindConnectionError
	FailureKindUncertainProcessing         = forwardingfailure.FailureKindUncertainProcessing
	FailureKindMalformedResponse           = forwardingfailure.FailureKindMalformedResponse
	FailureKindInvalidAdapterInput         = forwardingfailure.FailureKindInvalidAdapterInput
)

type AttemptState = forwardingfailure.AttemptState

const (
	AttemptStateNotSent          = forwardingfailure.AttemptStateNotSent
	AttemptStateSentNoResponse   = forwardingfailure.AttemptStateSentNoResponse
	AttemptStateResponseReceived = forwardingfailure.AttemptStateResponseReceived
)

type RetryAfter = forwardingfailure.RetryAfter

type Classification = forwardingfailure.Classification

type Failure = forwardingfailure.Failure
