//go:build integration

package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/botjobs"
)

const botJobsURL = "/v1/bot-jobs"

type botJobPayload struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	MeetingURL       string `json:"meeting_url"`
	Platform         string `json:"platform"`
	BotName          string `json:"bot_name"`
	ScheduledAt      string `json:"scheduled_at"`
	Status           string `json:"status"`
	EstimatedMinutes int32  `json:"estimated_minutes"`
	MinutesUsed      int32  `json:"minutes_used"`
	CreatedAt        string `json:"created_at"`
}

type botJobListPayload struct {
	BotJobs    []botJobPayload `json:"bot_jobs"`
	NextCursor string          `json:"next_cursor"`
}

type entitlementPayload struct {
	Error struct {
		Code     string `json:"code"`
		Message  string `json:"message"`
		TopUpURL string `json:"top_up_url"`
	} `json:"error"`
}

func (h *billingHarness) createBotJob(session session, body map[string]any) response {
	h.t.Helper()
	return h.do(http.MethodPost, botJobsURL, body,
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
}

func TestBotJobCreateIsRefusedBelowTheEstimateAndAllowedAtTheBoundary(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("bot-credits@example.com")
	workspaceID := uuid.MustParse(session.Workspace.ID)
	body := map[string]any{"meeting_url": "https://meet.google.com/abc-defg-hij"}

	broke := h.createBotJob(session, body)
	if broke.Status != http.StatusPaymentRequired {
		t.Fatalf("bot job on a zero balance: status %d, body %s", broke.Status, broke.Body)
	}
	var refused entitlementPayload
	broke.decode(t, &refused)
	if refused.Error.Code != "insufficient_credits" {
		t.Fatalf("error code = %q, want insufficient_credits", refused.Error.Code)
	}
	if refused.Error.TopUpURL != "http://localhost:3000/billing/top-up" {
		t.Fatalf("top_up_url = %q", refused.Error.TopUpURL)
	}

	h.grant(workspaceID, 59)
	justShort := h.createBotJob(session, body)
	if justShort.Status != http.StatusPaymentRequired {
		t.Fatalf("bot job one minute short of the estimate: status %d, body %s", justShort.Status, justShort.Body)
	}

	h.grant(workspaceID, 1)
	exact := h.createBotJob(session, body)
	if exact.Status != http.StatusCreated {
		t.Fatalf("bot job at exactly the estimate: status %d, body %s", exact.Status, exact.Body)
	}
	var created botJobPayload
	exact.decode(t, &created)
	if created.Status != botjobs.StatusScheduled {
		t.Fatalf("status = %q, want scheduled", created.Status)
	}
	if created.Platform != botjobs.PlatformMeet {
		t.Fatalf("platform = %q, want meet", created.Platform)
	}
	if created.EstimatedMinutes != 60 {
		t.Fatalf("estimated minutes = %d, want the default 60", created.EstimatedMinutes)
	}
	if created.WorkspaceID != session.Workspace.ID {
		t.Fatalf("workspace = %q", created.WorkspaceID)
	}

	if got := h.balance(workspaceID); got != 60 {
		t.Fatalf("balance after scheduling = %d, want 60: the estimate is checked, not spent", got)
	}
}

func TestBotJobEstimateDrivesTheCheck(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("bot-estimate@example.com")
	h.grant(uuid.MustParse(session.Workspace.ID), 30)

	body := map[string]any{"meeting_url": "https://zoom.us/j/9876543210", "estimated_minutes": 30}
	allowed := h.createBotJob(session, body)
	if allowed.Status != http.StatusCreated {
		t.Fatalf("bot job within a smaller estimate: status %d, body %s", allowed.Status, allowed.Body)
	}
	var created botJobPayload
	allowed.decode(t, &created)
	if created.Platform != botjobs.PlatformZoom || created.EstimatedMinutes != 30 {
		t.Fatalf("bot job = %+v", created)
	}

	body["estimated_minutes"] = 31
	refused := h.createBotJob(session, body)
	if refused.Status != http.StatusPaymentRequired {
		t.Fatalf("bot job above the balance: status %d, body %s", refused.Status, refused.Body)
	}
}

func TestBotJobCreateValidatesTheMeetingLink(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("bot-links@example.com")
	h.grant(uuid.MustParse(session.Workspace.ID), 600)

	cases := []struct {
		name   string
		body   map[string]any
		status int
	}{
		{"missing url", map[string]any{}, http.StatusBadRequest},
		{"not a url", map[string]any{"meeting_url": "not a url"}, http.StatusBadRequest},
		{"unknown host", map[string]any{"meeting_url": "https://example.com/room/1"}, http.StatusBadRequest},
		{"platform mismatch", map[string]any{"meeting_url": "https://meet.google.com/abc", "platform": "zoom"}, http.StatusBadRequest},
		{"unknown host named as zoom", map[string]any{"meeting_url": "http://169.254.169.254/latest/meta-data/", "platform": "zoom"}, http.StatusBadRequest},
		{"link local host over https", map[string]any{"meeting_url": "https://169.254.169.254/latest/meta-data/", "platform": "zoom"}, http.StatusBadRequest},
		{"plain http meet link", map[string]any{"meeting_url": "http://meet.google.com/abc-defg-hij"}, http.StatusBadRequest},
		{"scheduled in the past", map[string]any{"meeting_url": "https://meet.google.com/abc", "scheduled_at": "2020-01-01T00:00:00Z"}, http.StatusBadRequest},
		{"scheduled beyond the horizon", map[string]any{"meeting_url": "https://meet.google.com/abc", "scheduled_at": "2099-01-01T00:00:00Z"}, http.StatusBadRequest},
		{"unknown platform", map[string]any{"meeting_url": "https://meet.google.com/abc", "platform": "webex"}, http.StatusBadRequest},
		{"zero estimate", map[string]any{"meeting_url": "https://meet.google.com/abc", "estimated_minutes": 0}, http.StatusBadRequest},
		{"absurd estimate", map[string]any{"meeting_url": "https://meet.google.com/abc", "estimated_minutes": 100000}, http.StatusBadRequest},
		{"bad timestamp", map[string]any{"meeting_url": "https://meet.google.com/abc", "scheduled_at": "tomorrow"}, http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := h.createBotJob(session, testCase.body)
			if got.Status != testCase.status {
				t.Fatalf("status %d, body %s", got.Status, got.Body)
			}
		})
	}

	teams := h.createBotJob(session, map[string]any{
		"meeting_url":  "https://teams.microsoft.com/l/meetup-join/xyz",
		"bot_name":     "Afterword Notes",
		"scheduled_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	if teams.Status != http.StatusCreated {
		t.Fatalf("teams bot job: status %d, body %s", teams.Status, teams.Body)
	}
	var created botJobPayload
	teams.decode(t, &created)
	if created.Platform != botjobs.PlatformTeams || created.BotName != "Afterword Notes" {
		t.Fatalf("bot job = %+v", created)
	}
}

func TestBotJobsAreListedAndFetchedWithinTheWorkspace(t *testing.T) {
	h := newBillingHarness(t)
	owner := h.signIn("bot-list@example.com")
	stranger := h.signIn("bot-stranger@example.com")
	h.grant(uuid.MustParse(owner.Workspace.ID), 600)

	var ids []string
	for _, link := range []string{
		"https://meet.google.com/aaa-bbbb-ccc",
		"https://meet.google.com/ddd-eeee-fff",
		"https://meet.google.com/ggg-hhhh-iii",
	} {
		created := h.createBotJob(owner, map[string]any{"meeting_url": link})
		if created.Status != http.StatusCreated {
			t.Fatalf("create %s: status %d, body %s", link, created.Status, created.Body)
		}
		var job botJobPayload
		created.decode(t, &job)
		ids = append(ids, job.ID)
	}

	first := h.do(http.MethodGet, botJobsURL+"?limit=2", nil,
		withBearer(owner.AccessToken), withWorkspace(owner.Workspace.ID))
	if first.Status != http.StatusOK {
		t.Fatalf("list: status %d, body %s", first.Status, first.Body)
	}
	var page botJobListPayload
	first.decode(t, &page)
	if len(page.BotJobs) != 2 || page.NextCursor == "" {
		t.Fatalf("first page = %d rows, cursor %q", len(page.BotJobs), page.NextCursor)
	}

	second := h.do(http.MethodGet, botJobsURL+"?limit=2&cursor="+page.NextCursor, nil,
		withBearer(owner.AccessToken), withWorkspace(owner.Workspace.ID))
	var rest botJobListPayload
	second.decode(t, &rest)
	if len(rest.BotJobs) != 1 || rest.NextCursor != "" {
		t.Fatalf("second page = %d rows, cursor %q", len(rest.BotJobs), rest.NextCursor)
	}

	one := h.do(http.MethodGet, botJobsURL+"/"+ids[0], nil,
		withBearer(owner.AccessToken), withWorkspace(owner.Workspace.ID))
	if one.Status != http.StatusOK {
		t.Fatalf("get: status %d, body %s", one.Status, one.Body)
	}

	missing := h.do(http.MethodGet, botJobsURL+"/"+uuid.NewString(), nil,
		withBearer(owner.AccessToken), withWorkspace(owner.Workspace.ID))
	if missing.Status != http.StatusNotFound {
		t.Fatalf("get an unknown job = %d, want 404", missing.Status)
	}

	crossed := h.do(http.MethodGet, botJobsURL+"/"+ids[0], nil,
		withBearer(stranger.AccessToken), withWorkspace(stranger.Workspace.ID))
	if crossed.Status != http.StatusNotFound {
		t.Fatalf("get across workspaces = %d, want 404", crossed.Status)
	}

	anonymous := h.do(http.MethodGet, botJobsURL, nil)
	if anonymous.Status != http.StatusUnauthorized {
		t.Fatalf("list without a token = %d, want 401", anonymous.Status)
	}
}

func TestSchedulingABotIsAnAdminAct(t *testing.T) {
	h := newBillingHarness(t)
	owner := h.signIn("bot-owner@example.com")
	member := h.joinAs(owner, "bot-member@example.com", auth.RoleMember)
	admin := h.joinAs(owner, "bot-admin@example.com", auth.RoleAdmin)
	h.grant(uuid.MustParse(owner.Workspace.ID), 600)

	body := map[string]any{"meeting_url": "https://meet.google.com/mmm-nnnn-ooo"}

	refused := h.createBotJob(member, body)
	if refused.Status != http.StatusForbidden {
		t.Fatalf("member bot job = %d, want 403", refused.Status)
	}

	allowed := h.createBotJob(admin, body)
	if allowed.Status != http.StatusCreated {
		t.Fatalf("admin bot job: status %d, body %s", allowed.Status, allowed.Body)
	}
	var created botJobPayload
	allowed.decode(t, &created)

	listed := h.do(http.MethodGet, botJobsURL, nil,
		withBearer(member.AccessToken), withWorkspace(member.Workspace.ID))
	if listed.Status != http.StatusOK {
		t.Fatalf("member list = %d, want 200", listed.Status)
	}

	fetched := h.do(http.MethodGet, botJobsURL+"/"+created.ID, nil,
		withBearer(member.AccessToken), withWorkspace(member.Workspace.ID))
	if fetched.Status != http.StatusOK {
		t.Fatalf("member get = %d, want 200", fetched.Status)
	}
}
