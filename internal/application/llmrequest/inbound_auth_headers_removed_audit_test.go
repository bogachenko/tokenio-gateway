package llmrequest

import (
	"reflect"
	"strings"
	"testing"
)

func TestForwardingStageCannotReceiveInboundTokenioAuthHeaders(t *testing.T) {
	preparedType := reflect.TypeOf(PreparedRequest{})

	for _, forbidden := range []string{
		"rawapikey",
		"authorization",
		"xapikey",
		"xgoogapikey",
		"token",
		"credential",
		"header",
		"headers",
	} {
		if structFieldNameContains(preparedType, forbidden) {
			t.Fatalf(
				"PreparedRequest must not carry inbound auth/header material; found field containing %q in %s",
				forbidden,
				preparedType,
			)
		}
	}

	executorType := reflect.TypeOf((*ForwardingStageExecutor)(nil)).Elem()
	method, ok := executorType.MethodByName("Execute")
	if !ok {
		t.Fatal("ForwardingStageExecutor.Execute not found")
	}
	if method.Type.NumIn() != 3 {
		t.Fatalf("Execute argument count = %d, want 3", method.Type.NumIn())
	}
	if got := method.Type.In(1); got != preparedType {
		t.Fatalf("Execute prepared argument = %s, want %s", got, preparedType)
	}
}

func TestPublicRawAPIKeyStopsAtAuthenticateInput(t *testing.T) {
	inputType := reflect.TypeOf(Input{})
	if !structFieldNameContains(inputType, "rawapikey") {
		t.Fatalf("Input must carry RawAPIKey only for public authentication")
	}

	preparedType := reflect.TypeOf(PreparedRequest{})
	if structFieldNameContains(preparedType, "rawapikey") {
		t.Fatalf("PreparedRequest must not carry RawAPIKey into forwarding")
	}
}

func structFieldNameContains(value reflect.Type, needle string) bool {
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		if strings.Contains(strings.ToLower(field.Name), needle) {
			return true
		}
	}
	return false
}
