package authenticate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type recordingAuthenticatorFake struct {
	result       Result
	err          error
	calls        int
	ctx          context.Context
	input        Input
	beforeReturn func()
}

func (f *recordingAuthenticatorFake) AuthenticatePublicRequest(
	ctx context.Context,
	input Input,
) (Result, error) {
	f.calls++
	f.ctx = ctx
	f.input = input
	if f.beforeReturn != nil {
		f.beforeReturn()
	}
	return f.result, f.err
}

type recordingUsageRecorderFake struct {
	err            error
	blockUntilDone bool

	calls       int
	ctx         context.Context
	apiKeyID    string
	usedAt      time.Time
	errAtCall   error
	waitErr     error
	deadline    time.Time
	hasDeadline bool
	contextData any
}

func (f *recordingUsageRecorderFake) RecordLastUsedAt(
	ctx context.Context,
	apiKeyID string,
	usedAt time.Time,
) error {
	f.calls++
	f.ctx = ctx
	f.apiKeyID = apiKeyID
	f.usedAt = usedAt
	f.errAtCall = ctx.Err()
	f.deadline, f.hasDeadline = ctx.Deadline()
	f.contextData = ctx.Value(recordingContextKey{})

	if f.blockUntilDone {
		<-ctx.Done()
		f.waitErr = ctx.Err()
		return ctx.Err()
	}
	return f.err
}

type recordingClockFake struct {
	now   time.Time
	calls int
}

func (f *recordingClockFake) Now() time.Time {
	f.calls++
	return f.now
}

type recordingContextKey struct{}

func TestUsageRecordingAuthenticatorRecordsAfterSuccess(
	t *testing.T,
) {
	location := time.FixedZone("UTC+3", 3*60*60)
	fixed := time.Date(
		2026,
		time.June,
		14,
		15,
		0,
		0,
		0,
		location,
	)

	parent := context.WithValue(
		context.Background(),
		recordingContextKey{},
		"trace-value",
	)
	parent, cancelParent := context.WithCancel(parent)

	next := &recordingAuthenticatorFake{
		result: Result{
			Principal: auth.APIKeyPrincipal{
				UserID:               "usr_1",
				APIKeyID:             "key_1",
				BillingSubjectUserID: "billing_1",
			},
		},
		beforeReturn: cancelParent,
	}
	recorder := &recordingUsageRecorderFake{}
	clock := &recordingClockFake{now: fixed}

	authenticator, err :=
		NewUsageRecordingAuthenticator(
			next,
			recorder,
			clock,
			250*time.Millisecond,
		)
	if err != nil {
		t.Fatalf(
			"NewUsageRecordingAuthenticator: %v",
			err,
		)
	}

	result, err :=
		authenticator.AuthenticatePublicRequest(
			parent,
			Input{RawAPIKey: "sk_test_value"},
		)
	if err != nil {
		t.Fatalf(
			"AuthenticatePublicRequest: %v",
			err,
		)
	}

	if result != next.result {
		t.Fatalf(
			"result=%+v want=%+v",
			result,
			next.result,
		)
	}
	if next.calls != 1 ||
		next.input.RawAPIKey != "sk_test_value" {
		t.Fatalf(
			"next calls=%d input=%+v",
			next.calls,
			next.input,
		)
	}
	if recorder.calls != 1 ||
		recorder.apiKeyID != "key_1" {
		t.Fatalf(
			"recorder calls=%d apiKeyID=%q",
			recorder.calls,
			recorder.apiKeyID,
		)
	}
	if recorder.errAtCall != nil {
		t.Fatalf(
			"recorder context inherited cancellation: %v",
			recorder.errAtCall,
		)
	}
	if !recorder.hasDeadline {
		t.Fatal("recorder context has no deadline")
	}
	if recorder.contextData != "trace-value" {
		t.Fatalf(
			"context value=%v want trace-value",
			recorder.contextData,
		)
	}
	if !recorder.usedAt.Equal(fixed.UTC()) ||
		recorder.usedAt.Location() != time.UTC {
		t.Fatalf(
			"usedAt=%v want UTC %v",
			recorder.usedAt,
			fixed.UTC(),
		)
	}
	if clock.calls != 1 {
		t.Fatalf(
			"clock calls=%d want=1",
			clock.calls,
		)
	}
	if recorder.ctx == nil ||
		!errors.Is(
			recorder.ctx.Err(),
			context.Canceled,
		) {
		t.Fatalf(
			"recording context was not canceled after call: %v",
			recorder.ctx,
		)
	}
}

