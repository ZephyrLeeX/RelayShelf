package recipients

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/users"
)

type Handler struct{ directory *users.DirectoryService }

func NewHandler(directory *users.DirectoryService) *Handler { return &Handler{directory: directory} }

func (h *Handler) ListRecipientUsers(w http.ResponseWriter, r *http.Request, params httpapi.ListRecipientUsersParams) {
	authn, ok := auth.FromContext(r.Context())
	if !ok {
		auth.WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	query := ""
	limit := 0
	if params.Query != nil {
		query = *params.Query
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	items, err := h.directory.ListRecipients(r.Context(), authn.User.ID, query, limit)
	if err != nil {
		if errors.Is(err, users.ErrInvalidList) {
			auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
		} else {
			auth.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		}
		return
	}
	out := make([]httpapi.RecipientUser, 0, len(items))
	for _, item := range items {
		out = append(out, httpapi.RecipientUser{Id: item.ID, Username: item.Username, DisplayName: item.DisplayName})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(httpapi.RecipientUserList{Items: out})
}
