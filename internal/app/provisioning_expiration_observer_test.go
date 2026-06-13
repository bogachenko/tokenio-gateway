package app

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	provisioningapp "github.com/bogachenko/tokenio-gateway/internal/application/provisioning"
	provisioningexpiration "github.com/bogachenko/tokenio-gateway/internal/worker/provisioningexpiration"
)

func TestProvisioningExpirationLogObserverRecordsSafeFields(
	t *testing.T,
) {
	var output bytes.Buffer
	observer, err :=
		NewProvisioningExpirationLogObserver(
			log.New(&output, "", 0),
		)
	if err != nil {
		t.Fatal(err)
	}

	observer.ObserveProvisioningExpirationCycle(
		provisioningexpiration.Cycle{
			Result: provisioningapp.ExpireDueResult{
				AsOf: time.Date(
					2026,
					time.June,
					13,
					12,
					0,
					0,
					0,
					time.UTC,
				),
				Selected:        4,
				Expired:         2,
				AlreadyTerminal: 1,
				Failed:          1,
				FailedProvisioningIDs: []string{
					"prov_sensitive_identifier",
				},
			},
			Err: provisioningapp.ErrExpirationPartialFailure,
		},
	)

	line := output.String()
	for _, fragment := range []string{
		"error_code=partial_failure",
		"selected=4",
		"expired=2",
		"already_terminal=1",
		"failed=1",
	} {
		if !strings.Contains(line, fragment) {
			t.Fatalf(
				"log is missing %q: %s",
				fragment,
				line,
			)
		}
	}
	if strings.Contains(
		line,
		"prov_sensitive_identifier",
	) {
		t.Fatalf(
			"log contains provisioning identifier: %s",
			line,
		)
	}
}

func TestProvisioningExpirationLogObserverDoesNotLogRawError(
	t *testing.T,
) {
	var output bytes.Buffer
	observer, err :=
		NewProvisioningExpirationLogObserver(
			log.New(&output, "", 0),
		)
	if err != nil {
		t.Fatal(err)
	}

	observer.ObserveProvisioningExpirationCycle(
		provisioningexpiration.Cycle{
			Err: errors.New(
				"sk_live_secret_from_untrusted_error",
			),
		},
	)

	line := output.String()
	if !strings.Contains(
		line,
		"error_code=unexpected",
	) {
		t.Fatalf("unexpected log: %s", line)
	}
	if strings.Contains(line, "sk_live_secret") {
		t.Fatalf("raw error leaked into log: %s", line)
	}
}

func TestNewProvisioningExpirationLogObserverRejectsNilLogger(
	t *testing.T,
) {
	observer, err :=
		NewProvisioningExpirationLogObserver(nil)
	if observer != nil ||
		!errors.Is(
			err,
			ErrInvalidProvisioningExpirationObserver,
		) {
		t.Fatalf(
			"observer=%v error=%v",
			observer,
			err,
		)
	}
}
