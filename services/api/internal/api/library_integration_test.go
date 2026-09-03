//go:build integration

package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

func TestCreateMeetingReturnsUploadTargets(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{
		"title":      "Team standup",
		"source":     "desktop",
		"platform":   "meet",
		"started_at": time.Now().UTC().Format(time.RFC3339),
		"duration_s": 900,
	})

	if created.Meeting.Title != "Team standup" || created.Meeting.Source != "desktop" || created.Meeting.Platform != "meet" {
		t.Fatalf("unexpected meeting: %+v", created.Meeting)
	}
	if created.Meeting.Status != meetings.StatusPending || created.Meeting.Visibility != meetings.VisibilityPrivate {
		t.Fatalf("unexpected initial state: %+v", created.Meeting)
	}
	if created.Upload.AudioURL == "" || created.Upload.TranscriptURL == "" {
		t.Fatalf("missing upload urls: %+v", created.Upload)
	}
	if !created.Upload.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("upload expiry %v is not in the future", created.Upload.ExpiresAt)
	}

	presigns := harness.memory.Presigns()
	if len(presigns) != 2 {
		t.Fatalf("expected two presigned uploads, got %+v", presigns)
	}
	audioKey := fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, created.Meeting.ID)
	transcriptKey := fmt.Sprintf("ws/%s/meetings/%s/transcript.json", owner.Workspace.ID, created.Meeting.ID)
	if presigns[0].Key != audioKey || presigns[0].Bucket != "audio" {
		t.Fatalf("unexpected audio target: %+v", presigns[0])
	}
	if presigns[1].Key != transcriptKey || presigns[1].Bucket != "transcripts" {
		t.Fatalf("unexpected transcript target: %+v", presigns[1])
	}
}

func TestCreateMeetingReportsEveryFieldError(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	result := harness.call(http.MethodPost, "/v1/meetings", map[string]any{
		"title":      "",
		"source":     "telepathy",
		"platform":   "carrier-pigeon",
		"duration_s": -5,
		"visibility": "everyone",
	}, harness.as(owner)...)
	if result.Status != http.StatusBadRequest {
		t.Fatalf("status %d, body %s", result.Status, result.Body)
	}

	var envelope validationEnvelope
	result.decode(t, &envelope)
	if envelope.Error.Code != "validation_failed" {
		t.Fatalf("code %q", envelope.Error.Code)
	}
	fields := map[string]string{}
	for _, field := range envelope.Error.Fields {
		fields[field.Field] = field.Message
	}
	for _, expected := range []string{"title", "source", "platform", "duration_s", "visibility"} {
		if _, ok := fields[expected]; !ok {
			t.Fatalf("field %q missing from %+v", expected, envelope.Error.Fields)
		}
	}
}

func TestMeetingRoutesRequireAuthenticationAndAWorkspace(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	anonymous := harness.call(http.MethodGet, "/v1/meetings", nil)
	if anonymous.Status != http.StatusUnauthorized {
		t.Fatalf("anonymous list: status %d, body %s", anonymous.Status, anonymous.Body)
	}

	noWorkspace := harness.call(http.MethodGet, "/v1/meetings", nil, withBearer(owner.AccessToken))
	if noWorkspace.Status != http.StatusBadRequest {
		t.Fatalf("missing workspace: status %d, body %s", noWorkspace.Status, noWorkspace.Body)
	}
}

