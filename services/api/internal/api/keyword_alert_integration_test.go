//go:build integration

package api_test

import (
	"net/http"
	"testing"

	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

func TestKeywordAlertsLifecycle(t *testing.T) {
	memory := storage.NewMemory()
	pool := dbtest.New(t)
	harness := buildLibraryHarness(t, pool, memory, memory)
	owner := harness.signIn("owner@example.com")

	created := harness.call(http.MethodPost, "/v1/keyword-alerts", map[string]any{
		"phrase":  "budget overrun",
		"channel": "slack",
	}, harness.as(owner)...)
	if created.Status != http.StatusCreated {
		t.Fatalf("create alert: status %d, body %s", created.Status, created.Body)
	}
	var alert struct {
		ID      string `json:"id"`
		Phrase  string `json:"phrase"`
		Channel string `json:"channel"`
	}
	created.decode(t, &alert)
	if alert.Phrase != "budget overrun" || alert.Channel != "slack" {
		t.Fatalf("unexpected alert: %+v", alert)
	}

	badChannel := harness.call(http.MethodPost, "/v1/keyword-alerts", map[string]any{
		"phrase": "x", "channel": "carrier-pigeon",
	}, harness.as(owner)...)
	if badChannel.Status != http.StatusBadRequest {
		t.Fatalf("invalid channel should be rejected: status %d", badChannel.Status)
	}

	listed := harness.call(http.MethodGet, "/v1/keyword-alerts", nil, harness.as(owner)...)
	var list struct {
		Alerts []struct {
			ID string `json:"id"`
		} `json:"alerts"`
	}
	listed.decode(t, &list)
	if len(list.Alerts) != 1 || list.Alerts[0].ID != alert.ID {
		t.Fatalf("expected one alert, got %+v", list.Alerts)
	}

	updated := harness.call(http.MethodPatch, "/v1/keyword-alerts/"+alert.ID, map[string]any{
		"channel": "email",
	}, harness.as(owner)...)
	if updated.Status != http.StatusOK {
		t.Fatalf("update alert: status %d, body %s", updated.Status, updated.Body)
	}
	var afterUpdate struct {
		Channel string `json:"channel"`
	}
	updated.decode(t, &afterUpdate)
	if afterUpdate.Channel != "email" {
		t.Fatalf("channel not updated: %+v", afterUpdate)
	}

	removed := harness.call(http.MethodDelete, "/v1/keyword-alerts/"+alert.ID, nil, harness.as(owner)...)
	if removed.Status != http.StatusNoContent {
		t.Fatalf("delete alert: status %d, body %s", removed.Status, removed.Body)
	}

	other := harness.signIn("outsider@example.com")
	leak := harness.call(http.MethodGet, "/v1/keyword-alerts", nil, harness.as(other)...)
	var otherList struct {
		Alerts []struct{} `json:"alerts"`
	}
	leak.decode(t, &otherList)
	if len(otherList.Alerts) != 0 {
		t.Fatalf("another workspace must not see these alerts, got %d", len(otherList.Alerts))
	}
}
