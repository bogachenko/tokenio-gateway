package transportfailure

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http/httptrace"
	"sync/atomic"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
	failure "github.com/bogachenko/tokenio-gateway/internal/ports/forwardingfailure"
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
	kind := failure.FailureKindRequestError
	retry := false
	if errors.Is(err, context.DeadlineExceeded) {
		kind = failure.FailureKindTimeout
		retry = true
	}
	return ports.ForwardResponse{}, failure.NewFailure(
		kind,
		0,
		failure.AttemptStateNotSent,
		retry,
		err,
	)
}

func TransportErr(err error, writeAttempted bool) (ports.ForwardResponse, error) {
	if writeAttempted {
		return ports.ForwardResponse{}, failure.NewFailure(
			failure.FailureKindUncertainProcessing,
			0,
			failure.AttemptStateSentNoResponse,
			false,
			err,
		)
	}
	return ports.ForwardResponse{}, failure.NewFailure(
		TransportFailureKind(err),
		0,
		failure.AttemptStateNotSent,
		true,
		err,
	)
}

func NilResponse() (ports.ForwardResponse, error) {
	return ports.ForwardResponse{}, failure.NewFailure(
		failure.FailureKindMalformedResponse,
		0,
		failure.AttemptStateNotSent,
		true,
		nil,
	)
}

func TransportFailureKind(err error) failure.FailureKind {
	if errors.Is(err, context.DeadlineExceeded) {
		return failure.FailureKindTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return failure.FailureKindTimeout
	}
	return failure.FailureKindConnectionError
}