func TestVisibilityMatrix(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")
	member := harness.join(t, owner, "member@example.com", auth.RoleMember)
	stranger := harness.signIn("stranger@example.com")

	cases := []struct {
		visibility string
		owner      int
		member     int
		stranger   int
		anonymous  int
	}{
		{meetings.VisibilityPrivate, http.StatusOK, http.StatusNotFound, http.StatusForbidden, http.StatusUnauthorized},
		{meetings.VisibilityWorkspace, http.StatusOK, http.StatusOK, http.StatusForbidden, http.StatusUnauthorized},
		{meetings.VisibilityLink, http.StatusOK, http.StatusOK, http.StatusForbidden, http.StatusUnauthorized},
	}

	for _, testCase := range cases {
		t.Run(testCase.visibility, func(t *testing.T) {
			created := harness.createMeeting(owner, map[string]any{
				"title":      "Visibility " + testCase.visibility,
				"source":     "desktop",
				"visibility": testCase.visibility,
			})
			path := "/v1/meetings/" + created.Meeting.ID

			if got := harness.call(http.MethodGet, path, nil, harness.as(owner)...); got.Status != testCase.owner {
				t.Fatalf("owner: status %d, body %s", got.Status, got.Body)
			}
			if got := harness.call(http.MethodGet, path, nil, harness.as(member)...); got.Status != testCase.member {
				t.Fatalf("member: status %d, body %s", got.Status, got.Body)
			}
			strangerRequest := []func(*http.Request){withBearer(stranger.AccessToken), withWorkspace(owner.Workspace.ID)}
			if got := harness.call(http.MethodGet, path, nil, strangerRequest...); got.Status != testCase.stranger {
				t.Fatalf("stranger: status %d, body %s", got.Status, got.Body)
			}
			if got := harness.call(http.MethodGet, path, nil); got.Status != testCase.anonymous {
				t.Fatalf("anonymous: status %d, body %s", got.Status, got.Body)
			}
		})
	}
}

func TestVisibilityMatrixForListing(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")
	member := harness.join(t, owner, "member@example.com", auth.RoleMember)

	private := harness.createMeeting(owner, map[string]any{"title": "Private", "source": "desktop", "visibility": "private"})
	shared := harness.createMeeting(owner, map[string]any{"title": "Shared", "source": "desktop", "visibility": "workspace"})

	ownerList := harness.call(http.MethodGet, "/v1/meetings", nil, harness.as(owner)...)
	var ownerPage meetingListPayload
	ownerList.decode(t, &ownerPage)
	if len(ownerPage.Meetings) != 2 {
		t.Fatalf("owner sees %d meetings", len(ownerPage.Meetings))
	}

	memberList := harness.call(http.MethodGet, "/v1/meetings", nil, harness.as(member)...)
	var memberPage meetingListPayload
	memberList.decode(t, &memberPage)
	if len(memberPage.Meetings) != 1 || memberPage.Meetings[0].ID != shared.Meeting.ID {
		t.Fatalf("member sees %+v, expected only %s", memberPage.Meetings, shared.Meeting.ID)
	}
	if memberPage.Meetings[0].ID == private.Meeting.ID {
		t.Fatal("a private meeting leaked into the member listing")
	}
}

func TestOnlyAnOwnerOrAdminCanChangeAMeeting(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")
	member := harness.join(t, owner, "member@example.com", auth.RoleMember)
	admin := harness.join(t, owner, "admin@example.com", auth.RoleAdmin)

	created := harness.createMeeting(owner, map[string]any{"title": "Shared", "source": "desktop", "visibility": "workspace"})
	path := "/v1/meetings/" + created.Meeting.ID

	refused := harness.call(http.MethodPatch, path, map[string]any{"title": "Hijacked"}, harness.as(member)...)
	if refused.Status != http.StatusForbidden {
		t.Fatalf("member patch: status %d, body %s", refused.Status, refused.Body)
	}
	if refused := harness.call(http.MethodDelete, path, nil, harness.as(member)...); refused.Status != http.StatusForbidden {
		t.Fatalf("member delete: status %d, body %s", refused.Status, refused.Body)
	}

	allowed := harness.call(http.MethodPatch, path, map[string]any{"title": "Renamed by an admin"}, harness.as(admin)...)
	if allowed.Status != http.StatusOK {
		t.Fatalf("admin patch: status %d, body %s", allowed.Status, allowed.Body)
	}
}