func TestUsageRecordingAuthenticatorIgnoresRecorderError(
	t *testing.T,
) {
	want := Result{
		Principal: auth.APIKeyPrincipal{
			UserID:               "usr_1",
			APIKeyID:             "key_1",
			BillingSubjectUserID: "billing_1",
		},
	}
	next := &recordingAuthenticatorFake{
		result: want,
	}
	recorder := &recordingUsageRecorderFake{
		err: ports.ErrStoreUnavailable,
	}
	clock := &recordingClockFake{
		now: time.Date(
			2026,
			time.June,
			14,
			12,
			0,
			0,
			0,
			time.UTC,
		),
	}

	authenticator, err :=
		NewUsageRecordingAuthenticator(
			next,
			recorder,
			clock,
			time.Second,
		)
	if err != nil {
		t.Fatal(err)
	}

	got, err :=
		authenticator.AuthenticatePublicRequest(
			context.Background(),
			Input{RawAPIKey: "sk_test_value"},
		)
	if err != nil {
		t.Fatalf(
			"recorder error changed auth result: %v",
			err,
		)
	}
	if got != want {
		t.Fatalf("result=%+v want=%+v", got, want)
	}
	if recorder.calls != 1 {
		t.Fatalf(
			"recorder calls=%d want=1",
			recorder.calls,
		)
	}
}

func TestUsageRecordingAuthenticatorDoesNotRecordAuthFailure(
	t *testing.T,
) {
	next := &recordingAuthenticatorFake{
		err: ErrInvalidAPIKey,
	}
	recorder := &recordingUsageRecorderFake{}
	clock := &recordingClockFake{}

	authenticator, err :=
		NewUsageRecordingAuthenticator(
			next,
			recorder,
			clock,
			time.Second,
		)
	if err != nil {
		t.Fatal(err)
	}

	result, err :=
		authenticator.AuthenticatePublicRequest(
			context.Background(),
			Input{RawAPIKey: "sk_invalid"},
		)
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf(
			"error=%v want ErrInvalidAPIKey",
			err,
		)
	}
	if result != (Result{}) {
		t.Fatalf("partial result=%+v", result)
	}
	if recorder.calls != 0 || clock.calls != 0 {
		t.Fatalf(
			"recorder calls=%d clock calls=%d",
			recorder.calls,
			clock.calls,
		)
	}
}

func TestUsageRecordingAuthenticatorBoundsBlockingRecorder(
	t *testing.T,
) {
	next := &recordingAuthenticatorFake{
		result: Result{
			Principal: auth.APIKeyPrincipal{
				UserID:               "usr_1",
				APIKeyID:             "key_1",
				BillingSubjectUserID: "billing_1",
			},
		},
	}
	recorder := &recordingUsageRecorderFake{
		blockUntilDone: true,
	}
	clock := &recordingClockFake{
		now: time.Date(
			2026,
			time.June,
			14,
			12,
			0,
			0,
			0,
			time.UTC,
		),
	}

	authenticator, err :=
		NewUsageRecordingAuthenticator(
			next,
			recorder,
			clock,
			10*time.Millisecond,
		)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	_, err = authenticator.AuthenticatePublicRequest(
		context.Background(),
		Input{RawAPIKey: "sk_test_value"},
	)
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf(
			"timeout changed auth result: %v",
			err,
		)
	}
	if !errors.Is(
		recorder.waitErr,
		context.DeadlineExceeded,
	) {
		t.Fatalf(
			"recorder wait error=%v",
			recorder.waitErr,
		)
	}
	if elapsed > time.Second {
		t.Fatalf(
			"bounded recorder took too long: %s",
			elapsed,
		)
	}
}

func TestNewUsageRecordingAuthenticatorRejectsInvalidConfig(
	t *testing.T,
) {
	next := &recordingAuthenticatorFake{}
	recorder := &recordingUsageRecorderFake{}
	clock := &recordingClockFake{}

	tests := []struct {
		name     string
		next     PublicAuthenticator
		recorder ports.APIKeyUsageRecorder
		clock    ports.Clock
		timeout  time.Duration
	}{
		{
			name:     "missing next",
			recorder: recorder,
			clock:    clock,
			timeout:  time.Second,
		},
		{
			name:    "missing recorder",
			next:    next,
			clock:   clock,
			timeout: time.Second,
		},
		{
			name:     "missing clock",
			next:     next,
			recorder: recorder,
			timeout:  time.Second,
		},
		{
			name:     "zero timeout",
			next:     next,
			recorder: recorder,
			clock:    clock,
		},
		{
			name:     "negative timeout",
			next:     next,
			recorder: recorder,
			clock:    clock,
			timeout:  -time.Millisecond,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err :=
				NewUsageRecordingAuthenticator(
					test.next,
					test.recorder,
					test.clock,
					test.timeout,
				)
			if value != nil || err == nil {
				t.Fatalf(
					"value=%v error=%v",
					value,
					err,
				)
			}
		})
	}
}
