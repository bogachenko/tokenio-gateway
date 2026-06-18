package telegramalert

import (
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type resellerListerFake struct {
	page   ports.Page[domain.Reseller]
	err    error
	filter ports.ResellerListFilter
}

func (f *resellerListerFake) ListResellers(
	_ context.Context,
	filter ports.ResellerListFilter,
) (ports.Page[domain.Reseller], error) {
	f.filter = filter
	if f.err != nil {
		return ports.Page[domain.Reseller]{}, f.err
	}
	return f.page, nil
}

type balanceCheckerFake struct {
	results map[string]CheckResult
	errs    map[string]error
	calls   []string
}

func (f *balanceCheckerFake) CheckReseller(
	_ context.Context,
	resellerID string,
) (CheckResult, error) {
	f.calls = append(f.calls, resellerID)
	if err := f.errs[resellerID]; err != nil {
		return CheckResult{}, err
	}
	return f.results[resellerID], nil
}

func TestBalanceScanUsesEnabledResellerPageAndSharedChecker(
	t *testing.T,
) {
	lister := &resellerListerFake{
		page: ports.Page[domain.Reseller]{
			Items: []domain.Reseller{
				{ID: "r1", Enabled: true},
				{ID: "r2", Enabled: true},
				{ID: "disabled", Enabled: false},
			},
			Total: 5,
		},
	}
	checker := &balanceCheckerFake{
		results: map[string]CheckResult{
			"r1": {
				ResellerID:     "r1",
				BelowThreshold: true,
				Alert: &domain.TelegramAlert{
					Status: domain.TelegramAlertStatusPending,
				},
			},
			"r2": {
				ResellerID:     "r2",
				BelowThreshold: false,
			},
		},
		errs: map[string]error{},
	}
	service, err := NewBalanceScanService(lister, checker)
	if err != nil {
		t.Fatalf("NewBalanceScanService: %v", err)
	}

	result, err := service.ScanEnabledResellers(
		context.Background(),
		3,
		2,
	)
	if err != nil {
		t.Fatalf("ScanEnabledResellers: %v", err)
	}

	if lister.filter.Enabled == nil ||
		!*lister.filter.Enabled ||
		lister.filter.Page.Limit != 3 ||
		lister.filter.Page.Offset != 2 {
		t.Fatalf("filter = %#v", lister.filter)
	}
	if len(checker.calls) != 2 ||
		checker.calls[0] != "r1" ||
		checker.calls[1] != "r2" {
		t.Fatalf("calls = %#v", checker.calls)
	}
	if result.Selected != 3 ||
		result.Checked != 2 ||
		result.BelowThreshold != 1 ||
		result.Alerted != 1 ||
		result.Skipped != 1 ||
		result.Failed != 0 ||
		result.NextOffset != 5 ||
		!result.Finished {
		t.Fatalf("result = %#v", result)
	}
}

func TestBalanceScanContinuesAfterPerResellerFailure(t *testing.T) {
	lister := &resellerListerFake{
		page: ports.Page[domain.Reseller]{
			Items: []domain.Reseller{
				{ID: "r1", Enabled: true},
				{ID: "r2", Enabled: true},
			},
			Total: 4,
		},
	}
	checker := &balanceCheckerFake{
		results: map[string]CheckResult{
			"r2": {ResellerID: "r2"},
		},
		errs: map[string]error{
			"r1": errors.New("temporary"),
		},
	}
	service, err := NewBalanceScanService(lister, checker)
	if err != nil {
		t.Fatalf("NewBalanceScanService: %v", err)
	}

	result, err := service.ScanEnabledResellers(
		context.Background(),
		2,
		0,
	)
	if err != nil {
		t.Fatalf("ScanEnabledResellers: %v", err)
	}

	if result.Selected != 2 ||
		result.Checked != 1 ||
		result.Failed != 1 ||
		result.NextOffset != 2 ||
		result.Finished {
		t.Fatalf("result = %#v", result)
	}
}