func TestAnAdminCannotReachAPrivateMeeting(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")
	admin := harness.join(t, owner, "admin@example.com", auth.RoleAdmin)

	created := harness.createMeeting(owner, map[string]any{"title": "Private", "source": "desktop"})
	path := "/v1/meetings/" + created.Meeting.ID

	if got := harness.call(http.MethodGet, path, nil, harness.as(admin)...); got.Status != http.StatusNotFound {
		t.Fatalf("admin get: status %d, body %s", got.Status, got.Body)
	}
	if got := harness.call(http.MethodDelete, path, nil, harness.as(admin)...); got.Status != http.StatusNotFound {
		t.Fatalf("admin delete: status %d, body %s", got.Status, got.Body)
	}
}

func TestFinalizeChecksTheObjectsAndQueuesTheRightWork(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "Nothing uploaded", "source": "desktop"})
	empty := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/finalize", nil, harness.as(owner)...)
	if empty.Status != http.StatusConflict {
		t.Fatalf("finalize with no objects: status %d, body %s", empty.Status, empty.Body)
	}

	audioOnly := harness.createMeeting(owner, map[string]any{"title": "Audio only", "source": "desktop"})
	harness.memory.Put("audio", fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, audioOnly.Meeting.ID), make([]byte, 2048), "audio/opus")

	finalized := harness.call(http.MethodPost, "/v1/meetings/"+audioOnly.Meeting.ID+"/finalize", nil, harness.as(owner)...)
	if finalized.Status != http.StatusOK {
		t.Fatalf("finalize audio: status %d, body %s", finalized.Status, finalized.Body)
	}
	var result struct {
		Meeting meetingPayload `json:"meeting"`
		Queued  []string       `json:"queued"`
	}
	finalized.decode(t, &result)
	if result.Meeting.Status != meetings.StatusReady {
		t.Fatalf("status %q", result.Meeting.Status)
	}
	if result.Meeting.AudioBytes == nil || *result.Meeting.AudioBytes != 2048 {
		t.Fatalf("audio bytes %+v", result.Meeting.AudioBytes)
	}
	if result.Meeting.Transcript != nil {
		t.Fatalf("transcript bytes %+v were recorded without an object", result.Meeting.Transcript)
	}
	if len(result.Queued) != 1 || result.Queued[0] != meetings.KindTranscribe {
		t.Fatalf("queued %+v", result.Queued)
	}
	if !harness.jobExists(t, meetings.KindTranscribe, "transcribe:meeting:"+audioOnly.Meeting.ID) {
		t.Fatal("no transcribe job was enqueued")
	}
	if harness.jobExists(t, meetings.KindSummarise, "summarise:meeting:"+audioOnly.Meeting.ID) {
		t.Fatal("a summarise job was enqueued without a transcript")
	}

	both := harness.createMeeting(owner, map[string]any{"title": "Audio and transcript", "source": "desktop"})
	harness.memory.Put("audio", fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, both.Meeting.ID), make([]byte, 4096), "audio/opus")
	harness.memory.Put("transcripts", fmt.Sprintf("ws/%s/meetings/%s/transcript.json", owner.Workspace.ID, both.Meeting.ID), []byte(`{"segments":[]}`), "application/json")

	done := harness.call(http.MethodPost, "/v1/meetings/"+both.Meeting.ID+"/finalize", nil, harness.as(owner)...)
	if done.Status != http.StatusOK {
		t.Fatalf("finalize both: status %d, body %s", done.Status, done.Body)
	}
	done.decode(t, &result)
	if len(result.Queued) != 1 || result.Queued[0] != meetings.KindSummarise {
		t.Fatalf("queued %+v", result.Queued)
	}
	if result.Meeting.Transcript == nil || *result.Meeting.Transcript != 15 {
		t.Fatalf("transcript bytes %+v", result.Meeting.Transcript)
	}
}

