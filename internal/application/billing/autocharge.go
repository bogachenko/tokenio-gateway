package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/ledger"
	"github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type AutoChargeConfig struct {
	ThresholdCents     int64
	MinimumChargeCents int64
}

type AutoChargeService struct {
	identity ports.BillingIdentityService
	balance  ports.BillingBalanceClient
	charge   ports.BillingChargeClient
	ledger   ports.UsageLedger
	clock    ports.Clock
	config   AutoChargeConfig
}

type AutoChargeInput struct {
	UserID               string
	BillingSubjectUserID string
	Currency             string
}

type AutoChargeResult struct {
	ProcessedBatchID        string
	ProcessedBatchIDs       []string
	Deferred                bool
	BillingBalanceCents     *int64
	ChargedAmountCents      int64
	UsedBillingBalanceCents bool
}

type chargeGroup struct {
	key     groupKey
	records []domain.UsageRecord
	pending int64
}

type groupKey struct {
	UserID       string
	ProviderType domain.ProviderType
	ClientModel  string
	Currency     string
}

func NewAutoChargeService(identity ports.BillingIdentityService, balance ports.BillingBalanceClient, charge ports.BillingChargeClient, usageLedger ports.UsageLedger, clock ports.Clock, config AutoChargeConfig) (*AutoChargeService, error) {
	if identity == nil || balance == nil || charge == nil || usageLedger == nil || clock == nil {
		return nil, fmt.Errorf("%w: dependency", ErrInvalidBillingInput)
	}
	if config.ThresholdCents <= 0 || config.MinimumChargeCents < 0 {
		return nil, fmt.Errorf("%w: config", ErrInvalidBillingInput)
	}
	return &AutoChargeService{identity: identity, balance: balance, charge: charge, ledger: usageLedger, clock: clock, config: config}, nil
}

func (s *AutoChargeService) Run(ctx context.Context, input AutoChargeInput) (AutoChargeResult, error) {
	var result AutoChargeResult
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.BillingSubjectUserID) == "" {
		return result, fmt.Errorf("%w: auto charge", ErrInvalidBillingInput)
	}
	currency := input.Currency
	if currency == "" {
		currency = currencyRUB
	}
	if currency != currencyRUB {
		return result, fmt.Errorf("%w: currency", ErrInvalidBillingInput)
	}

	open, err := s.ledger.LoadOpenChargeBatches(ctx, input.UserID, input.BillingSubjectUserID, currency)
	if err != nil {
		return result, ErrBillingStoreUnavailable
	}
	open = append([]ports.BillingChargeBatchSnapshot(nil), open...)
	sort.Slice(open, func(i, j int) bool {
		if !open[i].Batch.CreatedAt.Equal(open[j].Batch.CreatedAt) {
			return open[i].Batch.CreatedAt.Before(open[j].Batch.CreatedAt)
		}
		return open[i].Batch.ID < open[j].Batch.ID
	})
	for _, snapshot := range open {
		if snapshot.Batch.Status == domain.BillingChargeStatusSucceeded {
			return result, ErrBillingStoreContractViolation
		}
		processed, err := s.processPreparedBatch(ctx, input, snapshot)
		var mergeErr error
		result, mergeErr = mergeProcessedResult(result, processed)
		if mergeErr != nil {
			return result, mergeErr
		}
		if err != nil {
			return result, err
		}
	}

	candidates, err := s.ledger.LoadChargeCandidates(ctx, input.UserID, currency)
	if err != nil {
		return result, ErrBillingStoreUnavailable
	}
	groups, err := BuildChargeGroups(input.UserID, currency, candidates)
	if err != nil {
		return result, ErrBillingStoreContractViolation
	}
	pending, err := sumGroups(groups)
	if err != nil {
		return result, ErrBillingStoreContractViolation
	}
	if pending < s.config.ThresholdCents {
		result.Deferred = len(result.ProcessedBatchIDs) == 0
		if len(result.ProcessedBatchIDs) > 0 {
			return result, nil
		}
		return result, ErrChargeDeferred
	}

	token, err := s.identity.TokenForSubject(ctx, input.BillingSubjectUserID)
	if err != nil {
		return result, ErrBillingIdentityUnavailable
	}
	remote, err := s.balance.GetBalance(ctx, token)
	if err != nil {
		return result, ErrBillingUnavailable
	}
	if err := validateBillingBalance(remote); err != nil {
		return result, err
	}
	remainingRemote := remote.BalanceCents
	for groupIndex, group := range groups {
		amount := min64(group.pending, remainingRemote)
		if amount <= 0 || amount < s.config.MinimumChargeCents {
			continue
		}
		plan, err := BuildChargePlan(input.BillingSubjectUserID, group.records, amount, s.clock.Now().UTC())
		if err != nil {
			return result, ErrBillingStoreContractViolation
		}
		prepared, err := s.ledger.PrepareChargeBatch(ctx, plan)
		if err != nil {
			return result, ErrBillingStoreUnavailable
		}
		processed, err := s.processPreparedBatch(ctx, input, prepared)
		var mergeErr error
		result, mergeErr = mergeProcessedResult(result, processed)
		if mergeErr != nil {
			return result, mergeErr
		}
		if err != nil {
			return result, err
		}
		if groupIndex+1 < len(groups) {
			remainingRemote, err = s.resolveRemainingRemoteBalance(
				ctx,
				token,
				remainingRemote,
				prepared,
				processed,
			)
			if err != nil {
				return result, err
			}
		}
	}
	if len(result.ProcessedBatchIDs) == 0 {
		return AutoChargeResult{Deferred: true}, ErrChargeDeferred
	}
	return result, nil
}

