package tags

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/realtime"
	"github.com/google/uuid"
)

type Handler struct {
	service   *Service
	publisher realtime.Publisher
	ids       id.Generator
	clock     Clock
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) SetPublisher(p realtime.Publisher, ids id.Generator, clock Clock) {
	h.publisher, h.ids, h.clock = p, ids, clock
}
func (h *Handler) publish(r *http.Request, eventType string, resourceID uuid.UUID) {
	if h.publisher == nil || h.ids == nil || h.clock == nil {
		return
	}
	eventID, err := h.ids.New()
	if err != nil {
		return
	}
	a, _ := auth.FromContext(r.Context())
	origin := a.Device.ID
	h.publisher.Publish(a.User.ID, realtime.Event{ID: eventID, Type: eventType, ResourceID: resourceID, OriginDeviceID: &origin, OccurredAt: h.clock.Now()})
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func owner(r *http.Request) uuid.UUID { a, _ := auth.FromContext(r.Context()); return a.User.ID }
func dto(t Tag) httpapi.Tag {
	return httpapi.Tag{Id: t.ID, Name: t.Name, Color: t.Color, CreatedAt: t.CreatedAt.UTC(), UpdatedAt: t.UpdatedAt.UTC()}
}
func mapError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		auth.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, ErrDuplicate):
		auth.WriteError(w, r, http.StatusConflict, "TAG_ALREADY_EXISTS", "tag already exists")
	case errors.Is(err, ErrValidation):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
	default:
		auth.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}
func (h *Handler) ListTags(w http.ResponseWriter, r *http.Request) {
	rows, err := h.service.List(r.Context(), owner(r))
	if err != nil {
		mapError(w, r, err)
		return
	}
	out := make([]httpapi.Tag, 0, len(rows))
	for _, row := range rows {
		out = append(out, dto(row))
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *Handler) CreateTag(w http.ResponseWriter, r *http.Request) {
	var body httpapi.TagRequest
	if !decode(w, r, &body) {
		return
	}
	tag, err := h.service.Create(r.Context(), owner(r), body.Name, body.Color)
	if err != nil {
		mapError(w, r, err)
		return
	}
	h.publish(r, realtime.TagCreated, tag.ID)
	writeJSON(w, http.StatusCreated, dto(tag))
}
func (h *Handler) UpdateTag(w http.ResponseWriter, r *http.Request, tagID httpapi.TagId) {
	var body httpapi.UpdateTagRequest
	if !decode(w, r, &body) {
		return
	}
	tag, err := h.service.Update(r.Context(), owner(r), uuid.UUID(tagID), body.Name, body.Color)
	if err != nil {
		mapError(w, r, err)
		return
	}
	h.publish(r, realtime.TagUpdated, tag.ID)
	writeJSON(w, http.StatusOK, dto(tag))
}
func (h *Handler) DeleteTag(w http.ResponseWriter, r *http.Request, tagID httpapi.TagId) {
	if err := h.service.Delete(r.Context(), owner(r), uuid.UUID(tagID)); err != nil {
		mapError(w, r, err)
		return
	}
	h.publish(r, realtime.TagDeleted, uuid.UUID(tagID))
	w.WriteHeader(http.StatusNoContent)
}