func TestFinalizeIsIdempotent(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "Repeat", "source": "desktop"})
	harness.memory.Put("audio", fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, created.Meeting.ID), make([]byte, 64), "audio/opus")

	for attempt := range 3 {
		result := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/finalize", nil, harness.as(owner)...)
		if result.Status != http.StatusOK {
			t.Fatalf("attempt %d: status %d, body %s", attempt, result.Status, result.Body)
		}
	}
}

func TestDeleteRemovesTheRowsAndQueuesAPurge(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "Delete me", "source": "desktop"})
	audioKey := fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, created.Meeting.ID)
	transcriptKey := fmt.Sprintf("ws/%s/meetings/%s/transcript.json", owner.Workspace.ID, created.Meeting.ID)
	harness.memory.Put("audio", audioKey, make([]byte, 32), "audio/opus")
	harness.memory.Put("transcripts", transcriptKey, []byte("{}"), "application/json")

	stored := harness.call(http.MethodPut, "/v1/meetings/"+created.Meeting.ID+"/segments", map[string]any{
		"segments": []map[string]any{{"seq": 0, "speaker": "Ada", "start_s": 0, "end_s": 1.5, "text": "Hello"}},
	}, harness.as(owner)...)
	if stored.Status != http.StatusOK {
		t.Fatalf("store segments: status %d, body %s", stored.Status, stored.Body)
	}

	removed := harness.call(http.MethodDelete, "/v1/meetings/"+created.Meeting.ID, nil, harness.as(owner)...)
	if removed.Status != http.StatusNoContent {
		t.Fatalf("delete: status %d, body %s", removed.Status, removed.Body)
	}

	if got := harness.call(http.MethodGet, "/v1/meetings/"+created.Meeting.ID, nil, harness.as(owner)...); got.Status != http.StatusNotFound {
		t.Fatalf("get after delete: status %d", got.Status)
	}
	if !harness.jobExists(t, meetings.KindPurge, "purge:meeting:"+created.Meeting.ID) {
		t.Fatal("no purge job was enqueued")
	}
	if !harness.memory.Exists("audio", audioKey) {
		t.Fatal("the delete request purged the audio object inline instead of queueing it")
	}

	job, err := harness.queue.GetByIdempotencyKey(t.Context(), meetings.KindPurge, "purge:meeting:"+created.Meeting.ID)
	if err != nil {
		t.Fatalf("read purge job: %v", err)
	}
	handler := meetings.NewPurgeHandler(harness.store)
	if err := handler(t.Context(), job); err != nil {
		t.Fatalf("run purge handler: %v", err)
	}
	if harness.memory.Exists("audio", audioKey) || harness.memory.Exists("transcripts", transcriptKey) {
		t.Fatalf("purge left objects behind: %v", harness.memory.Keys())
	}
	if err := handler(t.Context(), job); err != nil {
		t.Fatalf("purge is not idempotent: %v", err)
	}
}

