//go:build integration

package credits_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
)

func TestBalanceOfAnUntouchedWorkspaceIsZero(t *testing.T) {
	ctx, _, ledger, workspaceID := setup(t)

	balance, err := ledger.Balance(ctx, workspaceID)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if balance != 0 {
		t.Errorf("balance = %d, want 0", balance)
	}
}

func TestBalanceAlwaysEqualsTheLedgerSum(t *testing.T) {
	ctx, pool, ledger, workspaceID := setup(t)

	if _, err := ledger.Grant(ctx, workspaceID, 300, "september"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if _, err := ledger.Purchase(ctx, workspaceID, 120, "payment-1"); err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if _, err := ledger.Debit(ctx, workspaceID, 45, credits.ReasonBotUsage, "bot-job-1"); err != nil {
		t.Fatalf("Debit: %v", err)
	}
	if _, err := ledger.Adjust(ctx, workspaceID, -25, "goodwill"); err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if _, err := ledger.Refund(ctx, workspaceID, 10, "payment-1"); err != nil {
		t.Fatalf("Refund: %v", err)
	}

	balance, err := ledger.Balance(ctx, workspaceID)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if want := int64(360); balance != want {
		t.Errorf("balance = %d, want %d", balance, want)
	}
	if got := ledgerSum(ctx, t, pool, workspaceID); got != balance {
		t.Errorf("ledger sum = %d, balance = %d", got, balance)
	}
}

func TestDebitRefusesToOverdraw(t *testing.T) {
	ctx, pool, ledger, workspaceID := setup(t)

	if _, err := ledger.Grant(ctx, workspaceID, 30, "september"); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	_, err := ledger.Debit(ctx, workspaceID, 31, credits.ReasonTranscribeUsage, "meeting-1")
	if !errors.Is(err, credits.ErrInsufficientCredits) {
		t.Fatalf("Debit = %v, want ErrInsufficientCredits", err)
	}
	if got := ledgerSum(ctx, t, pool, workspaceID); got != 30 {
		t.Errorf("ledger sum = %d, want 30: the rejected debit was written", got)
	}
	if got := countEntries(ctx, t, pool, workspaceID); got != 1 {
		t.Errorf("entry count = %d, want 1", got)
	}
}

func TestDebitOfTheWholeBalanceSucceeds(t *testing.T) {
	ctx, _, ledger, workspaceID := setup(t)

	if _, err := ledger.Grant(ctx, workspaceID, 30, "september"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	entry, err := ledger.Debit(ctx, workspaceID, 30, credits.ReasonAskUsage, "ask-1")
	if err != nil {
		t.Fatalf("Debit: %v", err)
	}
	if entry.DeltaMinutes != -30 {
		t.Errorf("delta = %d, want -30", entry.DeltaMinutes)
	}
	if entry.Reason != credits.ReasonAskUsage {
		t.Errorf("reason = %q, want %q", entry.Reason, credits.ReasonAskUsage)
	}
	if entry.RefID != "ask-1" {
		t.Errorf("ref id = %q, want ask-1", entry.RefID)
	}

	balance, err := ledger.Balance(ctx, workspaceID)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if balance != 0 {
		t.Errorf("balance = %d, want 0", balance)
	}
}

func TestDebitRejectsBadArguments(t *testing.T) {
	ctx, _, ledger, workspaceID := setup(t)

	if _, err := ledger.Debit(ctx, workspaceID, 0, credits.ReasonBotUsage, ""); !errors.Is(err, credits.ErrInvalidAmount) {
		t.Errorf("zero minutes = %v, want ErrInvalidAmount", err)
	}
	if _, err := ledger.Debit(ctx, workspaceID, -5, credits.ReasonBotUsage, ""); !errors.Is(err, credits.ErrInvalidAmount) {
		t.Errorf("negative minutes = %v, want ErrInvalidAmount", err)
	}
	if _, err := ledger.Debit(ctx, workspaceID, 5, credits.ReasonGrant, ""); !errors.Is(err, credits.ErrInvalidReason) {
		t.Errorf("credit reason = %v, want ErrInvalidReason", err)
	}
	if _, err := ledger.Grant(ctx, uuid.New(), 5, ""); err == nil {
		t.Error("granting to an unknown workspace succeeded")
	}
}

func TestConcurrentDebitsOnlyLetTheAffordableOnesThrough(t *testing.T) {
	ctx, pool, ledger, workspaceID := setup(t)

	if _, err := ledger.Grant(ctx, workspaceID, 100, "september"); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	const (
		goroutines = 10
		cost       = 30
	)
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ledger.Debit(ctx, workspaceID, cost, credits.ReasonBotUsage, "")
			switch {
			case err == nil:
				mu.Lock()
				succeeded++
				mu.Unlock()
			case errors.Is(err, credits.ErrInsufficientCredits):
			default:
				t.Errorf("Debit: %v", err)
			}
		}()
	}
	wg.Wait()

	if succeeded != 3 {
		t.Errorf("successful debits = %d, want 3", succeeded)
	}
	balance, err := ledger.Balance(ctx, workspaceID)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if balance != 10 {
		t.Errorf("balance = %d, want 10", balance)
	}
	if got := ledgerSum(ctx, t, pool, workspaceID); got != balance {
		t.Errorf("ledger sum = %d, balance = %d", got, balance)
	}
}

