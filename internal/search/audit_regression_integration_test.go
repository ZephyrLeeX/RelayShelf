//go:build integration

package search

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/google/uuid"
)

func TestSearchEditorContentClassificationAndPagination(t *testing.T) {
	f := newIntegrationFixture(t)
	raw, err := os.ReadFile("testdata/editor_content.json")
	if err != nil {
		t.Fatal(err)
	}
	var samples []struct{ Name, Body, Kind string }
	if err := json.Unmarshal(raw, &samples); err != nil {
		t.Fatal(err)
	}
	want := map[string]map[uuid.UUID]bool{"CODE": {}, "MARKDOWN": {}, "TEXT": {}}
	insertMarkdown := func(owner uuid.UUID, options messageOptions) uuid.UUID {
		t.Helper()
		id := f.insertMessage(t, owner, options)
		// Stale hints must not override the Markdown body's actual format.
		if _, err := f.db.Exec(context.Background(), "UPDATE messages SET body_format='MARKDOWN',detected_type='CODE',detected_language='bash' WHERE id=$1", id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	for _, sample := range samples {
		id := insertMarkdown(f.alice, messageOptions{body: &sample.Body})
		// New editor data has no detection fields at all.
		if _, err := f.db.Exec(context.Background(), "UPDATE messages SET detected_type=NULL,detected_language=NULL WHERE id=$1", id); err != nil {
			t.Fatal(err)
		}
		want[sample.Kind][id] = true
	}
	// Classification must inspect the full body, beyond the bounded projection.
	longCode := "````python\nmatching " + strings.Repeat("x", 20000) + "\n``` embedded\n````"
	want["CODE"][insertMarkdown(f.alice, messageOptions{body: &longCode})] = true
	want["MARKDOWN"][insertMarkdown(f.alice, messageOptions{body: textPointer("# matching prose")})] = true
	want["CODE"][f.insertMessage(t, f.alice, messageOptions{body: textPointer("matching legacy"), detectedType: textPointer("CODE")})] = true
	legacy := f.insertMessage(t, f.alice, messageOptions{body: textPointer("matching language hint")})
	if _, err := f.db.Exec(context.Background(), "UPDATE messages SET detected_language='bash' WHERE id=$1", legacy); err != nil {
		t.Fatal(err)
	}
	want["CODE"][legacy] = true
	want["TEXT"][f.insertMessage(t, f.alice, messageOptions{body: textPointer("matching plain")})] = true
	// Ineligible candidates cannot leak through classification or pagination.
	body := "```bash\nmatching private\n```"
	insertMarkdown(f.bob, messageOptions{body: &body})
	insertMarkdown(f.alice, messageOptions{body: &body, sensitive: true})
	expired, trashed := f.now.Add(-time.Hour), f.now.Add(-time.Minute)
	insertMarkdown(f.alice, messageOptions{body: &body, lifecycle: "TEMPORARY", expiresAt: &expired})
	insertMarkdown(f.alice, messageOptions{body: &body, trashedAt: &trashed})
	for kind, expected := range want {
		query := Query{Tokens: []string{"matching"}, DetectedType: &kind, Limit: 2}
		seen := map[uuid.UUID]bool{}
		for {
			page, err := f.service.Search(context.Background(), f.alice, query)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range page.Items {
				if seen[item.ID] || !expected[item.ID] {
					t.Fatalf("%s unexpected/duplicate %s", kind, item.ID)
				}
				seen[item.ID] = true
			}
			if page.NextCursor == nil {
				break
			}
			cursor, err := DecodeCursor(*page.NextCursor)
			if err != nil {
				t.Fatal(err)
			}
			query.Cursor = &cursor
		}
		if len(seen) != len(expected) {
			t.Fatalf("%s: got %d, want %d", kind, len(seen), len(expected))
		}
	}
}

func TestSavedSearchMalformedHistoricalConditions(t *testing.T) {
	f := newIntegrationFixture(t)
	h := NewHandler(f.service)
	h.SetSavedRepository(f.db)
	base := httpapi.SearchConditions{Q: "matching", Time: "all", Timezone: "UTC", TagIds: []uuid.UUID{}}
	raw, _ := json.Marshal(base)
	for _, missing := range []bool{false, true} {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		if missing {
			delete(fields, "tagIds")
		} else {
			fields["tagIds"] = []byte("null")
		}
		bad, _ := json.Marshal(fields)
		id := uuid.Must(uuid.NewV7())
		if _, err := f.db.Exec(context.Background(), "INSERT INTO saved_searches(id,owner_id,name,conditions) VALUES($1,$2,'old invalid',$3)", id, f.alice, bad); err != nil {
			t.Fatal(err)
		}
	}
	response := httptest.NewRecorder()
	h.ListSavedSearches(response, authenticatedSearchRequest("/", f.alice))
	var items []httpapi.SavedSearch
	// Response conditions remain malformed on purpose; do not execute them.
	var wire []struct {
		ID            uuid.UUID `json:"id"`
		InvalidReason string    `json:"invalidReason"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil || len(wire) != 2 {
		t.Fatalf("list: %d %s", response.Code, response.Body.String())
	}
	for _, item := range wire {
		if item.InvalidReason == "" {
			t.Fatal("silently normalized historical conditions")
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &items); err == nil {
		t.Fatal("malformed conditions decoded as valid")
	}
	// Updating through the real handler explicitly repairs the persisted rows.
	for _, item := range wire {
		body, _ := json.Marshal(httpapi.SavedSearchRequest{Name: "Repaired", Conditions: base})
		request := authenticatedSearchRequest("/", f.alice)
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		result := httptest.NewRecorder()
		h.UpdateSavedSearch(result, request, item.ID)
		if result.Code != 200 {
			t.Fatalf("repair: %d %s", result.Code, result.Body.String())
		}
	}
	response = httptest.NewRecorder()
	h.ListSavedSearches(response, authenticatedSearchRequest("/", f.alice))
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.InvalidReason != "" || item.Conditions.TagIds == nil {
			t.Fatalf("repair failed: %+v", item)
		}
	}
}
