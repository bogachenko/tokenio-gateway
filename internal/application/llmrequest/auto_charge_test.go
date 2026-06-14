package llmrequest

import (
	"context"
	"errors"
	"testing"
)

type recordingAutoCharger struct {
	result AutoChargeResult
	calls  int
	input  AutoChargeInput
}

func (a *recordingAutoCharger) Run(
	_ context.Context,
	input AutoChargeInput,
) AutoChargeResult {
	a.calls++
	a.input = input
	return a.result
}

func TestServiceExecuteAutoChargeFailureDoesNotReplaceSuccess(
	t *testing.T,
) {
	dependencies := validDependencies(nil)
	autoCharger := &recordingAutoCharger{
		result: AutoChargeResult{
			Status: AutoChargeStatusFailed,
		},
	}
	dependencies.AutoCharger = autoCharger

	service, err := NewService(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(
		context.Background(),
		validInput(),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if autoCharger.calls != 1 {
		t.Fatalf("auto-charge calls=%d", autoCharger.calls)
	}
	if autoCharger.input.FinalUsageRecord.LocalRequestID !=
		result.FinalUsageRecord.LocalRequestID {
		t.Fatalf(
			"auto-charge input=%+v result=%+v",
			autoCharger.input,
			result,
		)
	}
	if result.AutoCharge.Status != AutoChargeStatusFailed {
		t.Fatalf("result=%+v", result)
	}
}

func TestNewServiceRequiresAutoCharger(t *testing.T) {
	dependencies := validDependencies(nil)
	dependencies.AutoCharger = nil
	_, err := NewService(dependencies)
	if !errors.Is(err, ErrDependencyRequired) {
		t.Fatalf("error=%v", err)
	}
}
