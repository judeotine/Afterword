//go:build integration

package db_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
)

func TestQueriesScopeReadsByWorkspace(t *testing.T) {
	ctx, queries := newQueries(t)

	mine := newWorkspace(ctx, t, queries, "mine")
	theirs := newWorkspace(ctx, t, queries, "theirs")

	meeting, err := queries.CreateMeeting(ctx, sqlcgen.CreateMeetingParams{
		WorkspaceID:  mine.ID,
		Title:        "Standup",
		Source:       "desktop",
		ConsentState: "granted",
		Visibility:   "workspace",
		Status:       "ready",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}

	if _, err := queries.GetMeeting(ctx, sqlcgen.GetMeetingParams{ID: meeting.ID, WorkspaceID: mine.ID}); err != nil {
		t.Fatalf("GetMeeting in the owning workspace: %v", err)
	}
	_, err = queries.GetMeeting(ctx, sqlcgen.GetMeetingParams{ID: meeting.ID, WorkspaceID: theirs.ID})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetMeeting across workspaces = %v, want pgx.ErrNoRows", err)
	}

	rows, err := queries.ListMeetingsByWorkspace(ctx, sqlcgen.ListMeetingsByWorkspaceParams{
		WorkspaceID: theirs.ID,
		PageSize:    10,
	})
	if err != nil {
		t.Fatalf("ListMeetingsByWorkspace: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("listed %d meetings for a foreign workspace, want 0", len(rows))
	}

	affected, err := queries.DeleteMeeting(ctx, sqlcgen.DeleteMeetingParams{ID: meeting.ID, WorkspaceID: theirs.ID})
	if err != nil {
		t.Fatalf("DeleteMeeting: %v", err)
	}
	if affected != 0 {
		t.Fatalf("deleted %d rows from a foreign workspace, want 0", affected)
	}
}

func TestListMeetingsPagesByKeyset(t *testing.T) {
	ctx, queries := newQueries(t)
	workspace := newWorkspace(ctx, t, queries, "keyset")

	const total = 7
	created := make([]sqlcgen.Meeting, 0, total)
	for i := 0; i < total; i++ {
		meeting, err := queries.CreateMeeting(ctx, sqlcgen.CreateMeetingParams{
			WorkspaceID:  workspace.ID,
			Title:        fmt.Sprintf("Meeting %d", i),
			Source:       "bot",
			Platform:     stringPtr("meet"),
			ConsentState: "granted",
			Visibility:   "private",
			Status:       "ready",
		})
		if err != nil {
			t.Fatalf("CreateMeeting %d: %v", i, err)
		}
		created = append(created, meeting)
	}

	seen := map[uuid.UUID]bool{}
	params := sqlcgen.ListMeetingsByWorkspaceParams{WorkspaceID: workspace.ID, PageSize: 3}
	pages := 0
	for {
		page, err := queries.ListMeetingsByWorkspace(ctx, params)
		if err != nil {
			t.Fatalf("ListMeetingsByWorkspace: %v", err)
		}
		if len(page) == 0 {
			break
		}
		pages++
		if pages > total {
			t.Fatal("keyset pagination did not terminate")
		}
		for _, row := range page {
			if seen[row.ID] {
				t.Fatalf("meeting %s was returned twice", row.ID)
			}
			seen[row.ID] = true
		}
		last := page[len(page)-1]
		params.CursorCreatedAt = last.CreatedAt
		params.CursorID = &last.ID
	}

	if len(seen) != len(created) {
		t.Fatalf("paged over %d meetings, want %d", len(seen), len(created))
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3", pages)
	}
}

func TestTranscriptSegmentsStoreEmbeddingsAndCascade(t *testing.T) {
	ctx, queries := newQueries(t)
	workspace := newWorkspace(ctx, t, queries, "segments")

	meeting, err := queries.CreateMeeting(ctx, sqlcgen.CreateMeetingParams{
		WorkspaceID:  workspace.ID,
		Title:        "Design review",
		Source:       "desktop",
		ConsentState: "granted",
		Visibility:   "workspace",
		Status:       "ready",
	})
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}

	embedding := pgvector.NewVector(make([]float32, 768))
	segment, err := queries.CreateTranscriptSegment(ctx, sqlcgen.CreateTranscriptSegmentParams{
		MeetingID:   meeting.ID,
		WorkspaceID: workspace.ID,
		Seq:         0,
		Speaker:     stringPtr("Jude"),
		StartS:      0,
		EndS:        3.5,
		Text:        "the quarterly roadmap looks good",
		Embedding:   &embedding,
	})
	if err != nil {
		t.Fatalf("CreateTranscriptSegment: %v", err)
	}
	if segment.Embedding == "" {
		t.Error("embedding came back empty")
	}

	var matches int
	err = queries.DB().QueryRow(ctx,
		`SELECT count(*) FROM transcript_segments WHERE meeting_id = $1 AND tsv @@ websearch_to_tsquery('simple', 'roadmap')`,
		meeting.ID,
	).Scan(&matches)
	if err != nil {
		t.Fatalf("full text search: %v", err)
	}
	if matches != 1 {
		t.Errorf("full text matches = %d, want 1", matches)
	}

	if _, err := queries.DeleteMeeting(ctx, sqlcgen.DeleteMeetingParams{ID: meeting.ID, WorkspaceID: workspace.ID}); err != nil {
		t.Fatalf("DeleteMeeting: %v", err)
	}

	var remaining int
	if err := queries.DB().QueryRow(ctx, `SELECT count(*) FROM transcript_segments WHERE meeting_id = $1`, meeting.ID).Scan(&remaining); err != nil {
		t.Fatalf("count segments: %v", err)
	}
	if remaining != 0 {
		t.Errorf("segments left after the meeting was deleted = %d, want 0", remaining)
	}
}

