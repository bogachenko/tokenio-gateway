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

const AlertTypeResellerBalanceLow = "reseller_balance_low"

var (
	ErrInvalidInput       = errors.New("invalid Telegram alert input")
	ErrDependencyRequired = errors.New("Telegram alert dependency required")
	ErrResellerNotFound   = errors.New("reseller not found")
	ErrStoreUnavailable   = errors.New("Telegram alert store unavailable")
	ErrInvalidBalance     = errors.New("invalid reseller balance")
	ErrClockUnavailable   = errors.New("Telegram alert clock unavailable")
)

type ResellerReader interface {
	FindResellerByID(
		context.Context,
		string,
	) (*domain.Reseller, error)
}

type Config struct {
	ThresholdCents int64
	DedupePeriod   time.Duration
}

type CheckResult struct {
	ResellerID            string
	AvailableBalanceCents int64
	ThresholdCents        int64
	BelowThreshold        bool
	Alert                 *domain.TelegramAlert
}

type Service struct {
	resellers ResellerReader
	alerts    ports.TelegramAlertStore
	clock     ports.Clock
	config    Config
}

func NewService(
	resellers ResellerReader,
	alerts ports.TelegramAlertStore,
	clock ports.Clock,
	config Config,
) (*Service, error) {
	if resellers == nil || alerts == nil || clock == nil {
		return nil, ErrDependencyRequired
	}
	if config.ThresholdCents < 0 || config.DedupePeriod <= 0 {
		return nil, ErrInvalidInput
	}
	return &Service{
		resellers: resellers,
		alerts:    alerts,
		clock:     clock,
		config:    config,
	}, nil
}

func (s *Service) CheckReseller(
	ctx context.Context,
	resellerID string,
) (CheckResult, error) {
	if s == nil ||
		s.resellers == nil ||
		s.alerts == nil ||
		s.clock == nil {
		return CheckResult{}, ErrDependencyRequired
	}
	if ctx == nil || strings.TrimSpace(resellerID) == "" {
		return CheckResult{}, ErrInvalidInput
	}

	reseller, err := s.resellers.FindResellerByID(ctx, resellerID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return CheckResult{}, ErrResellerNotFound
		}
		return CheckResult{}, fmt.Errorf(
			"%w: find reseller: %v",
			ErrStoreUnavailable,
			err,
		)
	}
	if reseller == nil || reseller.ID != resellerID {
		return CheckResult{}, ErrResellerNotFound
	}
	if reseller.ReservedCents < 0 {
		return CheckResult{}, ErrInvalidBalance
	}

	available, err := checkedSub(
		reseller.BalanceCents,
		reseller.ReservedCents,
	)
	if err != nil {
		return CheckResult{}, err
	}

	result := CheckResult{
		ResellerID:            reseller.ID,
		AvailableBalanceCents: available,
		ThresholdCents:        s.config.ThresholdCents,
		BelowThreshold:        available <= s.config.ThresholdCents,
	}
	if !result.BelowThreshold {
		return result, nil
	}

	now := s.clock.Now()
	if now.IsZero() {
		return CheckResult{}, ErrClockUnavailable
	}
	now = now.UTC()

	requested := domain.TelegramAlert{
		ID:         stableAlertID(reseller.ID, available, now),
		AlertType:  AlertTypeResellerBalanceLow,
		DedupeKey:  reseller.ID,
		ResellerID: reseller.ID,
		Message: formatLowBalanceMessage(
			*reseller,
			available,
			s.config.ThresholdCents,
			now,
		),
		Status:    domain.TelegramAlertStatusPending,
		CreatedAt: now,
	}

	persisted, err := s.alerts.CreateOrSuppressTelegramAlert(
		ctx,
		requested,
		s.config.DedupePeriod,
	)
	if err != nil {
		return CheckResult{}, fmt.Errorf(
			"%w: persist alert: %v",
			ErrStoreUnavailable,
			err,
		)
	}
	if persisted.ID != requested.ID ||
		persisted.AlertType != requested.AlertType ||
		persisted.DedupeKey != requested.DedupeKey ||
		persisted.ResellerID != requested.ResellerID ||
		persisted.Message != requested.Message ||
		(persisted.Status != domain.TelegramAlertStatusPending &&
			persisted.Status != domain.TelegramAlertStatusSuppressed) {
		return CheckResult{}, fmt.Errorf(
			"%w: invalid persisted alert",
			ErrStoreUnavailable,
		)
	}

	result.Alert = &persisted
	return result, nil
}

func checkedSub(left int64, right int64) (int64, error) {
	if right > 0 && left < -1<<63+right {
		return 0, ErrInvalidBalance
	}
	return left - right, nil
}

func stableAlertID(
	resellerID string,
	availableBalanceCents int64,
	at time.Time,
) string {
	hash := sha256.New()
	for _, value := range []string{
		AlertTypeResellerBalanceLow,
		resellerID,
		strconv.FormatInt(availableBalanceCents, 10),
		at.UTC().Format(time.RFC3339Nano),
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return "tgalt_" + hex.EncodeToString(hash.Sum(nil))
}

func formatLowBalanceMessage(
	reseller domain.Reseller,
	availableBalanceCents int64,
	thresholdCents int64,
	at time.Time,
) string {
	return fmt.Sprintf(
		"Tokenio reseller balance alert\n"+
			"reseller_id: %s\n"+
			"provider_type: %s\n"+
			"available_balance_cents: %d\n"+
			"threshold_cents: %d\n"+
			"time: %s",
		reseller.ID,
		reseller.ProviderType,
		availableBalanceCents,
		thresholdCents,
		at.UTC().Format(time.RFC3339),
	)
}
