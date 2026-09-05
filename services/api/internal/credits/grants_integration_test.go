//go:build integration

package credits_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
}

func newGranter(t *testing.T, pool *pgxpool.Pool, ledger *credits.Ledger, clock *testClock, minutes int32) *credits.Granter {
	t.Helper()
	granter, err := credits.NewGranter(credits.GranterOptions{
		Pool:    pool,
		Ledger:  ledger,
		Minutes: minutes,
		Clock:   clock.Now,
	})
	if err != nil {
		t.Fatalf("new granter: %v", err)
	}
	return granter
}

func grantRows(ctx context.Context, t *testing.T, pool *pgxpool.Pool, workspaceID uuid.UUID) []struct {
	Period         time.Time
	Minutes        int32
	ExpiredMinutes int32
	Expired        bool
} {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT period, minutes, expired_minutes, expired_at IS NOT NULL FROM credit_grants WHERE workspace_id = $1 ORDER BY period`,
		workspaceID,
	)
	if err != nil {
		t.Fatalf("read credit grants: %v", err)
	}
	defer rows.Close()

	var result []struct {
		Period         time.Time
		Minutes        int32
		ExpiredMinutes int32
		Expired        bool
	}
	for rows.Next() {
		var row struct {
			Period         time.Time
			Minutes        int32
			ExpiredMinutes int32
			Expired        bool
		}
		if err := rows.Scan(&row.Period, &row.Minutes, &row.ExpiredMinutes, &row.Expired); err != nil {
			t.Fatalf("scan credit grant: %v", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate credit grants: %v", err)
	}
	return result
}

func TestMonthlyGrantIsWrittenOncePerWorkspacePerMonth(t *testing.T) {
	ctx, pool, ledger, workspaceID := setup(t)
	clock := &testClock{now: time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC)}
	granter := newGranter(t, pool, ledger, clock, 300)

	first, err := granter.RunOnce(ctx)
	if err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if first.Granted != 1 {
		t.Fatalf("first run granted %d workspaces, want 1", first.Granted)
	}

	for i := 0; i < 4; i++ {
		clock.Set(clock.Now().Add(6 * time.Hour))
		again, err := granter.RunOnce(ctx)
		if err != nil {
			t.Fatalf("repeat RunOnce: %v", err)
		}
		if again.Granted != 0 || again.Expired != 0 {
			t.Fatalf("repeat run granted %d and expired %d, want 0 and 0", again.Granted, again.Expired)
		}
	}

	if balance := balanceOf(ctx, t, ledger, workspaceID); balance != 300 {
		t.Fatalf("balance = %d, want 300 after five runs in one month", balance)
	}
	if rows := grantRows(ctx, t, pool, workspaceID); len(rows) != 1 {
		t.Fatalf("credit_grants rows = %d, want 1", len(rows))
	}
}

func TestMonthlyGrantIsIdempotentAcrossMonths(t *testing.T) {
	ctx, pool, ledger, workspaceID := setup(t)
	clock := &testClock{now: time.Date(2026, time.January, 2, 1, 0, 0, 0, time.UTC)}
	granter := newGranter(t, pool, ledger, clock, 300)

	if _, err := granter.RunOnce(ctx); err != nil {
		t.Fatalf("january RunOnce: %v", err)
	}
	if _, err := granter.RunOnce(ctx); err != nil {
		t.Fatalf("january second RunOnce: %v", err)
	}

	clock.Set(time.Date(2026, time.February, 1, 0, 30, 0, 0, time.UTC))
	february, err := granter.RunOnce(ctx)
	if err != nil {
		t.Fatalf("february RunOnce: %v", err)
	}
	if february.Granted != 1 {
		t.Fatalf("february granted %d, want 1", february.Granted)
	}
	if february.Expired != 1 || february.ExpiredMinutes != 300 {
		t.Fatalf("february expired %d grants for %d minutes, want 1 and 300", february.Expired, february.ExpiredMinutes)
	}
	if _, err := granter.RunOnce(ctx); err != nil {
		t.Fatalf("february second RunOnce: %v", err)
	}

	clock.Set(time.Date(2026, time.March, 3, 4, 0, 0, 0, time.UTC))
	if _, err := granter.RunOnce(ctx); err != nil {
		t.Fatalf("march RunOnce: %v", err)
	}

	rows := grantRows(ctx, t, pool, workspaceID)
	if len(rows) != 3 {
		t.Fatalf("credit_grants rows = %d, want 3", len(rows))
	}
	wantPeriods := []time.Month{time.January, time.February, time.March}
	for i, row := range rows {
		if row.Period.UTC().Month() != wantPeriods[i] || row.Period.UTC().Day() != 1 {
			t.Errorf("row %d period = %s, want the first of %s", i, row.Period, wantPeriods[i])
		}
		if row.Minutes != 300 {
			t.Errorf("row %d minutes = %d, want 300", i, row.Minutes)
		}
	}
	if !rows[0].Expired || rows[0].ExpiredMinutes != 300 {
		t.Errorf("january grant expired=%v for %d minutes, want true and 300", rows[0].Expired, rows[0].ExpiredMinutes)
	}
	if !rows[1].Expired || rows[1].ExpiredMinutes != 300 {
		t.Errorf("february grant expired=%v for %d minutes, want true and 300", rows[1].Expired, rows[1].ExpiredMinutes)
	}
	if rows[2].Expired {
		t.Error("march grant is already expired")
	}
	if balance := balanceOf(ctx, t, ledger, workspaceID); balance != 300 {
		t.Fatalf("balance = %d, want 300 after three months of grants and two expiries", balance)
	}
}

func TestExpiryTakesOnlyTheUnusedRemainderOfTheGrant(t *testing.T) {
	ctx, pool, ledger, workspaceID := setup(t)
	clock := &testClock{now: time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)}
	granter := newGranter(t, pool, ledger, clock, 300)

	if _, err := granter.RunOnce(ctx); err != nil {
		t.Fatalf("january RunOnce: %v", err)
	}
	if _, err := ledger.Debit(ctx, workspaceID, 250, credits.ReasonBotUsage, "bot-1"); err != nil {
		t.Fatalf("Debit: %v", err)
	}

	clock.Set(time.Date(2026, time.February, 1, 1, 0, 0, 0, time.UTC))
	run, err := granter.RunOnce(ctx)
	if err != nil {
		t.Fatalf("february RunOnce: %v", err)
	}
	if run.ExpiredMinutes != 50 {
		t.Fatalf("expired %d minutes, want 50", run.ExpiredMinutes)
	}
	if balance := balanceOf(ctx, t, ledger, workspaceID); balance != 300 {
		t.Fatalf("balance = %d, want 300: the leftover 50 expires and february grants 300", balance)
	}
	rows := grantRows(ctx, t, pool, workspaceID)
	if rows[0].ExpiredMinutes != 50 {
		t.Errorf("january expired_minutes = %d, want 50", rows[0].ExpiredMinutes)
	}
}

func TestExpiryNeverTakesPurchasedCredits(t *testing.T) {
	ctx, pool, ledger, workspaceID := setup(t)
	clock := &testClock{now: time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)}
	granter := newGranter(t, pool, ledger, clock, 300)

	if _, err := ledger.Purchase(ctx, workspaceID, 1000, "payment-1"); err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if _, err := granter.RunOnce(ctx); err != nil {
		t.Fatalf("january RunOnce: %v", err)
	}
	if _, err := ledger.Debit(ctx, workspaceID, 1200, credits.ReasonTranscribeUsage, "meeting-1"); err != nil {
		t.Fatalf("Debit: %v", err)
	}

	clock.Set(time.Date(2026, time.February, 1, 2, 0, 0, 0, time.UTC))
	run, err := granter.RunOnce(ctx)
	if err != nil {
		t.Fatalf("february RunOnce: %v", err)
	}
	if run.ExpiredMinutes != 0 {
		t.Fatalf("expired %d minutes, want 0: the grant was already spent", run.ExpiredMinutes)
	}
	if balance := balanceOf(ctx, t, ledger, workspaceID); balance != 400 {
		t.Fatalf("balance = %d, want 400: 100 purchased plus the february grant", balance)
	}
}

func TestExpiryNeverPushesTheBalanceNegative(t *testing.T) {
	ctx, pool, ledger, workspaceID := setup(t)
	clock := &testClock{now: time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)}
	granter := newGranter(t, pool, ledger, clock, 300)

	if _, err := granter.RunOnce(ctx); err != nil {
		t.Fatalf("january RunOnce: %v", err)
	}
	if _, err := ledger.Debit(ctx, workspaceID, 300, credits.ReasonAskUsage, "ask-1"); err != nil {
		t.Fatalf("Debit: %v", err)
	}

	clock.Set(time.Date(2026, time.February, 1, 0, 5, 0, 0, time.UTC))
	run, err := granter.RunOnce(ctx)
	if err != nil {
		t.Fatalf("february RunOnce: %v", err)
	}
	if run.ExpiredMinutes != 0 {
		t.Fatalf("expired %d minutes, want 0", run.ExpiredMinutes)
	}
	if balance := balanceOf(ctx, t, ledger, workspaceID); balance != 300 {
		t.Fatalf("balance = %d, want 300", balance)
	}
	if sum := ledgerSum(ctx, t, pool, workspaceID); sum != 300 {
		t.Fatalf("ledger sum = %d, want 300", sum)
	}
}

func TestSkippedMonthsAreAllExpiredOnTheNextRun(t *testing.T) {
	ctx, pool, ledger, workspaceID := setup(t)
	clock := &testClock{now: time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)}
	granter := newGranter(t, pool, ledger, clock, 100)

	if _, err := granter.RunOnce(ctx); err != nil {
		t.Fatalf("january RunOnce: %v", err)
	}
	clock.Set(time.Date(2026, time.February, 2, 0, 0, 0, 0, time.UTC))
	if _, err := granter.RunOnce(ctx); err != nil {
		t.Fatalf("february RunOnce: %v", err)
	}

	clock.Set(time.Date(2026, time.May, 9, 0, 0, 0, 0, time.UTC))
	run, err := granter.RunOnce(ctx)
	if err != nil {
		t.Fatalf("may RunOnce: %v", err)
	}
	if run.Expired != 1 {
		t.Fatalf("expired %d grants, want the february grant only", run.Expired)
	}
	if run.Granted != 1 {
		t.Fatalf("granted %d, want 1", run.Granted)
	}
	if balance := balanceOf(ctx, t, ledger, workspaceID); balance != 100 {
		t.Fatalf("balance = %d, want 100", balance)
	}
	rows := grantRows(ctx, t, pool, workspaceID)
	if len(rows) != 3 {
		t.Fatalf("credit_grants rows = %d, want 3 (january, february, may)", len(rows))
	}
}

func TestConcurrentGrantRunsStillWriteOneGrant(t *testing.T) {
	ctx, pool, ledger, workspaceID := setup(t)
	clock := &testClock{now: time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)}

	const runners = 6
	var (
		wait   sync.WaitGroup
		mu     sync.Mutex
		total  int
		errors []error
	)
	wait.Add(runners)
	for i := 0; i < runners; i++ {
		go func() {
			defer wait.Done()
			granter := newGranterOrRecord(pool, ledger, clock, 300, &mu, &errors)
			if granter == nil {
				return
			}
			run, err := granter.RunOnce(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errors = append(errors, err)
				return
			}
			total += run.Granted
		}()
	}
	wait.Wait()

	for _, err := range errors {
		t.Fatalf("concurrent RunOnce: %v", err)
	}
	if total != 1 {
		t.Fatalf("granted %d times across %d concurrent runs, want 1", total, runners)
	}
	if balance := balanceOf(ctx, t, ledger, workspaceID); balance != 300 {
		t.Fatalf("balance = %d, want 300", balance)
	}
}

func newGranterOrRecord(pool *pgxpool.Pool, ledger *credits.Ledger, clock *testClock, minutes int32, mu *sync.Mutex, sink *[]error) *credits.Granter {
	granter, err := credits.NewGranter(credits.GranterOptions{
		Pool:    pool,
		Ledger:  ledger,
		Minutes: minutes,
		Clock:   clock.Now,
	})
	if err != nil {
		mu.Lock()
		*sink = append(*sink, err)
		mu.Unlock()
		return nil
	}
	return granter
}

func TestGrantsCoverEveryWorkspace(t *testing.T) {
	ctx, pool, ledger, first := setup(t)
	second := dbtest.NewWorkspace(t, pool)
	third := dbtest.NewWorkspace(t, pool)
	clock := &testClock{now: time.Date(2026, time.July, 4, 0, 0, 0, 0, time.UTC)}
	granter := newGranter(t, pool, ledger, clock, 300)

	run, err := granter.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if run.Granted != 3 {
		t.Fatalf("granted %d workspaces, want 3", run.Granted)
	}
	for _, workspaceID := range []uuid.UUID{first, second, third} {
		if balance := balanceOf(ctx, t, ledger, workspaceID); balance != 300 {
			t.Errorf("workspace %s balance = %d, want 300", workspaceID, balance)
		}
	}
}

func balanceOf(ctx context.Context, t *testing.T, ledger *credits.Ledger, workspaceID uuid.UUID) int64 {
	t.Helper()
	balance, err := ledger.Balance(ctx, workspaceID)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	return balance
}