func TestPaymentsAreUniquePerProviderReference(t *testing.T) {
	ctx, queries := newQueries(t)
	workspace := newWorkspace(ctx, t, queries, "payments")

	params := sqlcgen.CreatePaymentParams{
		WorkspaceID: workspace.ID,
		Provider:    "fake",
		ProviderRef: "ref-1",
		AmountMinor: 5000,
		Currency:    "UGX",
		Minutes:     300,
		Status:      "pending",
		Raw:         []byte(`{}`),
	}
	if _, err := queries.CreatePayment(ctx, params); err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	if _, err := queries.CreatePayment(ctx, params); err == nil {
		t.Fatal("a duplicate provider reference was accepted")
	}

	found, err := queries.GetPaymentByProviderRef(ctx, sqlcgen.GetPaymentByProviderRefParams{
		Provider:    "fake",
		ProviderRef: "ref-1",
	})
	if err != nil {
		t.Fatalf("GetPaymentByProviderRef: %v", err)
	}
	if found.WorkspaceID != workspace.ID {
		t.Errorf("workspace = %s, want %s", found.WorkspaceID, workspace.ID)
	}
}

func TestCreditLedgerQueriesReportTheBalance(t *testing.T) {
	ctx, queries := newQueries(t)
	workspace := newWorkspace(ctx, t, queries, "ledger")

	ledger := credits.NewLedger(queries.Pool())
	if _, err := ledger.Grant(ctx, workspace.ID, 300, ""); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if _, err := ledger.Debit(ctx, workspace.ID, 45, credits.ReasonBotUsage, "bot-1"); err != nil {
		t.Fatalf("Debit: %v", err)
	}

	balance, err := queries.GetCreditBalance(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("GetCreditBalance: %v", err)
	}
	if balance != 255 {
		t.Errorf("balance = %d, want 255", balance)
	}

	page, err := queries.ListCreditLedgerByWorkspace(ctx, sqlcgen.ListCreditLedgerByWorkspaceParams{
		WorkspaceID: workspace.ID,
		PageSize:    10,
	})
	if err != nil {
		t.Fatalf("ListCreditLedgerByWorkspace: %v", err)
	}
	if len(page) != 2 {
		t.Errorf("ledger rows = %d, want 2", len(page))
	}
}

func TestMembershipsAndJobsRoundTrip(t *testing.T) {
	ctx, queries := newQueries(t)
	workspace := newWorkspace(ctx, t, queries, "members")

	user, err := queries.CreateUser(ctx, sqlcgen.CreateUserParams{
		Email: stringPtr(fmt.Sprintf("%s@example.com", uuid.NewString())),
		Name:  "Jude",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := queries.CreateMembership(ctx, sqlcgen.CreateMembershipParams{
		WorkspaceID: workspace.ID,
		UserID:      user.ID,
		Role:        "owner",
	}); err != nil {
		t.Fatalf("CreateMembership: %v", err)
	}

	members, err := queries.ListUsersByWorkspace(ctx, sqlcgen.ListUsersByWorkspaceParams{
		WorkspaceID: workspace.ID,
		PageSize:    10,
	})
	if err != nil {
		t.Fatalf("ListUsersByWorkspace: %v", err)
	}
	if len(members) != 1 || members[0].ID != user.ID {
		t.Fatalf("members = %+v, want just %s", members, user.ID)
	}

	job, err := queries.CreateJob(ctx, sqlcgen.CreateJobParams{
		Kind:           "transcribe",
		Payload:        []byte(`{"meeting":"m1"}`),
		IdempotencyKey: stringPtr("meeting-1"),
		RunAt:          pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		MaxAttempts:    5,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.Status != "pending" {
		t.Errorf("status = %q, want pending", job.Status)
	}

	same, err := queries.GetJobByIdempotencyKey(ctx, sqlcgen.GetJobByIdempotencyKeyParams{
		Kind:           "transcribe",
		IdempotencyKey: stringPtr("meeting-1"),
	})
	if err != nil {
		t.Fatalf("GetJobByIdempotencyKey: %v", err)
	}
	if same.ID != job.ID {
		t.Errorf("job id = %s, want %s", same.ID, job.ID)
	}

	if _, err := queries.DeleteWorkspace(ctx, workspace.ID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	if _, err := queries.GetMembership(ctx, sqlcgen.GetMembershipParams{
		WorkspaceID: workspace.ID,
		UserID:      user.ID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetMembership after the workspace was deleted = %v, want pgx.ErrNoRows", err)
	}
}

type queries struct {
	*sqlcgen.Queries
	pool *pgxpool.Pool
}

func (q queries) DB() sqlcgen.DBTX { return q.pool }

func (q queries) Pool() *pgxpool.Pool { return q.pool }

func newQueries(t *testing.T) (context.Context, queries) {
	t.Helper()
	pool := dbtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ctx, queries{Queries: sqlcgen.New(pool), pool: pool}
}

func newWorkspace(ctx context.Context, t *testing.T, q queries, name string) sqlcgen.Workspace {
	t.Helper()
	workspace, err := q.CreateWorkspace(ctx, sqlcgen.CreateWorkspaceParams{
		Name:          name,
		Slug:          "ws-" + uuid.NewString(),
		BotName:       "Afterword Notetaker",
		RetentionDays: 365,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	return workspace
}

func stringPtr(value string) *string { return &value }
