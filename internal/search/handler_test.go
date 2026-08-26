package search

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
	"github.com/google/uuid"
)

func authenticatedSearchRequest(path string, userID uuid.UUID) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	return request.WithContext(auth.ContextWithAuthentication(request.Context(), auth.Authentication{User: auth.User{ID: userID}}))
}

func TestHandlerValidationIsNoStoreAndDoesNotEchoQuery(t *testing.T) {
	handler := NewHandler(NewService(&fakeRepository{}, fixedClock{time.Now()}))
	request := authenticatedSearchRequest("/api/v1/search?q=s", uuid.Must(uuid.NewV7()))
	recorder := httptest.NewRecorder()
	query := "my-private-secret"
	handler.SearchMessages(recorder, request, httpapi.SearchMessagesParams{Q: &query})
	if recorder.Code != http.StatusOK {
		t.Fatalf("valid query status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	short := "密"
	recorder = httptest.NewRecorder()
	handler.SearchMessages(recorder, request, httpapi.SearchMessagesParams{Q: &short})
	if recorder.Code != http.StatusUnprocessableEntity || recorder.Header().Get("Cache-Control") != "no-store" || strings.Contains(recorder.Body.String(), short) {
		t.Fatalf("status=%d cache=%q body=%s", recorder.Code, recorder.Header().Get("Cache-Control"), recorder.Body.String())
	}
	var response struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Code != "SEARCH_QUERY_TOO_SHORT" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestHandlerRejectsCursorWithTrailingJSON(t *testing.T) {
	handler := NewHandler(NewService(&fakeRepository{}, fixedClock{time.Now()}))
	userID := uuid.Must(uuid.NewV7())
	valid := EncodeCursor(Cursor{CreatedAt: time.Now(), MessageID: uuid.Must(uuid.NewV7())})
	raw, err := base64.RawURLEncoding.DecodeString(valid)
	if err != nil {
		t.Fatal(err)
	}
	invalid := base64.RawURLEncoding.EncodeToString(append(raw, []byte(`{}`)...))
	request := authenticatedSearchRequest("/api/v1/search", userID)
	recorder := httptest.NewRecorder()
	handler.SearchMessages(recorder, request, httpapi.SearchMessagesParams{Cursor: &invalid})
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), `"code":"SEARCH_CURSOR_INVALID"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerUsesSharedSummaryAndDoesNotLeakInternalFileMetadata(t *testing.T) {
	userID, messageID, attachmentID, fileID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	repository := &fakeRepository{rows: []messages.Message{{
		ID: messageID, OwnerID: userID, Sensitive: true, BodyFormat: messages.Text, Lifecycle: messages.Permanent,
		CreatedAt: time.Now(), UpdatedAt: time.Now(), Tags: []messages.Tag{}, AttachmentTotal: 1,
		Attachments: []messages.Attachment{{ID: attachmentID, FileObjectID: fileID, OriginalFilename: "safe.pdf", DetectedMime: "application/pdf"}},
	}}}
	handler := NewHandler(NewService(repository, fixedClock{time.Now()}))
	request := authenticatedSearchRequest("/api/v1/search", userID)
	recorder := httptest.NewRecorder()
	handler.SearchMessages(recorder, request, httpapi.SearchMessagesParams{})
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", recorder.Code, recorder.Header(), body)
	}
	for _, forbidden := range []string{"fileObjectId", "sha256", "storageKey", "bodyCiphertext", fileID.String()} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"bodyPreview":null`) || !strings.Contains(body, `"bodyTruncated":false`) {
		t.Fatalf("sensitive summary shape=%s", body)
	}
}
