#!/usr/bin/env python3
from __future__ import annotations

import argparse
import difflib
import subprocess
import sys
from pathlib import Path

MODULE = "module github.com/bogachenko/tokenio-gateway"
AUTOCHARGE = Path("internal/application/billing/autocharge.go")
TEST = Path("internal/application/billing/stage11a_succeeded_replay_balance_test.go")
SPEC = Path("docs/spec/050-ledger-and-auto-charge.ru.md")

OLD_LOOP = """\
\tremainingRemote := remote.BalanceCents
\tfor _, group := range groups {
"""
NEW_LOOP = """\
\tremainingRemote := remote.BalanceCents
\tfor groupIndex, group := range groups {
"""

OLD_RESOLVE = """\
\t\tremainingRemote, err = s.resolveRemainingRemoteBalance(
\t\t\tctx,
\t\t\ttoken,
\t\t\tremainingRemote,
\t\t\tprepared,
\t\t\tprocessed,
\t\t)
\t\tif err != nil {
\t\t\treturn result, err
\t\t}
"""
NEW_RESOLVE = """\
\t\tif groupIndex+1 < len(groups) {
\t\t\tremainingRemote, err = s.resolveRemainingRemoteBalance(
\t\t\t\tctx,
\t\t\t\ttoken,
\t\t\t\tremainingRemote,
\t\t\t\tprepared,
\t\t\t\tprocessed,
\t\t\t)
\t\t\tif err != nil {
\t\t\t\treturn result, err
\t\t\t}
\t\t}
"""

SPEC_OLD = """\
Новые Billing charge calls и новые ledger mutations после такого replay не выполняются до successful refresh.

Для `pending`/`failed` snapshot, который был реально списан текущим вызовом и получил successful Billing response без balance, application вычитает `batch.AmountCents` ровно один раз из balance, загруженного до этого charge.
"""
SPEC_NEW = """\
Новые Billing charge calls и новые ledger mutations после такого replay не выполняются до successful refresh.

После последней provider/model group refresh не выполняется. Если следующей group нет, `succeeded` replay завершается успешно даже при недоступном Billing balance endpoint: пересчитанный `remainingRemote` больше не используется для нового financial decision.

Для `pending`/`failed` snapshot, который был реально списан текущим вызовом и получил successful Billing response без balance, application вычитает `batch.AmountCents` ровно один раз из balance, загруженного до этого charge, только если значение требуется для решения по следующей group.
"""