func (s *AutoChargeService) processPreparedBatch(ctx context.Context, input AutoChargeInput, snapshot ports.BillingChargeBatchSnapshot) (AutoChargeResult, error) {
	result := AutoChargeResult{ProcessedBatchID: snapshot.Batch.ID, ProcessedBatchIDs: []string{snapshot.Batch.ID}}
	if err := ValidateChargeSnapshot(input, snapshot); err != nil {
		return result, ErrBillingStoreContractViolation
	}
	if snapshot.Batch.Status == domain.BillingChargeStatusSucceeded {
		result.BillingBalanceCents = snapshot.Batch.BillingResponseBalanceCents
		result.ChargedAmountCents = snapshot.Batch.AmountCents
		result.UsedBillingBalanceCents = snapshot.Batch.BillingResponseBalanceCents != nil
		return result, nil
	}
	chargeResult, err := s.charge.Charge(ctx, ports.BillingChargeRequest{
		RequestID:    snapshot.Batch.ID,
		UserID:       snapshot.Batch.BillingSubjectUserID,
		Model:        snapshot.Batch.BillingModel,
		InputTokens:  snapshot.Batch.InputTokens,
		OutputTokens: snapshot.Batch.OutputTokens,
		AmountCents:  snapshot.Batch.AmountCents,
		Currency:     snapshot.Batch.Currency,
	})
	if err != nil {
		if markErr := s.ledger.MarkChargeBatchFailed(ctx, snapshot.Batch.ID, snapshot.Batch.Status, "billing_unavailable", s.clock.Now().UTC()); markErr != nil {
			return result, ErrChargeReconciliationRequired
		}
		return result, ErrBillingUnavailable
	}
	if err := validateBillingChargeResult(chargeResult); err != nil {
		return result, ErrChargeReconciliationRequired
	}
	success := ports.UsageChargeSuccess{BatchID: snapshot.Batch.ID, BillingBalanceCents: chargeResult.BalanceCents, ChargedAt: s.clock.Now().UTC(), Allocations: snapshot.Allocations, ExpectedRecords: snapshot.ExpectedRecords}
	if err := s.ledger.ApplyChargeSuccess(ctx, success); err != nil {
		return result, ErrChargeReconciliationRequired
	}
	result.BillingBalanceCents = chargeResult.BalanceCents
	result.ChargedAmountCents = snapshot.Batch.AmountCents
	result.UsedBillingBalanceCents = chargeResult.BalanceCents != nil
	return result, nil
}

func (s *AutoChargeService) resolveRemainingRemoteBalance(
	ctx context.Context,
	billingToken string,
	previous int64,
	prepared ports.BillingChargeBatchSnapshot,
	processed AutoChargeResult,
) (int64, error) {
	if prepared.Batch.Status == domain.BillingChargeStatusSucceeded &&
		processed.BillingBalanceCents == nil {
		refreshed, err := s.balance.GetBalance(ctx, billingToken)
		if err != nil {
			return 0, ErrBillingUnavailable
		}
		if err := validateBillingBalance(refreshed); err != nil {
			return 0, err
		}
		return refreshed.BalanceCents, nil
	}

	return nextRemoteBalance(
		previous,
		prepared.Batch.AmountCents,
		processed.BillingBalanceCents,
	)
}

