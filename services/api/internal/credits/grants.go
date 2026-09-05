package credits

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const (
	DefaultGrantMinutes int32 = 300
	DefaultGrantBatch   int32 = 200
	LockKeyMonthlyGrant int64 = 0x6166776772616e74
	grantRefPrefix            = "grant:"
	expiryRefPrefix           = "grant_expiry:"
)

var ErrInvalidGrantMinutes = errors.New("credits: the monthly grant must be a positive number of minutes")

type GranterOptions struct {
	Pool      *pgxpool.Pool
	Ledger    *Ledger
	Minutes   int32
	BatchSize int32
	Logger    zerolog.Logger
	Clock     func() time.Time
}

type Granter struct {
	pool      *pgxpool.Pool
	ledger    *Ledger
	minutes   int32
	batchSize int32
	logger    zerolog.Logger
	clock     func() time.Time
}

type GrantRun struct {
	Period         time.Time
	Granted        int
	GrantedMinutes int64
	Expired        int
	ExpiredMinutes int64
}

func NewGranter(options GranterOptions) (*Granter, error) {
	if options.Pool == nil {
		return nil, errors.New("credits: a database pool is required")
	}
	if options.Minutes <= 0 {
		return nil, ErrInvalidGrantMinutes
	}

	ledger := options.Ledger
	if ledger == nil {
		ledger = NewLedger(options.Pool)
	}
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultGrantBatch
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}

	return &Granter{
		pool:      options.Pool,
		ledger:    ledger,
		minutes:   options.Minutes,
		batchSize: batchSize,
		logger:    options.Logger,
		clock:     clock,
	}, nil
}

func (g *Granter) Minutes() int32 {
	return g.minutes
}

func (g *Granter) RunOnce(ctx context.Context) (GrantRun, error) {
	now := g.clock().UTC()
	run := GrantRun{Period: PeriodOf(now)}

	expired, expiredMinutes, err := g.expireDueGrants(ctx, now)
	if err != nil {
		return run, err
	}
	run.Expired = expired
	run.ExpiredMinutes = expiredMinutes

	granted, grantedMinutes, err := g.grantCurrentPeriod(ctx, run.Period)
	if err != nil {
		return run, err
	}
	run.Granted = granted
	run.GrantedMinutes = grantedMinutes
	return run, nil
}

func (g *Granter) expireDueGrants(ctx context.Context, now time.Time) (int, int64, error) {
	var (
		count   int
		minutes int64
	)
	for {
		ids, err := g.dueGrantIDs(ctx, now)
		if err != nil {
			return count, minutes, err
		}
		if len(ids) == 0 {
			return count, minutes, nil
		}
		progressed := false
		for _, id := range ids {
			taken, err := g.expireGrant(ctx, id, now)
			if err != nil {
				return count, minutes, err
			}
			if taken < 0 {
				continue
			}
			progressed = true
			count++
			minutes += taken
		}
		if !progressed {
			return count, minutes, nil
		}
	}
}

func (g *Granter) dueGrantIDs(ctx context.Context, now time.Time) ([]uuid.UUID, error) {
	rows, err := g.pool.Query(ctx,
		`SELECT id FROM credit_grants
WHERE expired_at IS NULL AND expires_at <= $1
ORDER BY period, id
LIMIT $2`,
		now, g.batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list due credit grants: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan due credit grant: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due credit grants: %w", err)
	}
	return ids, nil
}