func TestSegmentsAreReplacedTransactionallyAndCapped(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")
	created := harness.createMeeting(owner, map[string]any{"title": "Transcript", "source": "desktop"})
	path := "/v1/meetings/" + created.Meeting.ID + "/segments"

	first := []map[string]any{
		{"seq": 0, "speaker": "Ada", "start_s": 0, "end_s": 1, "text": "one"},
		{"seq": 1, "speaker": "Grace", "start_s": 1, "end_s": 2, "text": "two"},
		{"seq": 2, "speaker": "Ada", "start_s": 2, "end_s": 3, "text": "three"},
	}
	if result := harness.call(http.MethodPut, path, map[string]any{"segments": first}, harness.as(owner)...); result.Status != http.StatusOK {
		t.Fatalf("first write: status %d, body %s", result.Status, result.Body)
	}

	second := []map[string]any{{"seq": 0, "speaker": "Ada", "start_s": 0, "end_s": 9, "text": "replaced"}}
	if result := harness.call(http.MethodPut, path, map[string]any{"segments": second}, harness.as(owner)...); result.Status != http.StatusOK {
		t.Fatalf("second write: status %d, body %s", result.Status, result.Body)
	}

	listed := harness.call(http.MethodGet, path, nil, harness.as(owner)...)
	var page segmentListPayload
	listed.decode(t, &page)
	if page.Total != 1 || len(page.Segments) != 1 || page.Segments[0].Text != "replaced" {
		t.Fatalf("unexpected segments: %+v", page)
	}

	invalid := []map[string]any{
		{"seq": 0, "start_s": 0, "end_s": 1, "text": "kept?"},
		{"seq": 1, "start_s": 5, "end_s": 1, "text": "backwards"},
	}
	rejected := harness.call(http.MethodPut, path, map[string]any{"segments": invalid}, harness.as(owner)...)
	if rejected.Status != http.StatusBadRequest {
		t.Fatalf("invalid write: status %d, body %s", rejected.Status, rejected.Body)
	}

	listed = harness.call(http.MethodGet, path, nil, harness.as(owner)...)
	listed.decode(t, &page)
	if page.Total != 1 || page.Segments[0].Text != "replaced" {
		t.Fatalf("a rejected write changed the stored transcript: %+v", page)
	}

	oversized := make([]map[string]any, meetings.MaxSegments+1)
	for index := range oversized {
		oversized[index] = map[string]any{"seq": index, "start_s": 0, "end_s": 1, "text": "x"}
	}
	tooMany := harness.call(http.MethodPut, path, map[string]any{"segments": oversized}, harness.as(owner)...)
	if tooMany.Status != http.StatusRequestEntityTooLarge && tooMany.Status != http.StatusBadRequest {
		t.Fatalf("oversized write: status %d, body %s", tooMany.Status, tooMany.Body)
	}
	listed = harness.call(http.MethodGet, path, nil, harness.as(owner)...)
	listed.decode(t, &page)
	if page.Total != 1 {
		t.Fatalf("an oversized write changed the stored transcript: %+v", page)
	}
}

func TestSegmentsPaginateBySequence(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")
	created := harness.createMeeting(owner, map[string]any{"title": "Long", "source": "desktop"})
	path := "/v1/meetings/" + created.Meeting.ID + "/segments"

	segments := make([]map[string]any, 7)
	for index := range segments {
		segments[index] = map[string]any{"seq": index, "start_s": index, "end_s": index + 1, "text": fmt.Sprintf("line %d", index)}
	}
	if result := harness.call(http.MethodPut, path, map[string]any{"segments": segments}, harness.as(owner)...); result.Status != http.StatusOK {
		t.Fatalf("write: status %d, body %s", result.Status, result.Body)
	}

	seen := make([]int32, 0, 7)
	url := path + "?page_size=3"
	for pages := 0; pages < 5; pages++ {
		result := harness.call(http.MethodGet, url, nil, harness.as(owner)...)
		if result.Status != http.StatusOK {
			t.Fatalf("page: status %d, body %s", result.Status, result.Body)
		}
		var page segmentListPayload
		result.decode(t, &page)
		for _, segment := range page.Segments {
			seen = append(seen, segment.Seq)
		}
		if page.NextSeq == nil {
			break
		}
		url = fmt.Sprintf("%s?page_size=3&after_seq=%d", path, *page.NextSeq)
	}
	if len(seen) != 7 {
		t.Fatalf("paged through %v", seen)
	}
	for index, seq := range seen {
		if int(seq) != index {
			t.Fatalf("out of order: %v", seen)
		}
	}
}