func TestRandomOperationsNeverProduceANegativeBalance(t *testing.T) {
	ctx, pool, ledger, workspaceID := setup(t)

	source := rand.New(rand.NewPCG(1, 2))
	expected := int64(0)
	for i := 0; i < 300; i++ {
		switch source.IntN(3) {
		case 0:
			minutes := int32(source.IntN(50) + 1)
			if _, err := ledger.Grant(ctx, workspaceID, minutes, ""); err != nil {
				t.Fatalf("Grant: %v", err)
			}
			expected += int64(minutes)
		case 1:
			minutes := int32(source.IntN(50) + 1)
			if _, err := ledger.Purchase(ctx, workspaceID, minutes, ""); err != nil {
				t.Fatalf("Purchase: %v", err)
			}
			expected += int64(minutes)
		default:
			minutes := int32(source.IntN(80) + 1)
			_, err := ledger.Debit(ctx, workspaceID, minutes, credits.ReasonTranscribeUsage, "")
			switch {
			case err == nil:
				expected -= int64(minutes)
			case errors.Is(err, credits.ErrInsufficientCredits):
				if int64(minutes) <= expected {
					t.Fatalf("debit of %d was refused with a balance of %d", minutes, expected)
				}
			default:
				t.Fatalf("Debit: %v", err)
			}
		}

		balance, err := ledger.Balance(ctx, workspaceID)
		if err != nil {
			t.Fatalf("Balance: %v", err)
		}
		if balance < 0 {
			t.Fatalf("balance went negative: %d", balance)
		}
		if balance != expected {
			t.Fatalf("balance = %d, want %d", balance, expected)
		}
	}

	if got := ledgerSum(ctx, t, pool, workspaceID); got != expected {
		t.Errorf("ledger sum = %d, want %d", got, expected)
	}
}

func TestAdjustCannotPushTheBalanceBelowZero(t *testing.T) {
	ctx, _, ledger, workspaceID := setup(t)

	if _, err := ledger.Grant(ctx, workspaceID, 10, ""); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if _, err := ledger.Adjust(ctx, workspaceID, -11, "correction"); !errors.Is(err, credits.ErrInsufficientCredits) {
		t.Fatalf("Adjust = %v, want ErrInsufficientCredits", err)
	}
	if _, err := ledger.Adjust(ctx, workspaceID, 0, "no-op"); !errors.Is(err, credits.ErrInvalidAmount) {
		t.Fatalf("zero Adjust = %v, want ErrInvalidAmount", err)
	}
	entry, err := ledger.Adjust(ctx, workspaceID, -10, "correction")
	if err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if entry.Reason != credits.ReasonAdjust {
		t.Errorf("reason = %q, want %q", entry.Reason, credits.ReasonAdjust)
	}
}

func setup(t *testing.T) (context.Context, *pgxpool.Pool, *credits.Ledger, uuid.UUID) {
	t.Helper()
	pool := dbtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx, pool, credits.NewLedger(pool), dbtest.NewWorkspace(t, pool)
}

func ledgerSum(ctx context.Context, t *testing.T, pool *pgxpool.Pool, workspaceID uuid.UUID) int64 {
	t.Helper()
	var sum int64
	err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta_minutes), 0)::bigint FROM credit_ledger WHERE workspace_id = $1`,
		workspaceID,
	).Scan(&sum)
	if err != nil {
		t.Fatalf("sum ledger: %v", err)
	}
	return sum
}

func countEntries(ctx context.Context, t *testing.T, pool *pgxpool.Pool, workspaceID uuid.UUID) int {
	t.Helper()
	var count int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM credit_ledger WHERE workspace_id = $1`, workspaceID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	return count
}
