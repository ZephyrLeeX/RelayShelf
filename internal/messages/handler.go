package messages

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
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

func (h *Handler) publish(r *http.Request, userID uuid.UUID, eventType string, resourceID uuid.UUID, version *int64, origin *uuid.UUID) {
	if h.publisher == nil || h.ids == nil || h.clock == nil {
		return
	}
	eventID, err := h.ids.New()
	if err != nil {
		return
	}
	h.publisher.Publish(userID, realtime.Event{ID: eventID, Type: eventType, ResourceID: resourceID, Version: version, OriginDeviceID: origin, OccurredAt: h.clock.Now()})
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONEnvelopeBytes)
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
func actor(r *http.Request) auth.AuthContext { a, _ := auth.FromContext(r.Context()); return a }
func mapError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		auth.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, ErrVersionConflict):
		auth.WriteError(w, r, http.StatusConflict, "MESSAGE_VERSION_CONFLICT", "message version conflict")
	case errors.Is(err, ErrFavoriteRequiresPermanent):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "MESSAGE_FAVORITE_REQUIRES_PERMANENT", "favorite requires a permanent message")
	case errors.Is(err, ErrNotSensitive):
		auth.WriteError(w, r, http.StatusConflict, "MESSAGE_NOT_SENSITIVE", "message is not sensitive")
	case errors.Is(err, ErrTrashed):
		auth.WriteError(w, r, http.StatusConflict, "MESSAGE_TRASHED", "message is trashed")
	case errors.Is(err, ErrNotTrashed):
		auth.WriteError(w, r, http.StatusConflict, "MESSAGE_NOT_TRASHED", "message is not trashed")
	case errors.Is(err, ErrIdempotencyKeyReused):
		auth.WriteError(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "idempotency key was used for another request")
	case errors.Is(err, ErrRecipientUnavailable):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "RECIPIENT_UNAVAILABLE", "recipient is unavailable")
	case errors.Is(err, ErrValidation):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
	case errors.Is(err, ErrContentRequired):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "MESSAGE_CONTENT_REQUIRED", "a body or attachment is required")
	case errors.Is(err, ErrUploadAlreadyConsumed):
		auth.WriteError(w, r, http.StatusConflict, "UPLOAD_ALREADY_CONSUMED", "upload was already consumed")
	default:
		auth.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}

func tagDTO(t Tag) httpapi.Tag {
	return httpapi.Tag{Id: t.ID, Name: t.Name, Color: t.Color, CreatedAt: t.CreatedAt.UTC(), UpdatedAt: t.UpdatedAt.UTC()}
}
func receiptDTO(receipt MessageDeliveryReceipt) httpapi.MessageDeliveryReceipt {
	return httpapi.MessageDeliveryReceipt{MessageId: receipt.MessageID, CreatedAt: receipt.CreatedAt.UTC(), ExpiresAt: receipt.ExpiresAt.UTC()}
}
func messageDTO(m Message) httpapi.Message {
	tags := make([]httpapi.Tag, 0, len(m.Tags))
	for _, t := range m.Tags {
		tags = append(tags, tagDTO(t))
	}
	body := m.BodyPlaintext
	if m.Sensitive {
		body = nil
	}
	attachments := make([]httpapi.AttachmentSummary, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		attachments = append(attachments, attachmentDTO(a))
	}
	return httpapi.Message{Id: m.ID, Body: body, BodyFormat: httpapi.BodyFormat(m.BodyFormat), DetectedType: m.DetectedType, DetectedLanguage: m.DetectedLanguage, Sensitive: m.Sensitive, Lifecycle: httpapi.Lifecycle(m.Lifecycle), Favorite: m.Favorite, ExpiresAt: m.ExpiresAt, TrashedAt: m.TrashedAt, PurgeAt: m.PurgeAt, SourceUserId: m.SourceUserID, SourceMessageId: m.SourceMessageID, Version: m.Version, CreatedAt: m.CreatedAt.UTC(), UpdatedAt: m.UpdatedAt.UTC(), Tags: tags, Attachments: attachments}
}
func attachmentDTO(a Attachment) httpapi.AttachmentSummary {
	return httpapi.AttachmentSummary{Id: a.ID, OriginalFilename: a.OriginalFilename, ClientMime: a.ClientMime, DetectedMime: a.DetectedMime, SizeBytes: a.SizeBytes, DisplayOrder: a.DisplayOrder}
}

