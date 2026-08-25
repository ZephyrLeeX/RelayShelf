package uploads

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/google/uuid"
)

const maxCreateBody = 4 << 10

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeCreate(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxCreateBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
		return false
	}
	return true
}

func mapError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		auth.WriteError(w, r, http.StatusNotFound, "UPLOAD_NOT_FOUND", "upload not found")
	case errors.Is(err, ErrFileTooLarge):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "UPLOAD_FILE_TOO_LARGE", "file exceeds the configured limit")
	case errors.Is(err, ErrStagingFull):
		auth.WriteError(w, r, http.StatusServiceUnavailable, "UPLOAD_STAGING_FULL", "upload staging capacity is unavailable")
	case errors.Is(err, ErrStagingUnavailable):
		auth.WriteError(w, r, http.StatusServiceUnavailable, "UPLOAD_STAGING_UNAVAILABLE", "upload staging is unavailable")
	case errors.Is(err, ErrExpired):
		auth.WriteError(w, r, http.StatusGone, "UPLOAD_EXPIRED", "upload expired")
	case errors.Is(err, ErrInvalidState):
		auth.WriteError(w, r, http.StatusConflict, "UPLOAD_INVALID_STATE", "upload is not writable")
	case errors.Is(err, ErrPartOutOfRange):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "UPLOAD_PART_OUT_OF_RANGE", "part number is out of range")
	case errors.Is(err, ErrPartSizeMismatch):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "UPLOAD_PART_SIZE_MISMATCH", "part size does not match")
	case errors.Is(err, ErrIncomplete):
		auth.WriteError(w, r, http.StatusConflict, "UPLOAD_INCOMPLETE", "upload parts are incomplete")
	case errors.Is(err, ErrStagingCorrupt):
		auth.WriteError(w, r, http.StatusConflict, "UPLOAD_STAGING_CORRUPT", "upload staging is corrupt")
	case errors.Is(err, ErrFinalizeRetryable):
		auth.WriteError(w, r, http.StatusServiceUnavailable, "UPLOAD_FINALIZE_RETRYABLE", "file finalization can be retried")
	case errors.Is(err, ErrStorageQuota):
		auth.WriteError(w, r, http.StatusInsufficientStorage, "STORAGE_QUOTA_EXCEEDED", "logical storage quota exceeded")
	case errors.Is(err, ErrValidation):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
	default:
		auth.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}

func apiSession(row Session) httpapi.UploadSession {
	parts := row.CompletedParts
	if parts == nil {
		parts = []int{}
	}
	return httpapi.UploadSession{Id: row.ID, OriginalFilename: row.OriginalFilename, ExpectedSize: row.ExpectedSize, ClientMime: row.ClientMime, ChunkSize: row.ChunkSize, PartCount: row.PartCount(), Status: httpapi.UploadStatus(row.Status), ExpiresAt: row.ExpiresAt.UTC(), CompletedParts: parts, CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC()}
}

func owner(r *http.Request) uuid.UUID {
	actor, _ := auth.FromContext(r.Context())
	return actor.User.ID
}

func (h *Handler) CreateUpload(w http.ResponseWriter, r *http.Request) {
	var body httpapi.CreateUploadRequest
	if !decodeCreate(w, r, &body) {
		return
	}
	row, err := h.service.Create(r.Context(), owner(r), CreateCommand{OriginalFilename: body.OriginalFilename, ExpectedSize: body.ExpectedSize, ClientMime: body.ClientMime})
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, apiSession(row))
}

func (h *Handler) GetUpload(w http.ResponseWriter, r *http.Request, uploadID httpapi.UploadId) {
	row, err := h.service.Get(r.Context(), owner(r), uuid.UUID(uploadID))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiSession(row))
}

func (h *Handler) PutUploadPart(w http.ResponseWriter, r *http.Request, uploadID httpapi.UploadId, partNumber httpapi.PartNumber) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/octet-stream") {
		auth.WriteError(w, r, http.StatusUnsupportedMediaType, "VALIDATION_ERROR", "content type must be application/octet-stream")
		return
	}
	if err = h.service.PutPart(r.Context(), owner(r), uuid.UUID(uploadID), partNumber, r.ContentLength, r.Body); err != nil {
		mapError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CompleteUpload(w http.ResponseWriter, r *http.Request, uploadID httpapi.UploadId) {
	row, err := h.service.Complete(r.Context(), owner(r), uuid.UUID(uploadID))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiSession(row))
}
