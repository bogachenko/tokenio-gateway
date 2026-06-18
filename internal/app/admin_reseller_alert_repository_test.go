package app

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type postCommitResellerRepositoryFake struct {
	ports.ResellerRepository
	mu        sync.Mutex
	committed bool
	persisted domain.Reseller
	err       error
}

func (f *postCommitResellerRepositoryFake) CompareAndSwapResellerWithAudit(
	context.Context,
	domain.Reseller,
	domain.Reseller,
	domain.AuditContext,
) (domain.Reseller, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return domain.Reseller{}, f.err
	}
	f.committed = true
	return f.persisted, nil
}

type postCommitBalanceCheckerFake struct {
	mu              sync.Mutex
	repository      *postCommitResellerRepositoryFake
	calls           int
	resellerID      string
	committedAtCall bool
	err             error
}

func (f *postCommitBalanceCheckerFake) CheckReseller(
	_ context.Context,
	resellerID string,
) (telegramalert.CheckResult, error) {
	f.repository.mu.Lock()
	committed := f.repository.committed
	f.repository.mu.Unlock()

	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.resellerID = resellerID
	f.committedAtCall = committed
	return telegramalert.CheckResult{}, f.err
}

func TestAdminResellerAlertRepositoryChecksOnlyAfterCommittedBalanceChange(
	t *testing.T,
) {
	at := time.Unix(100, 0).UTC()
	expected := domain.Reseller{
		ID:           "reseller-1",
		BalanceCents: 20000,
		CreatedAt:    at,
		UpdatedAt:    at,
	}
	persisted := expected
	persisted.BalanceCents = 9000
	persisted.UpdatedAt = at.Add(time.Second)

	repository := &postCommitResellerRepositoryFake{
		persisted: persisted,
	}
	checker := &postCommitBalanceCheckerFake{
		repository: repository,
	}
	decorator, err := newAdminResellerAlertRepository(
		repository,
		checker,
		log.New(io.Discard, "", 0),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := decorator.CompareAndSwapResellerWithAudit(
		context.Background(),
		expected,
		persisted,
		domain.AuditContext{},
	)
	if err != nil {
		t.Fatalf("CompareAndSwapResellerWithAudit: %v", err)
	}
	if result != persisted {
		t.Fatalf("result = %+v, want %+v", result, persisted)
	}

	checker.mu.Lock()
	defer checker.mu.Unlock()
	if checker.calls != 1 ||
		checker.resellerID != persisted.ID ||
		!checker.committedAtCall {
		t.Fatalf(
			"checker calls=%d reseller=%q committed=%v",
			checker.calls,
			checker.resellerID,
			checker.committedAtCall,
		)
	}
}

func TestAdminResellerAlertRepositoryDoesNotCheckFailedOrUnchangedMutation(
	t *testing.T,
) {
	at := time.Unix(100, 0).UTC()
	current := domain.Reseller{
		ID:           "reseller-1",
		BalanceCents: 20000,
		CreatedAt:    at,
		UpdatedAt:    at,
	}

	tests := []struct {
		name       string
		repository *postCommitResellerRepositoryFake
		wantErr    bool
	}{
		{
			name: "store failure",
			repository: &postCommitResellerRepositoryFake{
				err: errors.New("store unavailable"),
			},
			wantErr: true,
		},
		{
			name: "unchanged balance",
			repository: &postCommitResellerRepositoryFake{
				persisted: current,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checker := &postCommitBalanceCheckerFake{
				repository: tc.repository,
			}
			decorator, err := newAdminResellerAlertRepository(
				tc.repository,
				checker,
				log.New(io.Discard, "", 0),
			)
			if err != nil {
				t.Fatal(err)
			}

			_, err = decorator.CompareAndSwapResellerWithAudit(
				context.Background(),
				current,
				current,
				domain.AuditContext{},
			)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr=%v", err, tc.wantErr)
			}

			checker.mu.Lock()
			calls := checker.calls
			checker.mu.Unlock()
			if calls != 0 {
				t.Fatalf("checker calls=%d, want 0", calls)
			}
		})
	}
}

func TestAdminResellerAlertRepositoryIgnoresPostCommitCheckFailure(
	t *testing.T,
) {
	at := time.Unix(100, 0).UTC()
	expected := domain.Reseller{
		ID:           "reseller-1",
		BalanceCents: 20000,
		CreatedAt:    at,
		UpdatedAt:    at,
	}
	persisted := expected
	persisted.BalanceCents = 9000

	repository := &postCommitResellerRepositoryFake{
		persisted: persisted,
	}
	checker := &postCommitBalanceCheckerFake{
		repository: repository,
		err:        errors.New("alert store unavailable"),
	}
	decorator, err := newAdminResellerAlertRepository(
		repository,
		checker,
		log.New(io.Discard, "", 0),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := decorator.CompareAndSwapResellerWithAudit(
		context.Background(),
		expected,
		persisted,
		domain.AuditContext{},
	)
	if err != nil {
		t.Fatalf("financial result was rolled back: %v", err)
	}
	if result != persisted {
		t.Fatalf("result = %+v, want %+v", result, persisted)
	}
}
