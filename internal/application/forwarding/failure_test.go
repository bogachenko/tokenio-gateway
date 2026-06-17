package forwarding

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFailureContract(t *testing.T) {
	cause := errors.New("internal cause")
	err := NewFailure(FailureKindProvider5XX, 502, AttemptStateResponseReceived, true, cause)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Kind != FailureKindProvider5XX || failure.AttemptState != AttemptStateResponseReceived || !failure.RouteRetryCandidate {
		t.Fatalf("unexpected failure: %#v", failure)
	}
	if !errors.Is(err, cause) {
		t.Fatal("cause is not wrapped")
	}
	for _, secret := range []string{"route-123", "reseller-123", "APIKeyEnv", "Authorization", "request-body", "response-body", "sk_secret"} {
		if strings.Contains(fmt.Sprint(err), secret) {
			t.Fatalf("Error() leaked %q: %q", secret, err.Error())
		}
	}
}

func TestStableValues(t *testing.T) {
	kinds := map[FailureKind]string{
		FailureKindRequestError:                "request_error",
		FailureKindAuthError:                   "auth_error",
		FailureKindRateLimited:                 "rate_limited",
		FailureKindQuotaExceeded:               "quota_exceeded",
		FailureKindInsufficientResellerBalance: "insufficient_reseller_balance",
		FailureKindProvider5XX:                 "provider_5xx",
		FailureKindTimeout:                     "timeout",
		FailureKindConnectionError:             "connection_error",
		FailureKindUncertainProcessing:         "uncertain_processing",
		FailureKindMalformedResponse:           "malformed_response",
		FailureKindInvalidAdapterInput:         "invalid_adapter_input",
	}
	for got, want := range kinds {
		if string(got) != want {
			t.Fatalf("kind = %q want %q", got, want)
		}
	}
	states := map[AttemptState]string{
		AttemptStateNotSent:          "not_sent",
		AttemptStateSentNoResponse:   "sent_no_response",
		AttemptStateResponseReceived: "response_received",
	}
	for got, want := range states {
		if string(got) != want {
			t.Fatalf("state = %q want %q", got, want)
		}
	}
}

func TestNoForbiddenPublicFields(t *testing.T) {
	forbidden := map[string]struct{}{
		"RawBody": {}, "ResponseBody": {}, "RequestBody": {}, "Authorization": {},
		"ResellerAPIKey": {}, "APIKeyEnv": {}, "BillingJWT": {}, "ServiceToken": {},
	}
	for _, typ := range []reflect.Type{reflect.TypeOf(Classification{}), reflect.TypeOf(Failure{})} {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if _, found := forbidden[field.Name]; found {
				t.Fatalf("%s has forbidden field %s", typ.Name(), field.Name)
			}
		}
	}
}

func TestRetryAfterContract(t *testing.T) {
	delay, err := NewRetryAfterDelay(3 * time.Second)
	if err != nil {
		t.Fatalf("NewRetryAfterDelay: %v", err)
	}
	failure := NewFailureWithRetryAfter(
		FailureKindRateLimited,
		429,
		AttemptStateResponseReceived,
		true,
		delay,
		errors.New("internal"),
	)
	if !failure.FailureRetryAfterPresent() ||
		failure.FailureRetryAfterDelay() != 3*time.Second ||
		!failure.FailureRetryAfterTime().IsZero() {
		t.Fatalf("retry-after = %#v", failure.FailureRetryAfter())
	}

	at := time.Date(2026, time.June, 17, 18, 0, 0, 0, time.FixedZone("x", 3600))
	absolute, err := NewRetryAfterTime(at)
	if err != nil {
		t.Fatalf("NewRetryAfterTime: %v", err)
	}
	if !absolute.At().Equal(at.UTC()) ||
		absolute.At().Location() != time.UTC ||
		absolute.Delay() != 0 {
		t.Fatalf("absolute retry-after = %#v", absolute)
	}

	zero, err := NewRetryAfterDelay(0)
	if err != nil {
		t.Fatalf("NewRetryAfterDelay zero: %v", err)
	}
	if zero.IsZero() {
		t.Fatal("explicit zero retry-after lost presence")
	}

	if _, err := NewRetryAfterDelay(-time.Second); !errors.Is(
		err,
		ErrInvalidRetryAfter,
	) {
		t.Fatalf("negative delay error = %v", err)
	}
	if _, err := NewRetryAfterTime(time.Time{}); !errors.Is(
		err,
		ErrInvalidRetryAfter,
	) {
		t.Fatalf("zero time error = %v", err)
	}
}
