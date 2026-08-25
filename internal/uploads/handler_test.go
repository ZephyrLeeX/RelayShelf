package uploads

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/google/uuid"
)

func authenticated(r *http.Request, owner uuid.UUID) *http.Request {
	return r.WithContext(auth.ContextWithAuthentication(r.Context(), auth.Authentication{User: auth.User{ID: owner}}))
}

type tinyReader struct {
	data  []byte
	reads int
}

func (r *tinyReader) Read(p []byte) (int, error) {
	r.reads++
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

func TestUploadHandlerCreateStatusAndRawStreaming(t *testing.T) {
	c := &testClock{now: time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)}
	owner, uploadID := uuid.New(), uuid.New()
	repo, stage := newMemoryRepo(), newMemoryStaging()
	service := testService(repo, stage, c, 8, uploadID)
	handler := NewHandler(service)

	create := authenticated(httptest.NewRequest(http.MethodPost, "/api/v1/uploads", strings.NewReader(`{"originalFilename":"safe.bin","expectedSize":4}`)), owner)
	create.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.CreateUpload(recorder, create)
	if recorder.Code != http.StatusCreated || strings.Contains(recorder.Body.String(), "staging") || strings.Contains(recorder.Body.String(), "fileObject") {
		t.Fatalf("create status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["chunkSize"] != float64(ChunkSize) || payload["status"] != Created {
		t.Fatalf("payload=%v", payload)
	}

	reader := &tinyReader{data: []byte("data")}
	put := authenticated(httptest.NewRequest(http.MethodPut, "/api/v1/uploads/"+uploadID.String()+"/parts/0", reader), owner)
	put.Header.Set("Content-Type", "application/octet-stream")
	put.ContentLength = -1
	putRecorder := httptest.NewRecorder()
	handler.PutUploadPart(putRecorder, put, uploadID, 0)
	if putRecorder.Code != http.StatusNoContent || putRecorder.Body.Len() != 0 || reader.reads < 4 {
		t.Fatalf("put status=%d reads=%d body=%s", putRecorder.Code, reader.reads, putRecorder.Body.String())
	}

	status := authenticated(httptest.NewRequest(http.MethodGet, "/api/v1/uploads/"+uploadID.String(), nil), owner)
	statusRecorder := httptest.NewRecorder()
	handler.GetUpload(statusRecorder, status, uploadID)
	if statusRecorder.Code != http.StatusOK || !bytes.Contains(statusRecorder.Body.Bytes(), []byte(`"completedParts":[0]`)) {
		t.Fatalf("status=%d body=%s", statusRecorder.Code, statusRecorder.Body.String())
	}
}

func TestUploadHandlerRejectsUnknownCreateFieldsAndWrongMedia(t *testing.T) {
	c := &testClock{now: time.Now().UTC()}
	owner, id := uuid.New(), uuid.New()
	repo, stage := newMemoryRepo(), newMemoryStaging()
	handler := NewHandler(testService(repo, stage, c, 8, id))
	request := authenticated(httptest.NewRequest(http.MethodPost, "/api/v1/uploads", strings.NewReader(`{"originalFilename":"x","expectedSize":1,"chunkSize":1}`)), owner)
	rec := httptest.NewRecorder()
	handler.CreateUpload(rec, request)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
		t.Fatalf("create=%d %s", rec.Code, rec.Body.String())
	}
	put := authenticated(httptest.NewRequest(http.MethodPut, "/api/v1/uploads/"+id.String()+"/parts/0", strings.NewReader("x")), owner)
	put.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	handler.PutUploadPart(putRec, put, id, 0)
	if putRec.Code != http.StatusUnsupportedMediaType || strings.Contains(putRec.Body.String(), "x\"") {
		t.Fatalf("put=%d %s", putRec.Code, putRec.Body.String())
	}
}

func TestUploadHandlerMapsWriteAtFailureAndRetrySucceeds(t *testing.T) {
	c := &testClock{now: time.Now().UTC()}
	owner, id := uuid.New(), uuid.New()
	repo, stage := newMemoryRepo(), newMemoryStaging()
	putSession(repo, stage, id, owner, 4, c)
	stage.writeErr[id] = syscall.ENOSPC
	handler := NewHandler(testService(repo, stage, c, 8, id))

	request := authenticated(httptest.NewRequest(http.MethodPut, "/api/v1/uploads/"+id.String()+"/parts/0", strings.NewReader("data")), owner)
	request.Header.Set("Content-Type", "application/octet-stream")
	request.ContentLength = 4
	recorder := httptest.NewRecorder()
	handler.PutUploadPart(recorder, request, id, 0)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "UPLOAD_STAGING_UNAVAILABLE") {
		t.Fatalf("put=%d %s", recorder.Code, recorder.Body.String())
	}
	if len(repo.parts[id]) != 0 {
		t.Fatal("WriteAt failure created a part marker")
	}

	delete(stage.writeErr, id)
	retry := authenticated(httptest.NewRequest(http.MethodPut, "/api/v1/uploads/"+id.String()+"/parts/0", strings.NewReader("data")), owner)
	retry.Header.Set("Content-Type", "application/octet-stream")
	retry.ContentLength = 4
	retryRecorder := httptest.NewRecorder()
	handler.PutUploadPart(retryRecorder, retry, id, 0)
	if retryRecorder.Code != http.StatusNoContent || len(repo.parts[id]) != 1 {
		t.Fatalf("retry=%d %s markers=%d", retryRecorder.Code, retryRecorder.Body.String(), len(repo.parts[id]))
	}
}