// SummaryDTO maps the shared MessageSummary read model to the OpenAPI boundary.
// Search and message listing deliberately use the same response contract.
func SummaryDTO(s Summary) httpapi.MessageSummary {
	m := messageDTO(s.Message)
	return httpapi.MessageSummary{Id: m.Id, Body: nil, BodyPreview: s.BodyPreview, BodyTruncated: s.BodyTruncated, BodyFormat: m.BodyFormat, DetectedType: m.DetectedType, DetectedLanguage: m.DetectedLanguage, Sensitive: m.Sensitive, Lifecycle: m.Lifecycle, Favorite: m.Favorite, ExpiresAt: m.ExpiresAt, TrashedAt: m.TrashedAt, PurgeAt: m.PurgeAt, SourceUserId: m.SourceUserId, SourceMessageId: m.SourceMessageId, Version: m.Version, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, Tags: m.Tags, Attachments: m.Attachments, AttachmentCount: s.AttachmentCount}
}

func defaults(format *httpapi.BodyFormat, lifecycle *httpapi.Lifecycle, sensitive *bool) (string, string, bool) {
	f := Text
	if format != nil {
		f = string(*format)
	}
	l := Temporary
	if lifecycle != nil {
		l = string(*lifecycle)
	}
	secret := false
	if sensitive != nil {
		secret = *sensitive
	}
	return f, l, secret
}

