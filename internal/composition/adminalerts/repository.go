package adminalerts

import (
	"context"
	"errors"
	"log"

	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var ErrInvalidAdminResellerAlertRepository = errors.New(
	"invalid admin reseller alert repository",
)

type resellerBalanceAlertChecker interface {
	CheckReseller(
		context.Context,
		string,
	) (telegramalert.CheckResult, error)
}

type adminResellerAlertRepository struct {
	ports.ResellerRepository
	checker resellerBalanceAlertChecker
	logger  *log.Logger
}

func NewAdminResellerAlertRepository(
	repository ports.ResellerRepository,
	checker resellerBalanceAlertChecker,
	logger *log.Logger,
) (*adminResellerAlertRepository, error) {
	if repository == nil || checker == nil || logger == nil {
		return nil, ErrInvalidAdminResellerAlertRepository
	}
	return &adminResellerAlertRepository{
		ResellerRepository: repository,
		checker:            checker,
		logger:             logger,
	}, nil
}

func (r *adminResellerAlertRepository) CompareAndSwapResellerWithAudit(
	ctx context.Context,
	expected domain.Reseller,
	next domain.Reseller,
	audit domain.AuditContext,
) (domain.Reseller, error) {
	if r == nil ||
		r.ResellerRepository == nil ||
		r.checker == nil ||
		r.logger == nil {
		return domain.Reseller{}, ErrInvalidAdminResellerAlertRepository
	}

	persisted, err := r.ResellerRepository.CompareAndSwapResellerWithAudit(
		ctx,
		expected,
		next,
		audit,
	)
	if err != nil {
		return domain.Reseller{}, err
	}
	if persisted.BalanceCents == expected.BalanceCents {
		return persisted, nil
	}

	_, checkErr := r.checker.CheckReseller(
		context.WithoutCancel(ctx),
		persisted.ID,
	)
	if checkErr != nil {
		r.logger.Printf(
			"post-commit reseller balance alert check failed reseller_id=%s error_type=%T",
			persisted.ID,
			checkErr,
		)
	}
	return persisted, nil
}
