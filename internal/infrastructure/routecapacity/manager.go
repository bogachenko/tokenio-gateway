package routecapacity

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const accountingWindow = time.Minute

type Manager struct {
	mu sync.Mutex

	clock ports.Clock

	lastNow time.Time
	routes  map[string]map[string]*reservationEntry
	index   map[string]*reservationEntry
}

type reservationEntry struct {
	reservation ports.RouteCapacityReservation
	acquiredAt  time.Time
	tokens      int64
	active      bool
}

var _ ports.RouteCapacityManager = (*Manager)(nil)

func NewManager(clock ports.Clock) (*Manager, error) {
	if clock == nil {
		return nil, fmt.Errorf("%w: nil clock", ErrInvalidInput)
	}
	return &Manager{
		clock:  clock,
		routes: make(map[string]map[string]*reservationEntry),
		index:  make(map[string]*reservationEntry),
	}, nil
}

func (m *Manager) Check(
	ctx context.Context,
	input ports.RouteCapacityCheckInput,
) (ports.RouteCapacityResult, error) {
	if m == nil || m.clock == nil {
		return ports.RouteCapacityResult{},
			fmt.Errorf("%w: nil manager", ErrInvalidInput)
	}
	if err := validateContext(ctx); err != nil {
		return ports.RouteCapacityResult{}, err
	}
	tokens, err := validateCapacityInput(
		input.Route,
		input.Reseller,
		input.EstimatedUsage,
	)
	if err != nil {
		return ports.RouteCapacityResult{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now, err := m.nowLocked()
	if err != nil {
		return ports.RouteCapacityResult{}, err
	}
	m.pruneLocked(now)

	return m.checkLocked(input.Route, tokens, now), nil
}

func (m *Manager) Acquire(
	ctx context.Context,
	input ports.RouteCapacityAcquireInput,
) (ports.RouteCapacityReservation, error) {
	if m == nil || m.clock == nil {
		return ports.RouteCapacityReservation{},
			fmt.Errorf("%w: nil manager", ErrInvalidInput)
	}
	if err := validateContext(ctx); err != nil {
		return ports.RouteCapacityReservation{}, err
	}
	if strings.TrimSpace(input.LocalRequestID) == "" {
		return ports.RouteCapacityReservation{},
			fmt.Errorf("%w: blank local request id", ErrInvalidInput)
	}
	tokens, err := validateCapacityInput(
		input.Route,
		input.Reseller,
		input.EstimatedUsage,
	)
	if err != nil {
		return ports.RouteCapacityReservation{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now, err := m.nowLocked()
	if err != nil {
		return ports.RouteCapacityReservation{}, err
	}
	m.pruneLocked(now)

	if existing, ok := m.index[input.LocalRequestID]; ok {
		if existing.reservation.RouteID != input.Route.ID ||
			existing.tokens != tokens {
			return ports.RouteCapacityReservation{}, fmt.Errorf(
				"%w: local request id %q",
				ErrReservationConflict,
				input.LocalRequestID,
			)
		}
		return existing.reservation, nil
	}

	availability := m.checkLocked(input.Route, tokens, now)
	if !availability.RateLimitAllowed ||
		!availability.ConcurrencyAllowed {
		return ports.RouteCapacityReservation{},
			ErrCapacityUnavailable
	}

	reservation := ports.RouteCapacityReservation{
		LocalRequestID: input.LocalRequestID,
		RouteID:        input.Route.ID,
	}
	entry := &reservationEntry{
		reservation: reservation,
		acquiredAt:  now,
		tokens:      tokens,
		active:      true,
	}
	routeEntries := m.routes[input.Route.ID]
	if routeEntries == nil {
		routeEntries = make(map[string]*reservationEntry)
		m.routes[input.Route.ID] = routeEntries
	}
	routeEntries[input.LocalRequestID] = entry
	m.index[input.LocalRequestID] = entry

	return reservation, nil
}

func (m *Manager) Release(
	ctx context.Context,
	reservation ports.RouteCapacityReservation,
) error {
	if m == nil || m.clock == nil {
		return fmt.Errorf("%w: nil manager", ErrInvalidInput)
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(reservation.LocalRequestID) == "" ||
		strings.TrimSpace(reservation.RouteID) == "" {
		return fmt.Errorf(
			"%w: invalid reservation identity",
			ErrInvalidInput,
		)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now, err := m.nowLocked()
	if err != nil {
		return err
	}
	m.pruneLocked(now)

	entry, ok := m.index[reservation.LocalRequestID]
	if !ok {
		return nil
	}
	if entry.reservation.RouteID != reservation.RouteID {
		return fmt.Errorf(
			"%w: local request id %q",
			ErrReservationConflict,
			reservation.LocalRequestID,
		)
	}

	entry.active = false
	m.pruneLocked(now)
	return nil
}

func (m *Manager) checkLocked(
	route domain.Route,
	tokens int64,
	now time.Time,
) ports.RouteCapacityResult {
	requests, usedTokens, active := m.usageLocked(route.ID, now)

	rateAllowed := true
	if route.RequestsPerMinute > 0 {
		rateAllowed = requests < int64(route.RequestsPerMinute)
	}
	if rateAllowed && route.TokensPerMinute > 0 {
		limit := int64(route.TokensPerMinute)
		rateAllowed = tokens <= limit-usedTokens
	}

	concurrencyAllowed := true
	if route.ConcurrentRequests > 0 {
		concurrencyAllowed =
			active < int64(route.ConcurrentRequests)
	}

	return ports.RouteCapacityResult{
		RateLimitAllowed:   rateAllowed,
		ConcurrencyAllowed: concurrencyAllowed,
	}
}

func (m *Manager) usageLocked(
	routeID string,
	now time.Time,
) (requests int64, tokens int64, active int64) {
	cutoff := now.Add(-accountingWindow)
	for _, entry := range m.routes[routeID] {
		if entry.active {
			active++
		}
		if !entry.acquiredAt.After(cutoff) {
			continue
		}
		requests++
		tokens += entry.tokens
	}
	return requests, tokens, active
}

func (m *Manager) pruneLocked(now time.Time) {
	cutoff := now.Add(-accountingWindow)
	for routeID, entries := range m.routes {
		for requestID, entry := range entries {
			if entry.active || entry.acquiredAt.After(cutoff) {
				continue
			}
			delete(entries, requestID)
			delete(m.index, requestID)
		}
		if len(entries) == 0 {
			delete(m.routes, routeID)
		}
	}
}

func (m *Manager) nowLocked() (time.Time, error) {
	now := m.clock.Now()
	if now.IsZero() {
		return time.Time{}, fmt.Errorf(
			"%w: zero clock value",
			ErrInvalidInput,
		)
	}
	if !m.lastNow.IsZero() && now.Before(m.lastNow) {
		now = m.lastNow
	}
	m.lastNow = now
	return now, nil
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", ErrInvalidInput)
	}
	return ctx.Err()
}

func validateCapacityInput(
	route domain.Route,
	reseller domain.Reseller,
	usage domain.TokenUsage,
) (int64, error) {
	if strings.TrimSpace(route.ID) == "" ||
		strings.TrimSpace(route.ResellerID) == "" ||
		strings.TrimSpace(reseller.ID) == "" ||
		route.ResellerID != reseller.ID ||
		route.ProviderType == "" ||
		route.ProviderType != reseller.ProviderType ||
		route.RequestsPerMinute < 0 ||
		route.TokensPerMinute < 0 ||
		route.ConcurrentRequests < 0 {
		return 0, fmt.Errorf("%w: invalid route data", ErrInvalidInput)
	}
	return totalTokens(usage)
}

func totalTokens(usage domain.TokenUsage) (int64, error) {
	values := [...]int64{
		usage.InputTokens,
		usage.CachedInputTokens,
		usage.OutputTokens,
		usage.ReasoningTokens,
		usage.ImageInputTokens,
		usage.AudioInputTokens,
		usage.AudioOutputTokens,
		usage.FileInputTokens,
		usage.VideoInputTokens,
	}
	var total int64
	for _, value := range values {
		if value < 0 {
			return 0, fmt.Errorf(
				"%w: negative token usage",
				ErrInvalidInput,
			)
		}
		if total > math.MaxInt64-value {
			return 0, ErrAmountOverflow
		}
		total += value
	}
	if usage.ImageGenerationUnits < 0 {
		return 0, fmt.Errorf(
			"%w: negative image generation units",
			ErrInvalidInput,
		)
	}
	return total, nil
}