func TestMeetingListFiltersAndPages(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	folder := harness.call(http.MethodPost, "/v1/folders", map[string]any{"name": "Sales"}, harness.as(owner)...)
	if folder.Status != http.StatusCreated {
		t.Fatalf("create folder: status %d, body %s", folder.Status, folder.Body)
	}
	var created folderPayload
	folder.decode(t, &created)

	harness.createMeeting(owner, map[string]any{"title": "Weekly sales sync", "source": "desktop", "folder_id": created.ID})
	harness.createMeeting(owner, map[string]any{"title": "Bot recorded call", "source": "bot"})
	harness.createMeeting(owner, map[string]any{"title": "Imported archive", "source": "import"})

	byFolder := harness.call(http.MethodGet, "/v1/meetings?folder="+created.ID, nil, harness.as(owner)...)
	var page meetingListPayload
	byFolder.decode(t, &page)
	if len(page.Meetings) != 1 || page.Meetings[0].Title != "Weekly sales sync" {
		t.Fatalf("folder filter: %+v", page.Meetings)
	}

	bySource := harness.call(http.MethodGet, "/v1/meetings?source=bot", nil, harness.as(owner)...)
	bySource.decode(t, &page)
	if len(page.Meetings) != 1 || page.Meetings[0].Source != "bot" {
		t.Fatalf("source filter: %+v", page.Meetings)
	}

	byQuery := harness.call(http.MethodGet, "/v1/meetings?q=sales", nil, harness.as(owner)...)
	byQuery.decode(t, &page)
	if len(page.Meetings) != 1 || page.Meetings[0].Title != "Weekly sales sync" {
		t.Fatalf("query filter: %+v", page.Meetings)
	}

	wildcard := harness.call(http.MethodGet, "/v1/meetings?q=%25", nil, harness.as(owner)...)
	wildcard.decode(t, &page)
	if len(page.Meetings) != 0 {
		t.Fatalf("a literal wildcard matched %+v", page.Meetings)
	}

	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	byWindow := harness.call(http.MethodGet, "/v1/meetings?from="+future, nil, harness.as(owner)...)
	byWindow.decode(t, &page)
	if len(page.Meetings) != 0 {
		t.Fatalf("from filter returned %+v", page.Meetings)
	}

	first := harness.call(http.MethodGet, "/v1/meetings?page_size=2", nil, harness.as(owner)...)
	first.decode(t, &page)
	if len(page.Meetings) != 2 || page.NextCursor == "" {
		t.Fatalf("first page: %+v", page)
	}
	seen := map[string]bool{}
	for _, meeting := range page.Meetings {
		seen[meeting.ID] = true
	}

	next := harness.call(http.MethodGet, "/v1/meetings?page_size=2&cursor="+page.NextCursor, nil, harness.as(owner)...)
	next.decode(t, &page)
	if len(page.Meetings) != 1 || page.NextCursor != "" {
		t.Fatalf("second page: %+v", page)
	}
	if seen[page.Meetings[0].ID] {
		t.Fatal("the second page repeated a meeting")
	}

	bad := harness.call(http.MethodGet, "/v1/meetings?cursor=not-a-cursor", nil, harness.as(owner)...)
	if bad.Status != http.StatusBadRequest {
		t.Fatalf("bad cursor: status %d, body %s", bad.Status, bad.Body)
	}
}