func validateBillingChargeResult(result ports.BillingChargeResult) error {
	if result.BalanceCents != nil && *result.BalanceCents < 0 {
		return ErrBillingUnavailable
	}
	return nil
}

func BuildChargeGroups(userID string, currency string, records []domain.UsageRecord) ([]chargeGroup, error) {
	if strings.TrimSpace(userID) == "" || currency != currencyRUB {
		return nil, fmt.Errorf("%w: grouping input", ErrInvalidBillingInput)
	}
	copied := append([]domain.UsageRecord(nil), records...)
	filtered := make([]domain.UsageRecord, 0, len(copied))
	for _, record := range copied {
		if err := validateChargeCandidate(userID, currency, record); err != nil {
			return nil, err
		}
		filtered = append(filtered, record)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].ProviderType != filtered[j].ProviderType {
			return filtered[i].ProviderType < filtered[j].ProviderType
		}
		if filtered[i].ClientModel != filtered[j].ClientModel {
			return filtered[i].ClientModel < filtered[j].ClientModel
		}
		if filtered[i].Currency != filtered[j].Currency {
			return filtered[i].Currency < filtered[j].Currency
		}
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].LocalRequestID < filtered[j].LocalRequestID
	})
	byKey := make(map[groupKey][]domain.UsageRecord)
	for _, record := range filtered {
		key := groupKey{UserID: record.UserID, ProviderType: record.ProviderType, ClientModel: record.ClientModel, Currency: record.Currency}
		byKey[key] = append(byKey[key], record)
	}
	keys := make([]groupKey, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].ProviderType != keys[j].ProviderType {
			return keys[i].ProviderType < keys[j].ProviderType
		}
		if keys[i].ClientModel != keys[j].ClientModel {
			return keys[i].ClientModel < keys[j].ClientModel
		}
		return keys[i].Currency < keys[j].Currency
	})
	groups := make([]chargeGroup, 0, len(keys))
	for _, key := range keys {
		recs := append([]domain.UsageRecord(nil), byKey[key]...)
		sort.SliceStable(recs, func(i, j int) bool {
			if !recs[i].CreatedAt.Equal(recs[j].CreatedAt) {
				return recs[i].CreatedAt.Before(recs[j].CreatedAt)
			}
			return recs[i].LocalRequestID < recs[j].LocalRequestID
		})
		var pending int64
		for _, rec := range recs {
			var err error
			pending, err = checkedAddAmount(pending, rec.RemainingAmountCents)
			if err != nil {
				return nil, err
			}
		}
		groups = append(groups, chargeGroup{key: key, records: recs, pending: pending})
	}
	return groups, nil
}

func validateChargeCandidate(userID string, currency string, record domain.UsageRecord) error {
	billingModel, err := pricing.BillingModel(record.ProviderType, record.ClientModel)
	if err != nil || record.BillingModel != billingModel {
		return fmt.Errorf("%w: billing model", ErrInvalidChargePlan)
	}
	if err := ledger.ValidateRecord(record); err != nil {
		return err
	}
	if record.UserID != userID {
		return fmt.Errorf("%w: candidate user", ErrInvalidChargePlan)
	}
	if record.Currency != currency {
		return fmt.Errorf("%w: candidate currency", ErrInvalidChargePlan)
	}
	if record.Status != domain.UsageStatusBillable && record.Status != domain.UsageStatusPartiallyCharged {
		return fmt.Errorf("%w: candidate status", ErrInvalidChargePlan)
	}
	if record.RemainingAmountCents <= 0 || record.CreatedAt.IsZero() {
		return fmt.Errorf("%w: candidate fields", ErrInvalidChargePlan)
	}
	if record.Status == domain.UsageStatusBillable && record.BillingChargeRequestID != "" {
		return fmt.Errorf("%w: billable candidate claim", ErrInvalidChargePlan)
	}
	if record.Status == domain.UsageStatusPartiallyCharged && record.BillingChargeRequestID == "" {
		return fmt.Errorf("%w: partially charged candidate claim", ErrInvalidChargePlan)
	}
	chargedPlusRemaining, err := checkedAddAmount(record.ChargedAmountCents, record.RemainingAmountCents)
	if err != nil {
		return err
	}
	if record.ClientAmountCents <= 0 || record.ChargedAmountCents < 0 || chargedPlusRemaining != record.ClientAmountCents {
		return fmt.Errorf("%w: candidate amounts", ErrInvalidChargePlan)
	}
	return nil
}