TEST_MARKER = "func TestRunDoesNotRefreshBalanceAfterFinalSucceededReplayGroup("
TEST_APPEND = r'''

type stage11AFinalReplayLedger struct {
	candidate       domain.UsageRecord
	snapshot        ports.BillingChargeBatchSnapshot
	prepareCalls    int
	markFailedCalls int
	applyCalls      int
}

func (l *stage11AFinalReplayLedger) CreateReserved(
	_ context.Context,
	_ domain.UsageRecord,
) (ports.UsageReserveResult, error) {
	return ports.UsageReserveResult{}, nil
}

func (l *stage11AFinalReplayLedger) FindByLocalRequestID(
	_ context.Context,
	_ string,
) (*domain.UsageRecord, error) {
	return nil, ports.ErrNotFound
}

func (l *stage11AFinalReplayLedger) CompareAndSwap(
	_ context.Context,
	_ string,
	_ domain.UsageStatus,
	_ domain.UsageRecord,
) (ports.UsageTransitionResult, error) {
	return ports.UsageTransitionResult{}, nil
}

func (l *stage11AFinalReplayLedger) LoadExposure(
	_ context.Context,
	_ string,
	_ string,
) (ports.UsageExposureSnapshot, error) {
	return ports.UsageExposureSnapshot{}, nil
}

func (l *stage11AFinalReplayLedger) LoadOpenChargeBatches(
	_ context.Context,
	_ string,
	_ string,
	_ string,
) ([]ports.BillingChargeBatchSnapshot, error) {
	return nil, nil
}

func (l *stage11AFinalReplayLedger) LoadChargeCandidates(
	_ context.Context,
	_ string,
	_ string,
) ([]domain.UsageRecord, error) {
	return []domain.UsageRecord{l.candidate}, nil
}

func (l *stage11AFinalReplayLedger) PrepareChargeBatch(
	_ context.Context,
	plan ports.UsageChargeBatchPlan,
) (ports.BillingChargeBatchSnapshot, error) {
	l.prepareCalls++
	if plan.Batch.ID != l.snapshot.Batch.ID {
		return ports.BillingChargeBatchSnapshot{}, errors.New("unexpected replay batch id")
	}
	return l.snapshot, nil
}

func (l *stage11AFinalReplayLedger) MarkChargeBatchFailed(
	_ context.Context,
	_ string,
	_ domain.BillingChargeStatus,
	_ string,
	_ time.Time,
) error {
	l.markFailedCalls++
	return nil
}

func (l *stage11AFinalReplayLedger) ApplyChargeSuccess(
	_ context.Context,
	_ ports.UsageChargeSuccess,
) error {
	l.applyCalls++
	return nil
}

func TestRunDoesNotRefreshBalanceAfterFinalSucceededReplayGroup(t *testing.T) {
	now := time.Unix(1_700, 0).UTC()
	record := rec("final-succeeded-replay", domain.ProviderOpenAI, "m", 100, 0, 100, 1)
	plan, err := BuildChargePlan("billing", []domain.UsageRecord{record}, 100, now)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := ports.BillingChargeBatchSnapshot{
		Batch:           plan.Batch,
		Allocations:     append([]domain.BillingChargeAllocation(nil), plan.Allocations...),
		ExpectedRecords: claimedExpected(plan),
	}
	snapshot.Batch.Status = domain.BillingChargeStatusSucceeded
	snapshot.Batch.ChargedAt = &now
	snapshot.Batch.UpdatedAt = now

	ledger := &stage11AFinalReplayLedger{candidate: record, snapshot: snapshot}
	balance := &stage11ASequenceBalanceClient{
		responses: []ports.BillingBalance{{Currency: currencyRUB, BalanceCents: 100}},
	}
	charge := &fakeCharge{}
	service, err := NewAutoChargeService(
		&fakeIdentity{token: "billing-jwt"},
		balance,
		charge,
		ledger,
		testClock{t: now},
		AutoChargeConfig{ThresholdCents: 1, MinimumChargeCents: 1},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Run(t.Context(), AutoChargeInput{
		UserID:               "u",
		BillingSubjectUserID: "billing",
		Currency:             currencyRUB,
	})
	if err != nil {
		t.Fatalf("Run returned error after final succeeded replay: %v", err)
	}
	if balance.calls != 1 {
		t.Fatalf("balance calls = %d, want initial call only", balance.calls)
	}
	if len(charge.requests) != 0 {
		t.Fatalf("billing charge called for succeeded replay: %+v", charge.requests)
	}
	if ledger.prepareCalls != 1 || ledger.markFailedCalls != 0 || ledger.applyCalls != 0 {
		t.Fatalf(
			"ledger calls prepare=%d mark_failed=%d apply=%d",
			ledger.prepareCalls,
			ledger.markFailedCalls,
			ledger.applyCalls,
		)
	}
	if len(result.ProcessedBatchIDs) != 1 || result.ProcessedBatchIDs[0] != snapshot.Batch.ID {
		t.Fatalf("processed batches = %+v", result.ProcessedBatchIDs)
	}
}
'''


def fail(message: str) -> None:
    raise RuntimeError(message)


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        fail(f"{label}: expected exactly one anchor, found {count}")
    return text.replace(old, new, 1)


