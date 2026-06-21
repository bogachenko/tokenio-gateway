package transportfailure

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"

	"github.com/bogachenko/tokenio-gateway/internal/application/forwarding"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type WriteTracker struct {
	attempted atomic.Bool
}

func WithTrace(ctx context.Context, tracker *WriteTracker) context.Context {
	return httptrace.WithClientTrace(ctx, tracker.HTTPTrace())
}

func (tracker *WriteTracker) HTTPTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		WroteHeaders: func() { tracker.MarkAttempted() },
		WroteRequest: func(httptrace.WroteRequestInfo) { tracker.MarkAttempted() },
	}
}

func (tracker *WriteTracker) MarkAttempted() {
	if tracker != nil {
		tracker.attempted.Store(true)
	}
}

func (tracker *WriteTracker) Attempted() bool {
	return tracker != nil && tracker.attempted.Load()
}

func NewTrackedBody(body []byte, tracker *WriteTracker) io.ReadCloser {
	return &trackedBody{
		reader:  bytes.NewReader(body),
		tracker: tracker,
	}
}

type trackedBody struct {
	reader  *bytes.Reader
	tracker *WriteTracker
}

func (body *trackedBody) Read(p []byte) (int, error) {
	if body.tracker != nil {
		body.tracker.MarkAttempted()
	}
	return body.reader.Read(p)
}

func (body *trackedBody) Close() error { return nil }

func ContextErr(err error) (ports.ForwardResponse, error) {
	kind := forwarding.FailureKindRequestError
	retry := false
	if errors.Is(err, context.DeadlineExceeded) {
		kind = forwarding.FailureKindTimeout
		retry = true
	}
	return ports.ForwardResponse{}, forwarding.NewFailure(
		kind,
		0,
		forwarding.AttemptStateNotSent,
		retry,
		err,
	)
}

func TransportErr(err error, writeAttempted bool) (ports.ForwardResponse, error) {
	if writeAttempted {
		return ports.ForwardResponse{}, forwarding.NewFailure(
			forwarding.FailureKindUncertainProcessing,
			0,
			forwarding.AttemptStateSentNoResponse,
			false,
			err,
		)
	}
	return ports.ForwardResponse{}, forwarding.NewFailure(
		TransportFailureKind(err),
		0,
		forwarding.AttemptStateNotSent,
		true,
		err,
	)
}

func NilResponse() (ports.ForwardResponse, error) {
	return ports.ForwardResponse{}, forwarding.NewFailure(
		forwarding.FailureKindMalformedResponse,
		0,
		forwarding.AttemptStateNotSent,
		true,
		nil,
	)
}

func TransportFailureKind(err error) forwarding.FailureKind {
	if errors.Is(err, context.DeadlineExceeded) {
		return forwarding.FailureKindTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return forwarding.FailureKindTimeout
	}
	return forwarding.FailureKindConnectionError
}