func ValidateChargeSnapshot(input AutoChargeInput, snapshot ports.BillingChargeBatchSnapshot) error {
	batch := snapshot.Batch
	if batch.UserID != input.UserID || batch.BillingSubjectUserID != input.BillingSubjectUserID || batch.Currency != normalizedCurrency(input.Currency) {
		return fmt.Errorf("%w: batch owner", ErrInvalidChargePlan)
	}
	switch batch.Status {
	case domain.BillingChargeStatusPending, domain.BillingChargeStatusFailed, domain.BillingChargeStatusSucceeded:
	default:
		return fmt.Errorf("%w: batch status", ErrInvalidChargePlan)
	}
	if err := ledger.ValidateChargeBatch(batch); err != nil {
		return err
	}
	billingModel, err := pricing.BillingModel(batch.ProviderType, batch.ClientModel)
	if err != nil {
		return err
	}
	if batch.BillingModel != billingModel {
		return fmt.Errorf("%w: batch billing model", ErrInvalidChargePlan)
	}
	if len(snapshot.Allocations) == 0 || len(snapshot.ExpectedRecords) == 0 {
		return fmt.Errorf("%w: empty allocations", ErrInvalidChargePlan)
	}
	if len(snapshot.ExpectedRecords) != len(snapshot.Allocations) {
		return fmt.Errorf("%w: expected records", ErrInvalidChargePlan)
	}
	expectedByLocalRequestID := make(map[string]domain.UsageRecord, len(snapshot.ExpectedRecords))
	for _, record := range snapshot.ExpectedRecords {
		if _, ok := expectedByLocalRequestID[record.LocalRequestID]; ok {
			return fmt.Errorf("%w: duplicate expected record", ErrInvalidChargePlan)
		}
		if err := validateExpectedRecordForBatch(batch, record); err != nil {
			return err
		}
		expectedByLocalRequestID[record.LocalRequestID] = record
	}
	seen := make(map[string]struct{}, len(snapshot.Allocations))
	var total int64
	for _, allocation := range snapshot.Allocations {
		if err := ledger.ValidateAllocation(batch, allocation); err != nil {
			return err
		}
		if allocation.ID != StableAllocationID(batch.ID, allocation.LocalRequestID) {
			return fmt.Errorf("%w: allocation id", ErrInvalidChargePlan)
		}
		record, ok := expectedByLocalRequestID[allocation.LocalRequestID]
		if !ok {
			return fmt.Errorf("%w: allocation record", ErrInvalidChargePlan)
		}
		if allocation.ChargedAmountCents > record.RemainingAmountCents || allocation.RemainingAmountCents != record.RemainingAmountCents-allocation.ChargedAmountCents {
			return fmt.Errorf("%w: allocation expectation", ErrInvalidChargePlan)
		}
		if _, ok := seen[allocation.LocalRequestID]; ok {
			return fmt.Errorf("%w: duplicate allocation", ErrInvalidChargePlan)
		}
		seen[allocation.LocalRequestID] = struct{}{}
		var err error
		total, err = checkedAddAmount(total, allocation.ChargedAmountCents)
		if err != nil {
			return err
		}
	}
	if total != batch.AmountCents {
		return fmt.Errorf("%w: allocation sum", ErrInvalidChargePlan)
	}
	inputTokens, outputTokens, err := allocatedTokenTotals(snapshot.ExpectedRecords, snapshot.Allocations)
	if err != nil {
		return err
	}
	if batch.InputTokens != inputTokens || batch.OutputTokens != outputTokens {
		return fmt.Errorf("%w: token totals", ErrInvalidChargePlan)
	}
	if batch.ID != StableBatchID(batch, snapshot.ExpectedRecords, snapshot.Allocations) {
		return fmt.Errorf("%w: batch id", ErrInvalidChargePlan)
	}
	return nil
}