def run(command: list[str], cwd: Path) -> None:
    print("+", " ".join(command), flush=True)
    completed = subprocess.run(command, cwd=cwd, text=True)
    if completed.returncode != 0:
        fail(f"command failed with exit code {completed.returncode}: {' '.join(command)}")


def print_diff(path: Path, before: str, after: str) -> None:
    if before == after:
        return
    sys.stdout.writelines(
        difflib.unified_diff(
            before.splitlines(keepends=True),
            after.splitlines(keepends=True),
            fromfile=f"a/{path.as_posix()}",
            tofile=f"b/{path.as_posix()}",
        )
    )


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Refresh Billing balance only when another auto-charge group remains."
    )
    parser.add_argument("repo", nargs="?", default=".")
    parser.add_argument("--skip-full-suite", action="store_true")
    args = parser.parse_args()

    repo = Path(args.repo).resolve()
    go_mod = repo / "go.mod"
    if not go_mod.is_file() or MODULE not in go_mod.read_text(encoding="utf-8"):
        fail(f"{repo} is not the expected tokenio-gateway repository")

    paths = [AUTOCHARGE, TEST, SPEC]
    originals: dict[Path, str] = {}
    for relative in paths:
        path = repo / relative
        if not path.is_file():
            fail(f"required file not found: {relative}")
        originals[relative] = path.read_text(encoding="utf-8")

    try:
        autocharge = originals[AUTOCHARGE]
        if "for groupIndex, group := range groups" not in autocharge:
            autocharge = replace_once(autocharge, OLD_LOOP, NEW_LOOP, "indexed group loop")
        if "if groupIndex+1 < len(groups)" not in autocharge:
            autocharge = replace_once(autocharge, OLD_RESOLVE, NEW_RESOLVE, "conditional balance resolve")
        elif OLD_RESOLVE in autocharge:
            fail("autocharge contains both conditional and unconditional balance resolution")

        test = originals[TEST]
        if '"time"' not in test:
            test = replace_once(
                test,
                '"testing"\n',
                '"testing"\n\t"time"\n',
                "test time import",
            )
        if TEST_MARKER not in test:
            test = test.rstrip() + TEST_APPEND + "\n"

        spec = originals[SPEC]
        if "После последней provider/model group refresh не выполняется" not in spec:
            spec = replace_once(spec, SPEC_OLD, SPEC_NEW, "final group refresh contract")

        (repo / AUTOCHARGE).write_text(autocharge, encoding="utf-8")
        (repo / TEST).write_text(test, encoding="utf-8")
        (repo / SPEC).write_text(spec, encoding="utf-8")

        run(["gofmt", "-w", AUTOCHARGE.as_posix(), TEST.as_posix()], repo)

        print("\n--- PATCH DIFF ---")
        for relative in paths:
            after = (repo / relative).read_text(encoding="utf-8")
            print_diff(relative, originals[relative], after)

        run(["go", "test", "./internal/application/billing/..."], repo)
        run(["go", "test", "-race", "./internal/application/billing/..."], repo)
        if not args.skip_full_suite:
            run(["go", "test", "./..."], repo)
            run(["go", "vet", "./..."], repo)
        run(["git", "diff", "--check"], repo)

        print("\n--- VERIFICATION ---")
        run(["grep", "-n", "for groupIndex, group := range groups", AUTOCHARGE.as_posix()], repo)
        run(["grep", "-n", "if groupIndex+1 < len(groups)", AUTOCHARGE.as_posix()], repo)
        run(["grep", "-n", "TestRunDoesNotRefreshBalanceAfterFinalSucceededReplayGroup", TEST.as_posix()], repo)
        run(["grep", "-n", "После последней provider/model group", SPEC.as_posix()], repo)

        print("\nOK: balance refresh is limited to decisions for a following group")
        return 0
    except Exception:
        print("\nROLLBACK: restoring files changed by this script", file=sys.stderr)
        for relative, content in originals.items():
            (repo / relative).write_text(content, encoding="utf-8")
        raise


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
