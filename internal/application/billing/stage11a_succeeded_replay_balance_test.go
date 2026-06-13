package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type stage11ASequenceBalanceClient struct {
	responses []ports.BillingBalance
	err       error
	calls     int
}

func (c *stage11ASequenceBalanceClient) GetBalance(
	_ context.Context,
	_ string,
) (ports.BillingBalance, error) {
	c.calls++
	if c.err != nil {
		return ports.BillingBalance{}, c.err
	}
	if len(c.responses) == 0 {
		return ports.BillingBalance{}, errors.New("unexpected balance call")
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func TestSucceededReplayWithoutPersistedBalanceRefreshesRemoteBalance(t *testing.T) {
	balance := &stage11ASequenceBalanceClient{
		responses: []ports.BillingBalance{{
			Currency:     currencyRUB,
			BalanceCents: 70,
		}},
	}
	service := &AutoChargeService{balance: balance}

	got, err := service.resolveRemainingRemoteBalance(
		t.Context(),
		"billing-jwt",
		100,
		ports.BillingChargeBatchSnapshot{
			Batch: domain.BillingChargeBatch{
				Status:      domain.BillingChargeStatusSucceeded,
				AmountCents: 30,
			},
		},
		AutoChargeResult{},
	)
	if err != nil {
		t.Fatalf("resolve remaining remote balance: %v", err)
	}
	if got != 70 {
		t.Fatalf("remaining remote balance = %d, want 70", got)
	}
	if balance.calls != 1 {
		t.Fatalf("balance calls = %d, want 1", balance.calls)
	}
}

func TestSucceededReplayWithPersistedBalanceDoesNotRefresh(t *testing.T) {
	balance := &stage11ASequenceBalanceClient{}
	service := &AutoChargeService{balance: balance}
	persisted := int64(55)

	got, err := service.resolveRemainingRemoteBalance(
		t.Context(),
		"billing-jwt",
		100,
		ports.BillingChargeBatchSnapshot{
			Batch: domain.BillingChargeBatch{
				Status:      domain.BillingChargeStatusSucceeded,
				AmountCents: 30,
			},
		},
		AutoChargeResult{BillingBalanceCents: &persisted},
	)
	if err != nil {
		t.Fatalf("resolve remaining remote balance: %v", err)
	}
	if got != persisted {
		t.Fatalf("remaining remote balance = %d, want %d", got, persisted)
	}
	if balance.calls != 0 {
		t.Fatalf("balance calls = %d, want 0", balance.calls)
	}
}

func TestCurrentChargeWithoutReturnedBalanceSubtractsExactlyOnce(t *testing.T) {
	balance := &stage11ASequenceBalanceClient{}
	service := &AutoChargeService{balance: balance}

	got, err := service.resolveRemainingRemoteBalance(
		t.Context(),
		"billing-jwt",
		100,
		ports.BillingChargeBatchSnapshot{
			Batch: domain.BillingChargeBatch{
				Status:      domain.BillingChargeStatusPending,
				AmountCents: 30,
			},
		},
		AutoChargeResult{},
	)
	if err != nil {
		t.Fatalf("resolve remaining remote balance: %v", err)
	}
	if got != 70 {
		t.Fatalf("remaining remote balance = %d, want 70", got)
	}
	if balance.calls != 0 {
		t.Fatalf("balance calls = %d, want 0", balance.calls)
	}
}

func TestSucceededReplayBalanceRefreshFailureStopsProcessing(t *testing.T) {
	balance := &stage11ASequenceBalanceClient{err: errors.New("billing down")}
	service := &AutoChargeService{balance: balance}

	_, err := service.resolveRemainingRemoteBalance(
		t.Context(),
		"billing-jwt",
		100,
		ports.BillingChargeBatchSnapshot{
			Batch: domain.BillingChargeBatch{
				Status:      domain.BillingChargeStatusSucceeded,
				AmountCents: 30,
			},
		},
		AutoChargeResult{},
	)
	if !errors.Is(err, ErrBillingUnavailable) {
		t.Fatalf("error = %v, want ErrBillingUnavailable", err)
	}
	if balance.calls != 1 {
		t.Fatalf("balance calls = %d, want 1", balance.calls)
	}
}