func validateExpectedRecordForBatch(batch domain.BillingChargeBatch, record domain.UsageRecord) error {
	if err := ledger.ValidateRecord(record); err != nil {
		return err
	}
	if record.UserID != batch.UserID || record.ProviderType != batch.ProviderType || record.ClientModel != batch.ClientModel || record.Currency != batch.Currency || record.BillingModel != batch.BillingModel {
		return fmt.Errorf("%w: expected record owner", ErrInvalidChargePlan)
	}
	if record.Status != domain.UsageStatusBillable && record.Status != domain.UsageStatusPartiallyCharged {
		return fmt.Errorf("%w: expected record status", ErrInvalidChargePlan)
	}
	if record.BillingChargeRequestID != batch.ID {
		return fmt.Errorf("%w: expected record claim", ErrInvalidChargePlan)
	}
	chargedPlusRemaining, err := checkedAddAmount(record.ChargedAmountCents, record.RemainingAmountCents)
	if err != nil {
		return err
	}
	if record.ClientAmountCents <= 0 || record.ChargedAmountCents < 0 || record.RemainingAmountCents <= 0 || chargedPlusRemaining != record.ClientAmountCents {
		return fmt.Errorf("%w: expected record amounts", ErrInvalidChargePlan)
	}
	return nil
}

func allocatedTokenTotals(records []domain.UsageRecord, allocations []domain.BillingChargeAllocation) (int64, int64, error) {
	recordByID := make(map[string]domain.UsageRecord, len(records))
	for _, record := range records {
		recordByID[record.LocalRequestID] = record
	}
	var inputTokens int64
	var outputTokens int64
	for _, allocation := range allocations {
		record, ok := recordByID[allocation.LocalRequestID]
		if !ok {
			return 0, 0, fmt.Errorf("%w: allocation record", ErrInvalidChargePlan)
		}
		inTok, err := allocateTokens(record.Usage.InputTokens, record.ChargedAmountCents, record.ChargedAmountCents+allocation.ChargedAmountCents, record.ClientAmountCents)
		if err != nil {
			return 0, 0, err
		}
		outTok, err := allocateTokens(record.Usage.OutputTokens, record.ChargedAmountCents, record.ChargedAmountCents+allocation.ChargedAmountCents, record.ClientAmountCents)
		if err != nil {
			return 0, 0, err
		}
		inputTokens, err = checkedAddToken(inputTokens, inTok)
		if err != nil {
			return 0, 0, err
		}
		outputTokens, err = checkedAddToken(outputTokens, outTok)
		if err != nil {
			return 0, 0, err
		}
	}
	return inputTokens, outputTokens, nil
}

func normalizedCurrency(currency string) string {
	if currency == "" {
		return currencyRUB
	}
	return currency
}

