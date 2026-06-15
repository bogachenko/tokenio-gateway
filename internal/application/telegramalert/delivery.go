package telegramalert

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const DeliveryErrorUnavailable = "telegram_unavailable"

var (
	ErrAlertNotFound          = errors.New("Telegram alert not found")
	ErrAlertNotPending        = errors.New("Telegram alert is not pending")
	ErrDeliveryFailed         = errors.New("Telegram delivery failed")
	ErrDeliveryStateUncertain = errors.New("Telegram delivery state uncertain")
)

type DeliveryResult struct {
	Alert domain.TelegramAlert
	Sent  bool
}

type DeliveryService struct {
	alerts ports.TelegramAlertStore
	sender MessageSender
	clock  ports.Clock
}

func NewDeliveryService(
	alerts ports.TelegramAlertStore,
	sender MessageSender,
	clock ports.Clock,
) (*DeliveryService, error) {
	if alerts == nil || sender == nil || clock == nil {
		return nil, ErrDependencyRequired
	}
	return &DeliveryService{alerts: alerts, sender: sender, clock: clock}, nil
}

func (s *DeliveryService) Deliver(
	ctx context.Context,
	alertID string,
) (DeliveryResult, error) {
	if s == nil || s.alerts == nil || s.sender == nil || s.clock == nil {
		return DeliveryResult{}, ErrDependencyRequired
	}
	if ctx == nil || strings.TrimSpace(alertID) == "" {
		return DeliveryResult{}, ErrInvalidInput
	}

	current, err := s.alerts.FindTelegramAlertByID(ctx, alertID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return DeliveryResult{}, ErrAlertNotFound
		}
		return DeliveryResult{}, fmt.Errorf("%w: load alert", ErrStoreUnavailable)
	}
	if current == nil || current.ID != alertID {
		return DeliveryResult{}, ErrAlertNotFound
	}
	if current.Status != domain.TelegramAlertStatusPending {
		return DeliveryResult{
			Alert: *current,
			Sent:  current.Status == domain.TelegramAlertStatusSent,
		}, ErrAlertNotPending
	}

	if err := s.sender.SendMessage(ctx, current.Message); err != nil {
		next := *current
		next.Status = domain.TelegramAlertStatusFailed
		next.Error = DeliveryErrorUnavailable
		next.SentAt = nil

		persisted, storeErr := s.alerts.CompareAndSwapTelegramAlert(
			ctx,
			*current,
			next,
		)
		if storeErr != nil || !validFailedDelivery(persisted, next) {
			return DeliveryResult{}, fmt.Errorf(
				"%w: persist failed delivery",
				ErrDeliveryStateUncertain,
			)
		}
		return DeliveryResult{Alert: persisted}, ErrDeliveryFailed
	}

	sentAt := s.clock.Now()
	if sentAt.IsZero() {
		return DeliveryResult{}, ErrDeliveryStateUncertain
	}
	sentAt = sentAt.UTC()

	next := *current
	next.Status = domain.TelegramAlertStatusSent
	next.Error = ""
	next.SentAt = &sentAt

	persisted, err := s.alerts.CompareAndSwapTelegramAlert(
		ctx,
		*current,
		next,
	)
	if err != nil || !validSentDelivery(persisted, next, sentAt) {
		return DeliveryResult{}, fmt.Errorf(
			"%w: persist sent delivery",
			ErrDeliveryStateUncertain,
		)
	}
	return DeliveryResult{Alert: persisted, Sent: true}, nil
}

func validFailedDelivery(
	persisted domain.TelegramAlert,
	expected domain.TelegramAlert,
) bool {
	return sameDeliveryIdentity(persisted, expected) &&
		persisted.Status == domain.TelegramAlertStatusFailed &&
		persisted.Error == DeliveryErrorUnavailable &&
		persisted.SentAt == nil
}

func validSentDelivery(
	persisted domain.TelegramAlert,
	expected domain.TelegramAlert,
	sentAt time.Time,
) bool {
	return sameDeliveryIdentity(persisted, expected) &&
		persisted.Status == domain.TelegramAlertStatusSent &&
		persisted.Error == "" &&
		persisted.SentAt != nil &&
		persisted.SentAt.Equal(sentAt)
}

func sameDeliveryIdentity(
	left domain.TelegramAlert,
	right domain.TelegramAlert,
) bool {
	return left.ID == right.ID &&
		left.AlertType == right.AlertType &&
		left.DedupeKey == right.DedupeKey &&
		left.ResellerID == right.ResellerID &&
		left.RouteID == right.RouteID &&
		left.Message == right.Message &&
		left.CreatedAt.Equal(right.CreatedAt)
}
