package credits

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Reason string

const (
	ReasonGrant           Reason = "grant"
	ReasonPurchase        Reason = "purchase"
	ReasonBotUsage        Reason = "bot_usage"
	ReasonTranscribeUsage Reason = "transcribe_usage"
	ReasonSummaryUsage    Reason = "summary_usage"
	ReasonAskUsage        Reason = "ask_usage"
	ReasonRefund          Reason = "refund"
	ReasonAdjust          Reason = "adjust"
)

var (
	ErrInsufficientCredits = errors.New("credits: insufficient credits")
	ErrInvalidAmount       = errors.New("credits: amount must be a non-zero number of minutes")
	ErrInvalidReason       = errors.New("credits: reason is not valid for this operation")
	ErrWorkspaceNotFound   = errors.New("credits: workspace not found")
)

func (r Reason) Valid() bool {
	switch r {
	case ReasonGrant, ReasonPurchase, ReasonBotUsage, ReasonTranscribeUsage,
		ReasonSummaryUsage, ReasonAskUsage, ReasonRefund, ReasonAdjust:
		return true
	default:
		return false
	}
}

func (r Reason) IsUsage() bool {
	switch r {
	case ReasonBotUsage, ReasonTranscribeUsage, ReasonSummaryUsage, ReasonAskUsage:
		return true
	default:
		return false
	}
}

type Entry struct {
	ID           uuid.UUID
	WorkspaceID  uuid.UUID
	DeltaMinutes int32
	Reason       Reason
	RefID        string
	CreatedAt    time.Time
}

type Ledger struct {
	pool *pgxpool.Pool
}

func NewLedger(pool *pgxpool.Pool) *Ledger {
	return &Ledger{pool: pool}
}

func (l *Ledger) Balance(ctx context.Context, workspaceID uuid.UUID) (int64, error) {
	var balance int64
	err := l.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta_minutes), 0)::bigint FROM credit_ledger WHERE workspace_id = $1`,
		workspaceID,
	).Scan(&balance)
	if err != nil {
		return 0, fmt.Errorf("read credit balance: %w", err)
	}
	return balance, nil
}

func (l *Ledger) Require(ctx context.Context, workspaceID uuid.UUID, minutes int32) (int64, error) {
	if minutes < 0 {
		return 0, ErrInvalidAmount
	}
	balance, err := l.Balance(ctx, workspaceID)
	if err != nil {
		return 0, err
	}
	if balance < int64(minutes) {
		return balance, fmt.Errorf("%w: balance %d, requested %d", ErrInsufficientCredits, balance, minutes)
	}
	return balance, nil
}

func (l *Ledger) Grant(ctx context.Context, workspaceID uuid.UUID, minutes int32, refID string) (*Entry, error) {
	if minutes <= 0 {
		return nil, ErrInvalidAmount
	}
	return l.apply(ctx, workspaceID, minutes, ReasonGrant, refID)
}

func (l *Ledger) Purchase(ctx context.Context, workspaceID uuid.UUID, minutes int32, refID string) (*Entry, error) {
	if minutes <= 0 {
		return nil, ErrInvalidAmount
	}
	return l.apply(ctx, workspaceID, minutes, ReasonPurchase, refID)
}

func (l *Ledger) Refund(ctx context.Context, workspaceID uuid.UUID, minutes int32, refID string) (*Entry, error) {
	if minutes <= 0 {
		return nil, ErrInvalidAmount
	}
	return l.apply(ctx, workspaceID, minutes, ReasonRefund, refID)
}

func (l *Ledger) Adjust(ctx context.Context, workspaceID uuid.UUID, deltaMinutes int32, refID string) (*Entry, error) {
	if deltaMinutes == 0 {
		return nil, ErrInvalidAmount
	}
	return l.apply(ctx, workspaceID, deltaMinutes, ReasonAdjust, refID)
}

func (l *Ledger) Debit(ctx context.Context, workspaceID uuid.UUID, minutes int32, reason Reason, refID string) (*Entry, error) {
	if minutes <= 0 {
		return nil, ErrInvalidAmount
	}
	if !reason.IsUsage() {
		return nil, fmt.Errorf("%w: %s is not a usage reason", ErrInvalidReason, reason)
	}
	return l.apply(ctx, workspaceID, -minutes, reason, refID)
}

func (l *Ledger) PurchaseTx(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, minutes int32, refID string) (*Entry, error) {
	if minutes <= 0 {
		return nil, ErrInvalidAmount
	}
	return l.applyTx(ctx, tx, workspaceID, minutes, ReasonPurchase, refID)
}

func (l *Ledger) apply(ctx context.Context, workspaceID uuid.UUID, deltaMinutes int32, reason Reason, refID string) (*Entry, error) {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin credit transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	entry, err := l.applyTx(ctx, tx, workspaceID, deltaMinutes, reason, refID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit credit transaction: %w", err)
	}
	return entry, nil
}

func (l *Ledger) applyTx(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, deltaMinutes int32, reason Reason, refID string) (*Entry, error) {
	if !reason.Valid() {
		return nil, fmt.Errorf("%w: %s", ErrInvalidReason, reason)
	}
	if err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return nil, err
	}

	var ref *string
	if refID != "" {
		ref = &refID
	}

	const insert = `INSERT INTO credit_ledger (workspace_id, delta_minutes, reason, ref_id)
SELECT $1, $2, $3, $4
WHERE $2 >= 0
   OR (SELECT COALESCE(SUM(delta_minutes), 0) FROM credit_ledger WHERE workspace_id = $1) + $2 >= 0
RETURNING id, workspace_id, delta_minutes, reason, ref_id, created_at`

	entry, err := scanEntry(tx.QueryRow(ctx, insert, workspaceID, deltaMinutes, string(reason), ref))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, insufficientCredits(ctx, tx, workspaceID, deltaMinutes)
		}
		return nil, fmt.Errorf("write credit ledger entry: %w", err)
	}
	return entry, nil
}

func lockWorkspace(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO credit_locks (workspace_id) VALUES ($1) ON CONFLICT (workspace_id) DO NOTHING`,
		workspaceID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation {
			return ErrWorkspaceNotFound
		}
		return fmt.Errorf("create credit lock: %w", err)
	}

	var locked uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT workspace_id FROM credit_locks WHERE workspace_id = $1 FOR UPDATE`,
		workspaceID,
	).Scan(&locked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWorkspaceNotFound
		}
		return fmt.Errorf("lock workspace credits: %w", err)
	}
	return nil
}

func insufficientCredits(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, deltaMinutes int32) error {
	var balance int64
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta_minutes), 0)::bigint FROM credit_ledger WHERE workspace_id = $1`,
		workspaceID,
	).Scan(&balance)
	if err != nil {
		return fmt.Errorf("%w: balance unavailable: %w", ErrInsufficientCredits, err)
	}
	return fmt.Errorf("%w: balance %d, requested %d", ErrInsufficientCredits, balance, -int64(deltaMinutes))
}

func scanEntry(row pgx.Row) (*Entry, error) {
	var (
		entry  Entry
		reason string
		ref    *string
	)
	if err := row.Scan(&entry.ID, &entry.WorkspaceID, &entry.DeltaMinutes, &reason, &ref, &entry.CreatedAt); err != nil {
		return nil, err
	}
	entry.Reason = Reason(reason)
	if ref != nil {
		entry.RefID = *ref
	}
	return &entry, nil
}