func BuildChargePlan(billingSubjectUserID string, records []domain.UsageRecord, amount int64, createdAt time.Time) (ports.UsageChargeBatchPlan, error) {
	var plan ports.UsageChargeBatchPlan
	createdAt = createdAt.UTC()
	if strings.TrimSpace(billingSubjectUserID) == "" || amount <= 0 || len(records) == 0 {
		return plan, fmt.Errorf("%w: input", ErrInvalidChargePlan)
	}
	recs := append([]domain.UsageRecord(nil), records...)
	sort.SliceStable(recs, func(i, j int) bool {
		if !recs[i].CreatedAt.Equal(recs[j].CreatedAt) {
			return recs[i].CreatedAt.Before(recs[j].CreatedAt)
		}
		return recs[i].LocalRequestID < recs[j].LocalRequestID
	})
	first := recs[0]
	billingModel, err := pricing.BillingModel(first.ProviderType, first.ClientModel)
	if err != nil {
		return plan, err
	}
	left := amount
	allocations := make([]domain.BillingChargeAllocation, 0, len(recs))
	allocatedRecords := make([]domain.UsageRecord, 0, len(recs))
	var inputTokens, outputTokens int64
	partialCount := 0
	for _, rec := range recs {
		if rec.UserID != first.UserID || rec.ProviderType != first.ProviderType || rec.ClientModel != first.ClientModel || rec.Currency != first.Currency {
			return plan, fmt.Errorf("%w: mixed group", ErrInvalidChargePlan)
		}
		chargedPlusRemaining, err := checkedAddAmount(rec.ChargedAmountCents, rec.RemainingAmountCents)
		if err != nil {
			return plan, err
		}
		if rec.ClientAmountCents <= 0 || rec.ChargedAmountCents < 0 || rec.RemainingAmountCents <= 0 || chargedPlusRemaining != rec.ClientAmountCents {
			return plan, fmt.Errorf("%w: amount invariant", ErrInvalidChargePlan)
		}
		if left <= 0 {
			break
		}
		delta := min64(rec.RemainingAmountCents, left)
		if delta <= 0 {
			continue
		}
		newRemaining := rec.RemainingAmountCents - delta
		if newRemaining > 0 {
			partialCount++
		}
		inTok, err := allocateTokens(rec.Usage.InputTokens, rec.ChargedAmountCents, rec.ChargedAmountCents+delta, rec.ClientAmountCents)
		if err != nil {
			return plan, err
		}
		outTok, err := allocateTokens(rec.Usage.OutputTokens, rec.ChargedAmountCents, rec.ChargedAmountCents+delta, rec.ClientAmountCents)
		if err != nil {
			return plan, err
		}
		inputTokens, err = checkedAddToken(inputTokens, inTok)
		if err != nil {
			return plan, err
		}
		outputTokens, err = checkedAddToken(outputTokens, outTok)
		if err != nil {
			return plan, err
		}
		allocations = append(allocations, domain.BillingChargeAllocation{LocalRequestID: rec.LocalRequestID, ChargedAmountCents: delta, RemainingAmountCents: newRemaining, CreatedAt: createdAt})
		allocatedRecords = append(allocatedRecords, rec)
		left -= delta
	}
	if left != 0 || len(allocations) == 0 || partialCount > 1 {
		return plan, fmt.Errorf("%w: allocation", ErrInvalidChargePlan)
	}
	batch := domain.BillingChargeBatch{UserID: first.UserID, BillingSubjectUserID: billingSubjectUserID, ProviderType: first.ProviderType, ClientModel: first.ClientModel, BillingModel: billingModel, InputTokens: inputTokens, OutputTokens: outputTokens, AmountCents: amount, Currency: first.Currency, Status: domain.BillingChargeStatusPending, CreatedAt: createdAt, UpdatedAt: createdAt}
	batch.ID = StableBatchID(batch, allocatedRecords, allocations)
	for i := range allocations {
		allocations[i].BatchID = batch.ID
		allocations[i].ID = StableAllocationID(batch.ID, allocations[i].LocalRequestID)
	}
	batch.ID = StableBatchID(batch, allocatedRecords, allocations)
	for i := range allocations {
		allocations[i].BatchID = batch.ID
		allocations[i].ID = StableAllocationID(batch.ID, allocations[i].LocalRequestID)
	}
	plan.Batch = batch
	plan.Allocations = allocations
	plan.ExpectedRecords = allocatedRecords
	return plan, nil
}

func StableBatchID(batch domain.BillingChargeBatch, records []domain.UsageRecord, allocations []domain.BillingChargeAllocation) string {
	type recordExpectation struct {
		LocalRequestID       string             `json:"local_request_id"`
		Status               domain.UsageStatus `json:"status"`
		ClientAmountCents    int64              `json:"client_amount_cents"`
		ChargedAmountCents   int64              `json:"charged_amount_cents"`
		RemainingAmountCents int64              `json:"remaining_amount_cents"`
		InputTokens          int64              `json:"input_tokens"`
		OutputTokens         int64              `json:"output_tokens"`
	}
	type allocationExpectation struct {
		LocalRequestID       string `json:"local_request_id"`
		ChargedAmountCents   int64  `json:"charged_amount_cents"`
		RemainingAmountCents int64  `json:"remaining_amount_cents"`
	}
	type canonical struct {
		UserID               string                  `json:"user_id"`
		BillingSubjectUserID string                  `json:"billing_subject_user_id"`
		ProviderType         domain.ProviderType     `json:"provider_type"`
		ClientModel          string                  `json:"client_model"`
		BillingModel         string                  `json:"billing_model"`
		InputTokens          int64                   `json:"input_tokens"`
		OutputTokens         int64                   `json:"output_tokens"`
		Currency             string                  `json:"currency"`
		AmountCents          int64                   `json:"amount_cents"`
		Records              []recordExpectation     `json:"records"`
		Allocations          []allocationExpectation `json:"allocations"`
	}
	payload := canonical{UserID: batch.UserID, BillingSubjectUserID: batch.BillingSubjectUserID, ProviderType: batch.ProviderType, ClientModel: batch.ClientModel, BillingModel: batch.BillingModel, InputTokens: batch.InputTokens, OutputTokens: batch.OutputTokens, Currency: batch.Currency, AmountCents: batch.AmountCents}
	for _, rec := range records {
		payload.Records = append(payload.Records, recordExpectation{LocalRequestID: rec.LocalRequestID, Status: rec.Status, ClientAmountCents: rec.ClientAmountCents, ChargedAmountCents: rec.ChargedAmountCents, RemainingAmountCents: rec.RemainingAmountCents, InputTokens: rec.Usage.InputTokens, OutputTokens: rec.Usage.OutputTokens})
	}
	for _, alloc := range allocations {
		payload.Allocations = append(payload.Allocations, allocationExpectation{LocalRequestID: alloc.LocalRequestID, ChargedAmountCents: alloc.ChargedAmountCents, RemainingAmountCents: alloc.RemainingAmountCents})
	}
	body, _ := json.Marshal(payload)
	sum := sha256.Sum256(body)
	return "billchg_" + hex.EncodeToString(sum[:])
}

