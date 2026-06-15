package telegramalert

import (
	"context"
	"errors"
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type AlertDeliverer interface {
	Deliver(context.Context, string) (DeliveryResult, error)
}

type RecoveryItemStatus string

const (
	RecoveryItemStatusSent           RecoveryItemStatus = "sent"
	RecoveryItemStatusDeliveryFailed RecoveryItemStatus = "delivery_failed"
	RecoveryItemStatusCASConflict    RecoveryItemStatus = "cas_conflict"
	RecoveryItemStatusStateUncertain RecoveryItemStatus = "state_uncertain"
)

type RecoveryItem struct {
	AlertID string
	Status  RecoveryItemStatus
}

type RecoveryResult struct {
	Selected int
	Retried  int
	Sent     int
	Items    []RecoveryItem
}

type RecoveryService struct {
	alerts    ports.TelegramAlertStore
	deliverer AlertDeliverer
}

func NewRecoveryService(
	alerts ports.TelegramAlertStore,
	deliverer AlertDeliverer,
) (*RecoveryService, error) {
	if alerts == nil || deliverer == nil {
		return nil, ErrDependencyRequired
	}
	return &RecoveryService{
		alerts:    alerts,
		deliverer: deliverer,
	}, nil
}

func (s *RecoveryService) RecoverFailed(
	ctx context.Context,
	limit int,
) (RecoveryResult, error) {
	if s == nil || s.alerts == nil || s.deliverer == nil {
		return RecoveryResult{}, ErrDependencyRequired
	}
	if ctx == nil || limit <= 0 {
		return RecoveryResult{}, ErrInvalidInput
	}

	page, err := s.alerts.ListTelegramAlerts(
		ctx,
		ports.TelegramAlertListFilter{
			Status: domain.TelegramAlertStatusFailed,
			Page: ports.PageRequest{
				Limit:  limit,
				Offset: 0,
			},
		},
	)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf(
			"%w: list failed alerts",
			ErrStoreUnavailable,
		)
	}
	if len(page.Items) > limit {
		return RecoveryResult{}, fmt.Errorf(
			"%w: recovery page exceeds limit",
			ErrStoreUnavailable,
		)
	}

	result := RecoveryResult{
		Selected: len(page.Items),
		Items:    make([]RecoveryItem, 0, len(page.Items)),
	}

	for _, failed := range page.Items {
		if failed.Status != domain.TelegramAlertStatusFailed ||
			failed.ID == "" ||
			failed.Error == "" ||
			failed.SentAt != nil {
			result.Items = append(result.Items, RecoveryItem{
				AlertID: failed.ID,
				Status:  RecoveryItemStatusStateUncertain,
			})
			continue
		}

		pending := failed
		pending.Status = domain.TelegramAlertStatusPending
		pending.Error = ""
		pending.SentAt = nil

		persisted, err := s.alerts.CompareAndSwapTelegramAlert(
			ctx,
			failed,
			pending,
		)
		if err != nil {
			status := RecoveryItemStatusStateUncertain
			if errors.Is(err, ports.ErrStoreConflict) {
				status = RecoveryItemStatusCASConflict
			}
			result.Items = append(result.Items, RecoveryItem{
				AlertID: failed.ID,
				Status:  status,
			})
			continue
		}
		if !sameDeliveryIdentity(persisted, pending) ||
			persisted.Status != domain.TelegramAlertStatusPending ||
			persisted.Error != "" ||
			persisted.SentAt != nil {
			result.Items = append(result.Items, RecoveryItem{
				AlertID: failed.ID,
				Status:  RecoveryItemStatusStateUncertain,
			})
			continue
		}

		result.Retried++
		_, deliveryErr := s.deliverer.Deliver(ctx, persisted.ID)
		switch {
		case deliveryErr == nil:
			result.Sent++
			result.Items = append(result.Items, RecoveryItem{
				AlertID: persisted.ID,
				Status:  RecoveryItemStatusSent,
			})
		case errors.Is(deliveryErr, ErrDeliveryFailed):
			result.Items = append(result.Items, RecoveryItem{
				AlertID: persisted.ID,
				Status:  RecoveryItemStatusDeliveryFailed,
			})
		default:
			result.Items = append(result.Items, RecoveryItem{
				AlertID: persisted.ID,
				Status:  RecoveryItemStatusStateUncertain,
			})
		}
	}

	return result, nil
}
