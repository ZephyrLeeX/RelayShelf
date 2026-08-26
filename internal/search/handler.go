package search

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) SearchMessages(w http.ResponseWriter, r *http.Request, params httpapi.SearchMessagesParams) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	query := Query{Favorite: params.Favorite, CreatedAfter: params.CreatedAfter, CreatedBefore: params.CreatedBefore}
	if params.Q != nil {
		tokens, err := tokenize(*params.Q)
		if err != nil {
			writeError(w, r, err)
			return
		}
		query.Tokens = tokens
	}
	if params.Lifecycle != nil {
		value := string(*params.Lifecycle)
		query.Lifecycle = &value
	}
	if params.TagId != nil {
		query.TagIDs = append(query.TagIDs, (*params.TagId)...)
	}
	query.DetectedType = params.Type
	if params.Cursor != nil {
		cursor, err := DecodeCursor(*params.Cursor)
		if err != nil {
			writeError(w, r, err)
			return
		}
		query.Cursor = &cursor
	}
	if params.Limit != nil {
		query.Limit = *params.Limit
	}
	authentication, _ := auth.FromContext(r.Context())
	page, err := h.service.Search(r.Context(), authentication.User.ID, query)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]httpapi.MessageSummary, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, messages.SummaryDTO(item))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(httpapi.MessageList{Items: items, NextCursor: page.NextCursor})
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrQueryTooShort):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "SEARCH_QUERY_TOO_SHORT", "search term is too short")
	case errors.Is(err, ErrQueryTooLong):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "SEARCH_QUERY_TOO_LONG", "search query is too long")
	case errors.Is(err, ErrTooManyTokens):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "SEARCH_TOO_MANY_TOKENS", "search query has too many terms")
	case errors.Is(err, ErrCursorInvalid):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "SEARCH_CURSOR_INVALID", "search cursor is invalid")
	case errors.Is(err, ErrValidation):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
	default:
		auth.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}