func (h *Handler) CreateMessage(w http.ResponseWriter, r *http.Request, params httpapi.CreateMessageParams) {
	var body httpapi.CreateMessageRequest
	if !decode(w, r, &body) {
		return
	}
	a := actor(r)
	format, lifecycle, sensitive := defaults(body.BodyFormat, body.Lifecycle, body.Sensitive)
	ids := []uuid.UUID{}
	if body.TagIds != nil {
		for _, value := range *body.TagIds {
			ids = append(ids, uuid.UUID(value))
		}
	}
	uploadIDs := []uuid.UUID{}
	if body.UploadIds != nil {
		for _, value := range *body.UploadIds {
			uploadIDs = append(uploadIDs, uuid.UUID(value))
		}
	}
	messageBody := ""
	if body.Body != nil {
		messageBody = *body.Body
	}
	m, err := h.service.Create(r.Context(), a.User.ID, a.Device.ID, CreateCommand{Body: messageBody, BodyFormat: format, Lifecycle: lifecycle, Sensitive: sensitive, TagIDs: ids, UploadIDs: uploadIDs, IdempotencyKey: params.IdempotencyKey})
	if err != nil {
		mapError(w, r, err)
		return
	}
	version := m.Version
	origin := a.Device.ID
	h.publish(r, a.User.ID, realtime.MessageCreated, m.ID, &version, &origin)
	writeJSON(w, http.StatusCreated, messageDTO(m))
}
func (h *Handler) GetMessage(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	m, err := h.service.Detail(r.Context(), actor(r).User.ID, uuid.UUID(messageID))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, messageDTO(m))
}
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request, params httpapi.ListMessagesParams) {
	h.list(w, r, params.Lifecycle, params.Favorite, params.TagId, params.Cursor, params.Limit, false)
}
func (h *Handler) ListTrash(w http.ResponseWriter, r *http.Request, params httpapi.ListTrashParams) {
	h.list(w, r, nil, nil, nil, params.Cursor, params.Limit, true)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request, lifecycle *httpapi.Lifecycle, favorite *bool, tagIDs *[]uuid.UUID, cursor *string, limit *int, trash bool) {
	filter := ListFilter{Favorite: favorite}
	if lifecycle != nil {
		value := string(*lifecycle)
		filter.Lifecycle = &value
	}
	if tagIDs != nil {
		filter.TagIDs = append(filter.TagIDs, (*tagIDs)...)
	}
	if cursor != nil {
		decoded, err := DecodeCursor(*cursor)
		if err != nil {
			mapError(w, r, err)
			return
		}
		filter.Cursor = &decoded
	}
	if limit != nil {
		filter.Limit = *limit
	}
	page, err := h.service.List(r.Context(), actor(r).User.ID, filter, trash)
	if err != nil {
		mapError(w, r, err)
		return
	}
	items := make([]httpapi.MessageSummary, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, SummaryDTO(item))
	}
	writeJSON(w, http.StatusOK, httpapi.MessageList{Items: items, NextCursor: page.NextCursor})
}
func (h *Handler) EditMessage(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	var body struct {
		ExpectedVersion  int64               `json:"expectedVersion"`
		Body             json.RawMessage     `json:"body"`
		BodyFormat       *httpapi.BodyFormat `json:"bodyFormat"`
		DetectedType     json.RawMessage     `json:"detectedType"`
		DetectedLanguage json.RawMessage     `json:"detectedLanguage"`
	}
	if !decode(w, r, &body) {
		return
	}
	command := EditCommand{ExpectedVersion: body.ExpectedVersion}
	if len(body.Body) > 0 {
		if string(body.Body) == "null" {
			command.BodyClear = true
		} else {
			var value string
			if json.Unmarshal(body.Body, &value) != nil {
				mapError(w, r, ErrValidation)
				return
			}
			command.Body = &value
		}
	}
	if body.BodyFormat != nil {
		value := string(*body.BodyFormat)
		command.BodyFormat = &value
	}
	if len(body.DetectedType) > 0 {
		var value *string
		if string(body.DetectedType) != "null" {
			var text string
			if json.Unmarshal(body.DetectedType, &text) != nil {
				mapError(w, r, ErrValidation)
				return
			}
			value = &text
		}
		command.DetectedType = OptionalString{Set: true, Value: value}
	}
	if len(body.DetectedLanguage) > 0 {
		var value *string
		if string(body.DetectedLanguage) != "null" {
			var text string
			if json.Unmarshal(body.DetectedLanguage, &text) != nil {
				mapError(w, r, ErrValidation)
				return
			}
			value = &text
		}
		command.DetectedLanguage = OptionalString{Set: true, Value: value}
	}
	m, err := h.service.Edit(r.Context(), actor(r).User.ID, uuid.UUID(messageID), command)
	h.respondMessage(w, r, m, err)
}
func (h *Handler) respondMessage(w http.ResponseWriter, r *http.Request, m Message, err error) {
	if err != nil {
		mapError(w, r, err)
		return
	}
	a := actor(r)
	version := m.Version
	origin := a.Device.ID
	h.publish(r, a.User.ID, realtime.MessageUpdated, m.ID, &version, &origin)
	writeJSON(w, http.StatusOK, messageDTO(m))
}
func (h *Handler) MakeMessagePermanent(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	var body httpapi.VersionRequest
	if !decode(w, r, &body) {
		return
	}
	m, err := h.service.MakePermanent(r.Context(), actor(r).User.ID, uuid.UUID(messageID), body.ExpectedVersion)
	h.respondMessage(w, r, m, err)
}
func (h *Handler) SetMessageFavorite(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	var body httpapi.FavoriteRequest
	if !decode(w, r, &body) {
		return
	}
	m, err := h.service.SetFavorite(r.Context(), actor(r).User.ID, uuid.UUID(messageID), body.ExpectedVersion, body.Favorite)
	h.respondMessage(w, r, m, err)
}
func (h *Handler) TrashMessage(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	var body httpapi.VersionRequest
	if !decode(w, r, &body) {
		return
	}
	m, err := h.service.Trash(r.Context(), actor(r).User.ID, uuid.UUID(messageID), body.ExpectedVersion)
	h.respondMessage(w, r, m, err)
}
func (h *Handler) RestoreMessage(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	var body httpapi.VersionRequest
	if !decode(w, r, &body) {
		return
	}
	m, err := h.service.Restore(r.Context(), actor(r).User.ID, uuid.UUID(messageID), body.ExpectedVersion)
	h.respondMessage(w, r, m, err)
}
func (h *Handler) ReplaceMessageTags(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	var body httpapi.ReplaceMessageTagsRequest
	if !decode(w, r, &body) {
		return
	}
	ids := make([]uuid.UUID, 0, len(body.TagIds))
	for _, value := range body.TagIds {
		ids = append(ids, uuid.UUID(value))
	}
	m, err := h.service.ReplaceTags(r.Context(), actor(r).User.ID, uuid.UUID(messageID), body.ExpectedVersion, ids)
	h.respondMessage(w, r, m, err)
}
func (h *Handler) SetMessageSensitive(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	var body httpapi.SensitiveRequest
	if !decode(w, r, &body) {
		return
	}
	m, err := h.service.SetSensitive(r.Context(), actor(r).User.ID, uuid.UUID(messageID), body.ExpectedVersion, body.Sensitive)
	h.respondMessage(w, r, m, err)
}
func (h *Handler) RevealSensitiveBody(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	body, version, err := h.service.Reveal(r.Context(), actor(r).User.ID, uuid.UUID(messageID))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, httpapi.SensitiveBody{Body: body, Version: version})
}
func (h *Handler) EditSensitiveBody(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	var body httpapi.EditSensitiveBodyRequest
	if !decode(w, r, &body) {
		return
	}
	m, err := h.service.EditSensitive(r.Context(), actor(r).User.ID, uuid.UUID(messageID), body.ExpectedVersion, body.Body)
	h.respondMessage(w, r, m, err)
}
func (h *Handler) DirectSendMessage(w http.ResponseWriter, r *http.Request, params httpapi.DirectSendMessageParams) {
	var body httpapi.DirectSendRequest
	if !decode(w, r, &body) {
		return
	}
	format, _, sensitive := defaults(body.BodyFormat, nil, body.Sensitive)
	a := actor(r)
	uploadIDs := []uuid.UUID{}
	if body.UploadIds != nil {
		for _, value := range *body.UploadIds {
			uploadIDs = append(uploadIDs, uuid.UUID(value))
		}
	}
	messageBody := ""
	if body.Body != nil {
		messageBody = *body.Body
	}
	receipt, err := h.service.DirectSend(r.Context(), a.User.ID, a.Device.ID, DirectSendCommand{RecipientID: uuid.UUID(body.RecipientUserId), Body: messageBody, BodyFormat: format, Sensitive: sensitive, UploadIDs: uploadIDs, IdempotencyKey: params.IdempotencyKey})
	if err != nil {
		mapError(w, r, err)
		return
	}
	origin := a.Device.ID
	h.publish(r, uuid.UUID(body.RecipientUserId), realtime.MessageCreated, receipt.MessageID, nil, &origin)
	writeJSON(w, http.StatusCreated, receiptDTO(receipt))
}

