//go:build integration

package search

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/google/uuid"
)

func TestSavedSearchPersistenceOwnershipAndInvalidTags(t *testing.T) {
	f := newIntegrationFixture(t)
	h := NewHandler(f.service)
	h.SetSavedRepository(f.db)
	message := f.insertMessage(t, f.alice, messageOptions{body: textPointer("ordinary text")})
	tag := f.addTag(t, f.alice, message, "saved tag")
	c := httpapi.SearchConditions{Q: "ordinary", Time: "7d", Timezone: "Asia/Shanghai", TagIds: []uuid.UUID{tag}}
	call := func(owner uuid.UUID, name string, conditions httpapi.SearchConditions, id uuid.UUID, method string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(httpapi.SavedSearchRequest{Name: name, Conditions: conditions})
		r := authenticatedSearchRequest("/api/v1/saved-searches", owner)
		r.Body = http.NoBody
		if method != "GET" && method != "DELETE" {
			r.Body = io.NopCloser(strings.NewReader(string(body)))
		}
		w := httptest.NewRecorder()
		switch method {
		case "POST":
			h.CreateSavedSearch(w, r)
		case "PUT":
			h.UpdateSavedSearch(w, r, id)
		case "DELETE":
			h.DeleteSavedSearch(w, r, id)
		default:
			h.ListSavedSearches(w, r)
		}
		return w
	}
	created := call(f.alice, " Week ", c, uuid.Nil, "POST")
	if created.Code != 201 || created.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create %d: %s", created.Code, created.Body.String())
	}
	var item httpapi.SavedSearch
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.Name != "Week" || item.Conditions.Time != "7d" || item.Conditions.From != "" {
		t.Fatalf("snapshot persisted: %+v", item)
	}
	// Another handler/device reads PostgreSQL state, not a process-local cache.
	second := NewHandler(f.service)
	second.SetSavedRepository(f.db)
	list := httptest.NewRecorder()
	second.ListSavedSearches(list, authenticatedSearchRequest("/", f.alice))
	if !strings.Contains(list.Body.String(), item.Id.String()) {
		t.Fatal("second device cannot read view")
	}
	for _, method := range []string{"PUT", "DELETE"} {
		denied := call(f.bob, "attack", c, item.Id, method)
		if denied.Code != 404 {
			t.Fatalf("cross-owner %s: %d", method, denied.Code)
		}
	}
	other := call(f.bob, "", c, uuid.Nil, "GET")
	if strings.TrimSpace(other.Body.String()) != "[]" {
		t.Fatalf("admin read another user's view: %s", other.Body.String())
	}
	foreignTag := call(f.bob, "foreign tag", c, uuid.Nil, "POST")
	if foreignTag.Code != 422 {
		t.Fatalf("foreign tag accepted: %d", foreignTag.Code)
	}
	renamed := call(f.alice, "Renamed", c, item.Id, "PUT")
	if renamed.Code != 200 {
		t.Fatal(renamed.Body.String())
	}
	if _, err := f.db.Exec(context.Background(), "DELETE FROM tags WHERE id=$1", tag); err != nil {
		t.Fatal(err)
	}
	invalid := call(f.alice, "", c, uuid.Nil, "GET")
	var items []httpapi.SavedSearch
	_ = json.Unmarshal(invalid.Body.Bytes(), &items)
	if len(items) != 1 || items[0].InvalidReason == "" || len(items[0].Conditions.TagIds) != 1 {
		t.Fatalf("tag deleted silently: %s", invalid.Body.String())
	}
	if w := call(f.alice, "Invalid renamed", c, item.Id, "PUT"); w.Code != 200 {
		t.Fatalf("rename invalid tag view: %s", w.Body.String())
	}
	c.TagIds = []uuid.UUID{}
	c.Time = "custom"
	c.From = "2026-09-01T00:00"
	c.To = "2026-10-01T00:00"
	c.Timezone = "UTC"
	if w := call(f.alice, "Repaired", c, item.Id, "PUT"); w.Code != 200 {
		t.Fatalf("repair: %s", w.Body.String())
	}
	c.To = c.From
	if w := call(f.alice, "Bad date", c, item.Id, "PUT"); w.Code != 422 {
		t.Fatalf("invalid range accepted: %d", w.Code)
	}
	if w := call(f.alice, "", c, item.Id, "DELETE"); w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	if w := call(f.alice, "", c, item.Id, "DELETE"); w.Code != 404 {
		t.Fatalf("second delete %d", w.Code)
	}
}

func TestSearchSavedViewTypeAndDateFilters(t *testing.T) {
	f := newIntegrationFixture(t)
	plain := f.insertMessage(t, f.alice, messageOptions{body: textPointer("matching plain"), createdAt: f.now})
	codeType := "CODE"
	code := f.insertMessage(t, f.alice, messageOptions{body: textPointer("matching code"), detectedType: &codeType, createdAt: f.now})
	markdown := f.insertMessage(t, f.alice, messageOptions{body: textPointer("matching markdown"), createdAt: f.now})
	if _, err := f.db.Exec(context.Background(), "UPDATE messages SET body_format='MARKDOWN' WHERE id=$1", markdown); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		kind string
		id   uuid.UUID
	}{{"TEXT", plain}, {"CODE", code}, {"MARKDOWN", markdown}} {
		page := f.search(t, f.alice, "matching", func(q *Query) { q.DetectedType = &sample.kind })
		if len(page.Items) != 1 || !containsMessage(page, sample.id) {
			t.Fatalf("type %s returned %+v", sample.kind, page.Items)
		}
	}
	end := f.now.Add(time.Hour)
	f.insertMessage(t, f.alice, messageOptions{body: textPointer("matching end"), createdAt: end})
	f.insertMessage(t, f.alice, messageOptions{body: textPointer("matching before"), createdAt: f.now.Add(-time.Minute)})
	page := f.search(t, f.alice, "matching", func(q *Query) { q.CreatedAfter = &f.now; q.CreatedBefore = &end })
	if len(page.Items) != 3 {
		t.Fatalf("half-open interval returned %d messages", len(page.Items))
	}
}
