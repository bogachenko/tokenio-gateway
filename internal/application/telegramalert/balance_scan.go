package telegramalert

import (
	"context"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type ResellerLister interface {
	ListResellers(
		context.Context,
		ports.ResellerListFilter,
	) (ports.Page[domain.Reseller], error)
}

type BalanceChecker interface {
	CheckReseller(context.Context, string) (CheckResult, error)
}

type BalanceScanItemStatus string

const (
	BalanceScanItemBelowThreshold BalanceScanItemStatus = "below_threshold"
	BalanceScanItemAboveThreshold BalanceScanItemStatus = "above_threshold"
	BalanceScanItemFailed         BalanceScanItemStatus = "failed"
	BalanceScanItemSkipped        BalanceScanItemStatus = "skipped"
)

type BalanceScanItem struct {
	ResellerID string
	Status     BalanceScanItemStatus
}

type BalanceScanResult struct {
	Selected       int
	Checked        int
	BelowThreshold int
	Alerted        int
	Failed         int
	Skipped        int
	Total          int64
	NextOffset     int
	Finished       bool
	Items          []BalanceScanItem
}

type BalanceScanService struct {
	resellers ResellerLister
	checker   BalanceChecker
}

func NewBalanceScanService(
	resellers ResellerLister,
	checker BalanceChecker,
) (*BalanceScanService, error) {
	if resellers == nil || checker == nil {
		return nil, ErrDependencyRequired
	}
	return &BalanceScanService{
		resellers: resellers,
		checker:   checker,
	}, nil
}

func (s *BalanceScanService) ScanEnabledResellers(
	ctx context.Context,
	limit int,
	offset int,
) (BalanceScanResult, error) {
	if s == nil || s.resellers == nil || s.checker == nil {
		return BalanceScanResult{}, ErrDependencyRequired
	}
	if ctx == nil || limit <= 0 || offset < 0 {
		return BalanceScanResult{}, ErrInvalidInput
	}

	enabled := true
	page, err := s.resellers.ListResellers(
		ctx,
		ports.ResellerListFilter{
			Enabled: &enabled,
			Page: ports.PageRequest{
				Limit:  limit,
				Offset: offset,
			},
		},
	)
	if err != nil {
		return BalanceScanResult{}, fmt.Errorf(
			"%w: list enabled resellers: %v",
			ErrStoreUnavailable,
			err,
		)
	}
	if len(page.Items) > limit {
		return BalanceScanResult{}, fmt.Errorf(
			"%w: scan page exceeds limit",
			ErrStoreUnavailable,
		)
	}

	result := BalanceScanResult{
		Selected:   len(page.Items),
		Total:      page.Total,
		NextOffset: offset + len(page.Items),
		Finished:   int64(offset+len(page.Items)) >= page.Total,
		Items:      make([]BalanceScanItem, 0, len(page.Items)),
	}
	if len(page.Items) == 0 {
		result.Finished = true
		return result, nil
	}

	for _, reseller := range page.Items {
		if reseller.ID == "" || !reseller.Enabled {
			result.Skipped++
			result.Items = append(result.Items, BalanceScanItem{
				ResellerID: reseller.ID,
				Status:     BalanceScanItemSkipped,
			})
			continue
		}

		check, err := s.checker.CheckReseller(ctx, reseller.ID)
		if err != nil {
			result.Failed++
			result.Items = append(result.Items, BalanceScanItem{
				ResellerID: reseller.ID,
				Status:     BalanceScanItemFailed,
			})
			continue
		}
		result.Checked++
		status := BalanceScanItemAboveThreshold
		if check.BelowThreshold {
			status = BalanceScanItemBelowThreshold
			result.BelowThreshold++
		}
		if check.Alert != nil &&
			check.Alert.Status == domain.TelegramAlertStatusPending {
			result.Alerted++
		}
		result.Items = append(result.Items, BalanceScanItem{
			ResellerID: reseller.ID,
			Status:     status,
		})
	}
	return result, nil
}