func (h *Handler) AddMessageAttachments(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	var body httpapi.AddAttachmentsRequest
	if !decode(w, r, &body) {
		return
	}
	ids := make([]uuid.UUID, 0, len(body.UploadIds))
	for _, v := range body.UploadIds {
		ids = append(ids, uuid.UUID(v))
	}
	m, err := h.service.AddAttachments(r.Context(), actor(r).User.ID, uuid.UUID(messageID), body.ExpectedVersion, ids)
	h.respondMessage(w, r, m, err)
}
func (h *Handler) RemoveMessageAttachment(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId, attachmentID httpapi.AttachmentId) {
	var body httpapi.VersionRequest
	if !decode(w, r, &body) {
		return
	}
	m, err := h.service.RemoveAttachment(r.Context(), actor(r).User.ID, uuid.UUID(messageID), uuid.UUID(attachmentID), body.ExpectedVersion)
	h.respondMessage(w, r, m, err)
}
func (h *Handler) ForwardMessage(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId, params httpapi.ForwardMessageParams) {
	var body httpapi.ForwardRequest
	if !decode(w, r, &body) {
		return
	}
	a := actor(r)
	receipt, err := h.service.Forward(r.Context(), a.User.ID, a.Device.ID, ForwardCommand{SourceID: uuid.UUID(messageID), RecipientID: uuid.UUID(body.RecipientUserId), ExpectedVersion: body.ExpectedVersion, IdempotencyKey: params.IdempotencyKey})
	if err != nil {
		mapError(w, r, err)
		return
	}
	origin := a.Device.ID
	h.publish(r, uuid.UUID(body.RecipientUserId), realtime.MessageCreated, receipt.MessageID, nil, &origin)
	writeJSON(w, http.StatusCreated, receiptDTO(receipt))
}
func (h *Handler) PermanentlyDeleteMessage(w http.ResponseWriter, r *http.Request, messageID httpapi.MessageId) {
	a := actor(r)
	info, _ := auth.RequestInfo(r.Context())
	err := h.service.PermanentlyDelete(r.Context(), a.User.ID, uuid.UUID(messageID), AuditContext{DeviceID: a.Device.ID, SessionID: a.Session.ID, TraceID: httpx.TraceID(r), IP: info.ClientIP.String(), UserAgent: boundedUTF8(r.UserAgent(), 512)})
	if err != nil {
		mapError(w, r, err)
		return
	}
	origin := a.Device.ID
	h.publish(r, a.User.ID, realtime.MessageDeleted, uuid.UUID(messageID), nil, &origin)
	w.WriteHeader(http.StatusNoContent)
}

func boundedUTF8(value string, max int) string {
	for len(value) > max {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}