func TestFoldersAreScopedToTheWorkspace(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")
	stranger := harness.signIn("stranger@example.com")

	created := harness.call(http.MethodPost, "/v1/folders", map[string]any{"name": "Customers"}, harness.as(owner)...)
	if created.Status != http.StatusCreated {
		t.Fatalf("create: status %d, body %s", created.Status, created.Body)
	}
	var parent folderPayload
	created.decode(t, &parent)

	child := harness.call(http.MethodPost, "/v1/folders", map[string]any{"name": "Renewals", "parent_id": parent.ID}, harness.as(owner)...)
	if child.Status != http.StatusCreated {
		t.Fatalf("create child: status %d, body %s", child.Status, child.Body)
	}
	var nested folderPayload
	child.decode(t, &nested)

	listed := harness.call(http.MethodGet, "/v1/folders", nil, harness.as(owner)...)
	var folders folderListPayload
	listed.decode(t, &folders)
	if len(folders.Folders) != 2 {
		t.Fatalf("folders %+v", folders.Folders)
	}

	other := harness.call(http.MethodGet, "/v1/folders", nil, withBearer(stranger.AccessToken), withWorkspace(stranger.Workspace.ID))
	other.decode(t, &folders)
	if len(folders.Folders) != 0 {
		t.Fatalf("another workspace saw %+v", folders.Folders)
	}

	crossWorkspace := harness.call(http.MethodPatch, "/v1/folders/"+parent.ID, map[string]any{"name": "Stolen"},
		withBearer(stranger.AccessToken), withWorkspace(stranger.Workspace.ID))
	if crossWorkspace.Status != http.StatusNotFound {
		t.Fatalf("cross workspace patch: status %d, body %s", crossWorkspace.Status, crossWorkspace.Body)
	}

	blocked := harness.call(http.MethodDelete, "/v1/folders/"+parent.ID, nil, harness.as(owner)...)
	if blocked.Status != http.StatusConflict {
		t.Fatalf("delete with children: status %d, body %s", blocked.Status, blocked.Body)
	}

	cycle := harness.call(http.MethodPatch, "/v1/folders/"+parent.ID, map[string]any{"parent_id": nested.ID}, harness.as(owner)...)
	if cycle.Status != http.StatusConflict {
		t.Fatalf("cycle: status %d, body %s", cycle.Status, cycle.Body)
	}

	if got := harness.call(http.MethodDelete, "/v1/folders/"+nested.ID, nil, harness.as(owner)...); got.Status != http.StatusNoContent {
		t.Fatalf("delete child: status %d, body %s", got.Status, got.Body)
	}
	if got := harness.call(http.MethodDelete, "/v1/folders/"+parent.ID, nil, harness.as(owner)...); got.Status != http.StatusNoContent {
		t.Fatalf("delete parent: status %d, body %s", got.Status, got.Body)
	}
}

func TestSharedLinkGivesReadOnlyAccessWithoutAuth(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "Board update", "source": "desktop"})
	harness.memory.Put("transcripts", fmt.Sprintf("ws/%s/meetings/%s/transcript.json", owner.Workspace.ID, created.Meeting.ID), []byte("{}"), "application/json")
	if got := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/finalize", nil, harness.as(owner)...); got.Status != http.StatusOK {
		t.Fatalf("finalize: status %d, body %s", got.Status, got.Body)
	}
	if got := harness.call(http.MethodPut, "/v1/meetings/"+created.Meeting.ID+"/segments", map[string]any{
		"segments": []map[string]any{{"seq": 0, "start_s": 0, "end_s": 2, "text": "Welcome"}},
	}, harness.as(owner)...); got.Status != http.StatusOK {
		t.Fatalf("segments: status %d, body %s", got.Status, got.Body)
	}

	shared := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/share", map[string]any{"permission": "view"}, harness.as(owner)...)
	if shared.Status != http.StatusCreated {
		t.Fatalf("share: status %d, body %s", shared.Status, shared.Body)
	}
	var link sharePayload
	shared.decode(t, &link)
	if link.Token == "" || link.Permission != "view" || link.URL == "" {
		t.Fatalf("share link %+v", link)
	}

	anonymous := harness.call(http.MethodGet, "/v1/shared/"+link.Token, nil)
	if anonymous.Status != http.StatusOK {
		t.Fatalf("shared read: status %d, body %s", anonymous.Status, anonymous.Body)
	}
	var view sharedMeetingPayload
	anonymous.decode(t, &view)
	if view.Meeting.Title != "Board update" || view.Permission != "view" {
		t.Fatalf("shared view %+v", view)
	}
	if view.Meeting.OwnerUserID != "" || view.Meeting.WorkspaceID != "" {
		t.Fatalf("the shared view leaked workspace internals: %+v", view.Meeting)
	}
	if view.Download.Transcript == nil {
		t.Fatal("the shared view carries no transcript download url")
	}

	segments := harness.call(http.MethodGet, "/v1/shared/"+link.Token+"/segments", nil)
	if segments.Status != http.StatusOK {
		t.Fatalf("shared segments: status %d, body %s", segments.Status, segments.Body)
	}
	var page segmentListPayload
	segments.decode(t, &page)
	if page.Total != 1 || page.Segments[0].Text != "Welcome" {
		t.Fatalf("shared segments %+v", page)
	}

	for _, method := range []string{http.MethodPatch, http.MethodDelete, http.MethodPost} {
		if got := harness.call(method, "/v1/shared/"+link.Token, map[string]any{"title": "x"}); got.Status != http.StatusMethodNotAllowed {
			t.Fatalf("%s on a shared link: status %d, body %s", method, got.Status, got.Body)
		}
	}

	if got := harness.call(http.MethodGet, "/v1/shared/definitely-not-a-token", nil); got.Status != http.StatusNotFound {
		t.Fatalf("unknown token: status %d, body %s", got.Status, got.Body)
	}
}