func StableAllocationID(batchID string, localRequestID string) string {
	sum := sha256.Sum256([]byte(batchID + "\x00" + localRequestID))
	return "billalloc_" + hex.EncodeToString(sum[:])
}

func allocateTokens(totalTokens, previousCharged, newCharged, clientAmount int64) (int64, error) {
	if totalTokens < 0 || previousCharged < 0 || newCharged < previousCharged || clientAmount <= 0 || newCharged > clientAmount {
		return 0, fmt.Errorf("%w: token input", ErrInvalidChargePlan)
	}
	before := floorMulDiv(totalTokens, previousCharged, clientAmount)
	after := floorMulDiv(totalTokens, newCharged, clientAmount)
	return after - before, nil
}

func floorMulDiv(a, b, c int64) int64 {
	var x big.Int
	x.Mul(big.NewInt(a), big.NewInt(b))
	x.Div(&x, big.NewInt(c))
	return x.Int64()
}

func mergeProcessedResult(result AutoChargeResult, processed AutoChargeResult) (AutoChargeResult, error) {
	if processed.ProcessedBatchID != "" {
		result.ProcessedBatchID = processed.ProcessedBatchID
		result.ProcessedBatchIDs = append(result.ProcessedBatchIDs, processed.ProcessedBatchID)
	}
	var err error
	result.ChargedAmountCents, err = checkedAddAmount(result.ChargedAmountCents, processed.ChargedAmountCents)
	if err != nil {
		return result, err
	}
	if processed.BillingBalanceCents != nil {
		result.BillingBalanceCents = processed.BillingBalanceCents
	}
	result.UsedBillingBalanceCents = result.UsedBillingBalanceCents || processed.UsedBillingBalanceCents
	return result, nil
}

func sumGroups(groups []chargeGroup) (int64, error) {
	var sum int64
	for _, group := range groups {
		var err error
		sum, err = checkedAddAmount(sum, group.pending)
		if err != nil {
			return 0, err
		}
	}
	return sum, nil
}

func checkedAddAmount(left int64, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, ledger.ErrAmountOverflow
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, ledger.ErrAmountOverflow
	}
	return left + right, nil
}

func checkedAddToken(left int64, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, ErrTokenOverflow
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, ErrTokenOverflow
	}
	return left + right, nil
}

func nextRemoteBalance(previous int64, charged int64, billingBalanceCents *int64) (int64, error) {
	if billingBalanceCents != nil {
		if *billingBalanceCents < 0 {
			return 0, fmt.Errorf("%w: remote balance", ErrInvalidChargePlan)
		}
		return *billingBalanceCents, nil
	}
	return checkedSubAmount(previous, charged)
}

func checkedSubAmount(left int64, right int64) (int64, error) {
	if right > 0 && left < math.MinInt64+right {
		return 0, ledger.ErrAmountOverflow
	}
	if right < 0 && left > math.MaxInt64+right {
		return 0, ledger.ErrAmountOverflow
	}
	return left - right, nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