func (g *Granter) expireGrant(ctx context.Context, grantID uuid.UUID, now time.Time) (int64, error) {
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return -1, fmt.Errorf("begin grant expiry transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var (
		workspaceID    uuid.UUID
		grantMinutes   int32
		expiredMinutes int32
		grantedAt      time.Time
	)
	err = tx.QueryRow(ctx,
		`SELECT workspace_id, minutes, expired_minutes, created_at
FROM credit_grants
WHERE id = $1 AND expired_at IS NULL
FOR UPDATE`,
		grantID,
	).Scan(&workspaceID, &grantMinutes, &expiredMinutes, &grantedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return -1, nil
		}
		return -1, fmt.Errorf("lock credit grant: %w", err)
	}

	if err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return -1, err
	}

	balance, err := balanceInTx(ctx, tx, workspaceID)
	if err != nil {
		return -1, err
	}
	usedSince, err := usageSince(ctx, tx, workspaceID, grantedAt)
	if err != nil {
		return -1, err
	}

	taken := ExpiryMinutes(balance, int64(grantMinutes-expiredMinutes), usedSince)
	if taken > 0 {
		if _, err := g.ledger.applyTx(ctx, tx, workspaceID, -int32(taken), ReasonAdjust, expiryRefPrefix+grantID.String()); err != nil {
			return -1, fmt.Errorf("expire grant %s: %w", grantID, err)
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE credit_grants SET expired_at = $2, expired_minutes = expired_minutes + $3 WHERE id = $1`,
		grantID, now, int32(taken),
	); err != nil {
		return -1, fmt.Errorf("mark credit grant expired: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return -1, fmt.Errorf("commit grant expiry: %w", err)
	}
	return taken, nil
}

func (g *Granter) grantCurrentPeriod(ctx context.Context, period time.Time) (int, int64, error) {
	expiresAt := PeriodEnd(period)
	var (
		count   int
		minutes int64
		cursor  uuid.UUID
	)
	for {
		workspaceIDs, err := g.workspacesWithoutGrant(ctx, period, cursor)
		if err != nil {
			return count, minutes, err
		}
		if len(workspaceIDs) == 0 {
			return count, minutes, nil
		}
		for _, workspaceID := range workspaceIDs {
			cursor = workspaceID
			created, err := g.grantWorkspace(ctx, workspaceID, period, expiresAt)
			if err != nil {
				return count, minutes, err
			}
			if created {
				count++
				minutes += int64(g.minutes)
			}
		}
	}
}

func (g *Granter) workspacesWithoutGrant(ctx context.Context, period time.Time, cursor uuid.UUID) ([]uuid.UUID, error) {
	rows, err := g.pool.Query(ctx,
		`SELECT w.id FROM workspaces w
LEFT JOIN credit_grants g ON g.workspace_id = w.id AND g.period = $1::date
WHERE g.id IS NULL AND w.id > $2
ORDER BY w.id
LIMIT $3`,
		period, cursor, g.batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list workspaces without a grant: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workspaces: %w", err)
	}
	return ids, nil
}

func (g *Granter) grantWorkspace(ctx context.Context, workspaceID uuid.UUID, period, expiresAt time.Time) (bool, error) {
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin monthly grant transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var grantID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO credit_grants (workspace_id, period, minutes, expires_at)
VALUES ($1, $2::date, $3, $4)
ON CONFLICT (workspace_id, period) DO NOTHING
RETURNING id`,
		workspaceID, period, g.minutes, expiresAt,
	).Scan(&grantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("write credit grant: %w", err)
	}

	if _, err := g.ledger.applyTx(ctx, tx, workspaceID, g.minutes, ReasonGrant, grantRefPrefix+grantID.String()); err != nil {
		if errors.Is(err, ErrWorkspaceNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("grant workspace %s: %w", workspaceID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit monthly grant: %w", err)
	}
	return true, nil
}

func balanceInTx(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID) (int64, error) {
	var balance int64
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta_minutes), 0)::bigint FROM credit_ledger WHERE workspace_id = $1`,
		workspaceID,
	).Scan(&balance)
	if err != nil {
		return 0, fmt.Errorf("read credit balance: %w", err)
	}
	return balance, nil
}

func usageSince(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, since time.Time) (int64, error) {
	var used int64
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(-delta_minutes), 0)::bigint
FROM credit_ledger
WHERE workspace_id = $1
  AND created_at >= $2
  AND reason IN ('bot_usage', 'transcribe_usage', 'summary_usage', 'ask_usage')`,
		workspaceID, since,
	).Scan(&used)
	if err != nil {
		return 0, fmt.Errorf("read usage since the grant: %w", err)
	}
	return used, nil
}
