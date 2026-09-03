//go:build integration

package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"

	"github.com/judeotine/afterword/services/api/internal/accounts"
	"github.com/judeotine/afterword/services/api/internal/api"
	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

type libraryHarness struct {
	*harness
	pool    *pgxpool.Pool
	store   storage.Client
	memory  *storage.Memory
	buckets storage.Buckets
	queue   *jobs.Queue
	service *meetings.Service
}

func newLibraryHarness(t *testing.T) *libraryHarness {
	t.Helper()
	return newLibraryHarnessWith(t, storage.NewMemory())
}

func newLibraryHarnessWith(t *testing.T, memory *storage.Memory) *libraryHarness {
	t.Helper()
	pool := dbtest.New(t)
	return buildLibraryHarness(t, pool, memory, memory)
}

func buildLibraryHarness(t *testing.T, pool *pgxpool.Pool, client storage.Client, memory *storage.Memory) *libraryHarness {
	t.Helper()
	return buildLibrary(t, pool, client, memory, storage.Buckets{}.WithDefaults())
}

func buildLibrary(t *testing.T, pool *pgxpool.Pool, client storage.Client, memory *storage.Memory, buckets storage.Buckets) *libraryHarness {
	t.Helper()

	store, err := auth.NewStore(pool)
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	accountsService, err := accounts.NewService(pool)
	if err != nil {
		t.Fatalf("new accounts service: %v", err)
	}
	tokens, err := auth.NewTokenIssuer([]byte(harnessSecret))
	if err != nil {
		t.Fatalf("new token issuer: %v", err)
	}
	refresh, err := auth.NewRefreshManager(auth.RefreshManagerOptions{Store: store})
	if err != nil {
		t.Fatalf("new refresh manager: %v", err)
	}
	middleware, err := auth.NewMiddleware(auth.MiddlewareOptions{Issuer: tokens, Memberships: accountsService})
	if err != nil {
		t.Fatalf("new middleware: %v", err)
	}

	sender := &captureSender{}
	otp, err := auth.NewOTPService(auth.OTPServiceOptions{
		Store:       store,
		EmailSender: sender,
		SMSSender:   sender,
		HashCost:    bcrypt.MinCost,
	})
	if err != nil {
		t.Fatalf("new otp service: %v", err)
	}

	queue := jobs.NewQueue(pool)
	library, err := meetings.NewService(meetings.ServiceOptions{
		Pool:    pool,
		Storage: client,
		Buckets: buckets,
		Jobs:    queue,
	})
	if err != nil {
		t.Fatalf("new meetings service: %v", err)
	}

	server, err := api.NewServer(api.ServerOptions{
		Accounts:   accountsService,
		OTP:        otp,
		Tokens:     tokens,
		Refresh:    refresh,
		Middleware: middleware,
		Meetings:   library,
		Email:      sender,
		AppBaseURL: "http://localhost:3000",
	})
	if err != nil {
		t.Fatalf("new api server: %v", err)
	}

	return &libraryHarness{
		harness: &harness{
			t:      t,
			sender: sender,
			handler: httpx.NewRouter(httpx.RouterOptions{
				Logger:        zerolog.Nop(),
				AllowedOrigin: "http://localhost:3000",
				Mount:         server.Routes,
			}),
		},
		pool:    pool,
		store:   client,
		memory:  memory,
		buckets: buckets,
		queue:   queue,
		service: library,
	}
}

type meetingPayload struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	OwnerUserID string  `json:"owner_user_id"`
	Title       string  `json:"title"`
	Source      string  `json:"source"`
	Platform    string  `json:"platform"`
	DurationS   int32   `json:"duration_s"`
	Visibility  string  `json:"visibility"`
	FolderID    string  `json:"folder_id"`
	Status      string  `json:"status"`
	AudioBytes  *int64  `json:"audio_bytes"`
	Transcript  *int64  `json:"transcript_bytes"`
	StartedAt   *string `json:"started_at"`
	CreatedAt   string  `json:"created_at"`
}

type uploadPayload struct {
	AudioURL          string            `json:"audio_url"`
	TranscriptURL     string            `json:"transcript_url"`
	ExpiresAt         time.Time         `json:"expires_at"`
	MaxBytes          int64             `json:"max_bytes"`
	AudioHeaders      map[string]string `json:"audio_headers"`
	TranscriptHeaders map[string]string `json:"transcript_headers"`
}

type createMeetingPayload struct {
	Meeting meetingPayload `json:"meeting"`
	Upload  uploadPayload  `json:"upload"`
}

type meetingDetailPayload struct {
	Meeting  meetingPayload `json:"meeting"`
	Folder   *folderPayload `json:"folder"`
	Download struct {
		Audio      *presignPayload `json:"audio"`
		Transcript *presignPayload `json:"transcript"`
	} `json:"download"`
}

type presignPayload struct {
	Method    string    `json:"method"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type meetingListPayload struct {
	Meetings   []meetingPayload `json:"meetings"`
	NextCursor string           `json:"next_cursor"`
}

type folderPayload struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
}

type folderListPayload struct {
	Folders []folderPayload `json:"folders"`
}

type segmentPayload struct {
	Seq     int32   `json:"seq"`
	Speaker string  `json:"speaker"`
	StartS  float64 `json:"start_s"`
	EndS    float64 `json:"end_s"`
	Text    string  `json:"text"`
}

type segmentListPayload struct {
	Segments []segmentPayload `json:"segments"`
	Total    int64            `json:"total"`
	NextSeq  *int32           `json:"next_seq"`
}

type validationEnvelope struct {
	Error struct {
		Code    string             `json:"code"`
		Message string             `json:"message"`
		Fields  []httpx.FieldError `json:"fields"`
	} `json:"error"`
}

func (h *libraryHarness) as(session session) []func(*http.Request) {
	return []func(*http.Request){withBearer(session.AccessToken), withWorkspace(session.Workspace.ID)}
}

func (h *libraryHarness) call(method, path string, body any, decorate ...func(*http.Request)) response {
	h.t.Helper()
	return h.do(method, path, body, decorate...)
}

func (h *libraryHarness) createMeeting(session session, body map[string]any) createMeetingPayload {
	h.t.Helper()
	result := h.call(http.MethodPost, "/v1/meetings", body, h.as(session)...)
	if result.Status != http.StatusCreated {
		h.t.Fatalf("create meeting: status %d, body %s", result.Status, result.Body)
	}
	var payload createMeetingPayload
	result.decode(h.t, &payload)
	return payload
}

func (h *libraryHarness) join(t *testing.T, owner session, email string, role auth.Role) session {
	t.Helper()
	invited := h.call(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites",
		map[string]string{"email": email, "role": string(role)}, h.as(owner)...)
	if invited.Status != http.StatusCreated {
		t.Fatalf("create invite: status %d, body %s", invited.Status, invited.Body)
	}
	var invite struct {
		Token string `json:"token"`
	}
	invited.decode(t, &invite)

	member := h.signIn(email)
	accepted := h.call(http.MethodPost, "/v1/invites/"+invite.Token+"/accept", nil, withBearer(member.AccessToken))
	if accepted.Status != http.StatusOK && accepted.Status != http.StatusCreated {
		t.Fatalf("accept invite: status %d, body %s", accepted.Status, accepted.Body)
	}
	member.Workspace.ID = owner.Workspace.ID
	return member
}

func (h *libraryHarness) jobExists(t *testing.T, kind, key string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := h.queue.GetByIdempotencyKey(ctx, kind, key)
	return err == nil
}
