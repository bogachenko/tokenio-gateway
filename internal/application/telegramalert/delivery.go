package telegramalert

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const (
	DeliveryErrorUnavailable = "telegram_unavailable"

	deliveryAttemptFailureNotSent        = "telegram_not_sent"
	deliveryAttemptFailureRejected       = "telegram_rejected"
	deliveryAttemptFailureSentNoResponse = "telegram_delivery_uncertain"
	maxDeliveryAttemptsPerAlert          = 1000
)

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
	alerts   ports.TelegramAlertStore
	attempts ports.TelegramDeliveryAttemptStore
	sender   MessageSender
	clock    ports.Clock
}

func NewDeliveryService(
	alerts ports.TelegramAlertStore,
	attempts ports.TelegramDeliveryAttemptStore,
	sender MessageSender,
	clock ports.Clock,
) (*DeliveryService, error) {
	if alerts == nil || attempts == nil || sender == nil || clock == nil {
		return nil, ErrDependencyRequired
	}
	return &DeliveryService{
		alerts:   alerts,
		attempts: attempts,
		sender:   sender,
		clock:    clock,
	}, nil
}

func (s *DeliveryService) Deliver(
	ctx context.Context,
	alertID string,
) (DeliveryResult, error) {
	if s == nil ||
		s.alerts == nil ||
		s.attempts == nil ||
		s.sender == nil ||
		s.clock == nil {
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

	history, err := s.attempts.LoadTelegramDeliveryAttempts(
		ctx,
		alertID,
		maxDeliveryAttemptsPerAlert,
	)
	if err != nil {
		return DeliveryResult{}, fmt.Errorf(
			"%w: load delivery attempts",
			ErrStoreUnavailable,
		)
	}
	nextNumber, err := nextDeliveryAttemptNumber(history, alertID)
	if err != nil {
		return DeliveryResult{}, ErrDeliveryStateUncertain
	}

	startedAt := s.clock.Now()
	if startedAt.IsZero() {
		return DeliveryResult{}, ErrDeliveryStateUncertain
	}
	startedAt = startedAt.UTC()

	started := domain.TelegramDeliveryAttempt{
		ID:            deliveryAttemptID(alertID, nextNumber),
		AlertID:       alertID,
		AttemptNumber: nextNumber,
		Status:        domain.TelegramDeliveryAttemptStatusStarted,
		StartedAt:     startedAt,
	}
	persistedStarted, err := s.attempts.StartTelegramDeliveryAttempt(
		ctx,
		started,
	)
	if err != nil || !sameStartedDeliveryAttempt(persistedStarted, started) {
		return DeliveryResult{}, fmt.Errorf(
			"%w: persist started delivery attempt",
			ErrDeliveryStateUncertain,
		)
	}

	sendResult, sendErr := s.sender.SendMessage(ctx, current.Message)
	completedAt := s.clock.Now()
	if completedAt.IsZero() {
		return DeliveryResult{}, ErrDeliveryStateUncertain
	}
	completedAt = completedAt.UTC()
	if completedAt.Before(persistedStarted.StartedAt) {
		return DeliveryResult{}, ErrDeliveryStateUncertain
	}

	terminal, terminalErr := terminalDeliveryAttempt(
		persistedStarted,
		sendResult,
		sendErr,
		completedAt,
	)
	if terminalErr != nil {
		return DeliveryResult{}, ErrDeliveryStateUncertain
	}
	persistedTerminal, err := s.attempts.CompleteTelegramDeliveryAttempt(
		ctx,
		terminal,
	)
	if err != nil ||
		!sameTerminalDeliveryAttempt(persistedTerminal, terminal) {
		return DeliveryResult{}, fmt.Errorf(
			"%w: persist terminal delivery attempt",
			ErrDeliveryStateUncertain,
		)
	}

	switch {
	case persistedTerminal.Status ==
		domain.TelegramDeliveryAttemptStatusSucceeded:
		return s.persistSentAlert(ctx, *current, completedAt)
	case persistedTerminal.AttemptState ==
		domain.TelegramDeliveryAttemptStateSentNoResponse:
		return DeliveryResult{}, ErrDeliveryStateUncertain
	default:
		return s.persistFailedAlert(ctx, *current)
	}
}

func (s *DeliveryService) persistSentAlert(
	ctx context.Context,
	current domain.TelegramAlert,
	sentAt time.Time,
) (DeliveryResult, error) {
	next := current
	next.Status = domain.TelegramAlertStatusSent
	next.Error = ""
	next.SentAt = &sentAt

	persisted, err := s.alerts.CompareAndSwapTelegramAlert(
		ctx,
		current,
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

func (s *DeliveryService) persistFailedAlert(
	ctx context.Context,
	current domain.TelegramAlert,
) (DeliveryResult, error) {
	next := current
	next.Status = domain.TelegramAlertStatusFailed
	next.Error = DeliveryErrorUnavailable
	next.SentAt = nil

	persisted, err := s.alerts.CompareAndSwapTelegramAlert(
		ctx,
		current,
		next,
	)
	if err != nil || !validFailedDelivery(persisted, next) {
		return DeliveryResult{}, fmt.Errorf(
			"%w: persist failed delivery",
			ErrDeliveryStateUncertain,
		)
	}
	return DeliveryResult{Alert: persisted}, ErrDeliveryFailed
}

func nextDeliveryAttemptNumber(
	history []domain.TelegramDeliveryAttempt,
	alertID string,
) (int, error) {
	if len(history) >= maxDeliveryAttemptsPerAlert {
		return 0, ErrDeliveryStateUncertain
	}
	for index, attempt := range history {
		expectedNumber := index + 1
		if attempt.AlertID != alertID ||
			attempt.AttemptNumber != expectedNumber ||
			attempt.ID == "" ||
			attempt.StartedAt.IsZero() {
			return 0, ErrDeliveryStateUncertain
		}
		switch attempt.Status {
		case domain.TelegramDeliveryAttemptStatusSucceeded:
			return 0, ErrDeliveryStateUncertain
		case domain.TelegramDeliveryAttemptStatusStarted:
			return 0, ErrDeliveryStateUncertain
		case domain.TelegramDeliveryAttemptStatusFailed:
			if attempt.AttemptState ==
				domain.TelegramDeliveryAttemptStateSentNoResponse {
				return 0, ErrDeliveryStateUncertain
			}
		default:
			return 0, ErrDeliveryStateUncertain
		}
	}
	return len(history) + 1, nil
}

func terminalDeliveryAttempt(
	started domain.TelegramDeliveryAttempt,
	result MessageDeliveryResult,
	sendErr error,
	completedAt time.Time,
) (domain.TelegramDeliveryAttempt, error) {
	terminal := started
	terminal.CompletedAt = &completedAt

	if sendErr == nil {
		if result.Outcome != MessageDeliveryOutcomeResponseReceived {
			return domain.TelegramDeliveryAttempt{},
				ErrDeliveryStateUncertain
		}
		terminal.Status =
			domain.TelegramDeliveryAttemptStatusSucceeded
		terminal.AttemptState =
			domain.TelegramDeliveryAttemptStateResponseReceived
		terminal.TelegramMessageID = strings.TrimSpace(result.TelegramMessageID)
		return terminal, nil
	}

	terminal.Status = domain.TelegramDeliveryAttemptStatusFailed
	switch result.Outcome {
	case MessageDeliveryOutcomeNotSent:
		terminal.AttemptState =
			domain.TelegramDeliveryAttemptStateNotSent
		terminal.FailureCode = deliveryAttemptFailureNotSent
	case MessageDeliveryOutcomeResponseReceived:
		terminal.AttemptState =
			domain.TelegramDeliveryAttemptStateResponseReceived
		terminal.FailureCode = deliveryAttemptFailureRejected
	case MessageDeliveryOutcomeSentNoResponse:
		terminal.AttemptState =
			domain.TelegramDeliveryAttemptStateSentNoResponse
		terminal.FailureCode =
			deliveryAttemptFailureSentNoResponse
	default:
		return domain.TelegramDeliveryAttempt{},
			ErrDeliveryStateUncertain
	}
	return terminal, nil
}

func deliveryAttemptID(alertID string, attemptNumber int) string {
	sum := sha256.Sum256([]byte(
		alertID + "\x00" + strconv.Itoa(attemptNumber),
	))
	return "tgattempt_" + hex.EncodeToString(sum[:16])
}

func sameStartedDeliveryAttempt(
	left domain.TelegramDeliveryAttempt,
	right domain.TelegramDeliveryAttempt,
) bool {
	return left.ID == right.ID &&
		left.AlertID == right.AlertID &&
		left.AttemptNumber == right.AttemptNumber &&
		left.Status == domain.TelegramDeliveryAttemptStatusStarted &&
		right.Status == domain.TelegramDeliveryAttemptStatusStarted &&
		left.AttemptState == "" &&
		right.AttemptState == "" &&
		left.FailureCode == "" &&
		right.FailureCode == "" &&
		left.TelegramMessageID == "" &&
		right.TelegramMessageID == "" &&
		left.StartedAt.Equal(right.StartedAt) &&
		left.CompletedAt == nil &&
		right.CompletedAt == nil
}

func sameTerminalDeliveryAttempt(
	left domain.TelegramDeliveryAttempt,
	right domain.TelegramDeliveryAttempt,
) bool {
	return left.ID == right.ID &&
		left.AlertID == right.AlertID &&
		left.AttemptNumber == right.AttemptNumber &&
		left.Status == right.Status &&
		left.AttemptState == right.AttemptState &&
		left.FailureCode == right.FailureCode &&
		left.TelegramMessageID == right.TelegramMessageID &&
		left.StartedAt.Equal(right.StartedAt) &&
		equalDeliveryTimes(left.CompletedAt, right.CompletedAt)
}

func equalDeliveryTimes(left *time.Time, right *time.Time) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.Equal(*right)
	}
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
