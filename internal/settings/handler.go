package settings

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) GetRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.Get(r.Context())
	if err != nil {
		auth.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}
	write(w, http.StatusOK, dto(value))
}

func (h *Handler) UpdateRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	var body httpapi.UpdateRuntimeSettingsRequest
	if !decode(w, r, &body) {
		return
	}
	value, err := h.service.Update(r.Context(), actor(r), Settings{TemporaryTTLHours: body.TemporaryTtlHours, TrashTTLHours: body.TrashTtlHours, MaxFileSizeBytes: body.MaxFileSizeBytes, MaxStorageBytes: body.MaxStorageBytes, AuditRetentionDays: body.AuditRetentionDays, UploadRetentionHours: body.UploadRetentionHours})
	if errors.Is(err, ErrValidation) {
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "RUNTIME_SETTINGS_INVALID", "runtime settings are invalid")
		return
	}
	if err != nil {
		auth.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}
	write(w, http.StatusOK, dto(value))
}

func dto(value Settings) httpapi.RuntimeSettings {
	return httpapi.RuntimeSettings{TemporaryTtlHours: value.TemporaryTTLHours, TrashTtlHours: value.TrashTTLHours, MaxFileSizeBytes: value.MaxFileSizeBytes, MaxStorageBytes: value.MaxStorageBytes, AuditRetentionDays: value.AuditRetentionDays, UploadRetentionHours: value.UploadRetentionHours, UpdatedAt: value.UpdatedAt.UTC(), UpdatedByUserId: value.UpdatedByUserID}
}

func actor(r *http.Request) audit.Actor {
	a, _ := auth.FromContext(r.Context())
	info, _ := auth.RequestInfo(r.Context())
	return audit.Actor{UserID: a.User.ID, DeviceID: a.Device.ID, SessionID: a.Session.ID, IP: info.ClientIP, UserAgent: r.UserAgent(), TraceID: httpx.TraceID(r)}
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "RUNTIME_SETTINGS_INVALID", "runtime settings are invalid")
		return false
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "RUNTIME_SETTINGS_INVALID", "runtime settings are invalid")
		return false
	}
	return true
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