func TestSharedLinkStopsWorkingOnceRevokedOrExpired(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "Temporary", "source": "desktop"})
	expired := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/share", map[string]any{
		"expires_at": time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
	}, harness.as(owner)...)
	if expired.Status != http.StatusCreated {
		t.Fatalf("share: status %d, body %s", expired.Status, expired.Body)
	}
	var stale sharePayload
	expired.decode(t, &stale)
	if got := harness.call(http.MethodGet, "/v1/shared/"+stale.Token, nil); got.Status != http.StatusGone {
		t.Fatalf("expired token: status %d, body %s", got.Status, got.Body)
	}

	live := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/share", nil, harness.as(owner)...)
	var link sharePayload
	live.decode(t, &link)
	if got := harness.call(http.MethodGet, "/v1/shared/"+link.Token, nil); got.Status != http.StatusOK {
		t.Fatalf("live token: status %d, body %s", got.Status, got.Body)
	}

	if got := harness.call(http.MethodDelete, "/v1/meetings/"+created.Meeting.ID+"/share", nil, harness.as(owner)...); got.Status != http.StatusNoContent {
		t.Fatalf("revoke: status %d, body %s", got.Status, got.Body)
	}
	if got := harness.call(http.MethodGet, "/v1/shared/"+link.Token, nil); got.Status != http.StatusNotFound {
		t.Fatalf("revoked token: status %d, body %s", got.Status, got.Body)
	}
}

func TestTheApiNeverProxiesMedia(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "Audio", "source": "desktop"})
	audioKey := fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, created.Meeting.ID)
	harness.memory.Put("audio", audioKey, make([]byte, 1024), "audio/opus")
	if got := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/finalize", nil, harness.as(owner)...); got.Status != http.StatusOK {
		t.Fatalf("finalize: status %d, body %s", got.Status, got.Body)
	}

	detail := harness.call(http.MethodGet, "/v1/meetings/"+created.Meeting.ID, nil, harness.as(owner)...)
	var view meetingDetailPayload
	detail.decode(t, &view)
	if view.Download.Audio == nil || view.Download.Audio.Method != "GET" {
		t.Fatalf("download %+v", view.Download.Audio)
	}
	if len(detail.Body) > 8192 {
		t.Fatalf("the detail response is %d bytes, which looks like proxied media", len(detail.Body))
	}
}

func TestPresignFailuresDoNotLeakInternals(t *testing.T) {
	pool := dbtest.New(t)
	failing := storage.NewFailing(nil)
	harness := buildLibraryHarness(t, pool, failing, storage.NewMemory())
	owner := harness.signIn("owner@example.com")

	result := harness.call(http.MethodPost, "/v1/meetings", map[string]any{"title": "Broken", "source": "desktop"}, harness.as(owner)...)
	if result.Status != http.StatusInternalServerError {
		t.Fatalf("status %d, body %s", result.Status, result.Body)
	}
	if code := result.errorCode(t); code != "internal_error" {
		t.Fatalf("code %q", code)
	}
	if len(result.Body) > 200 {
		t.Fatalf("the error body leaked detail: %s", result.Body)
	}
}
