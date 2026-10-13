package search

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Saved searches persist conditions only; execution uses the existing search API.
type SavedRepository struct{ db *pgxpool.Pool }

func (h *Handler) SetSavedRepository(db *pgxpool.Pool) { h.saved = &SavedRepository{db} }

func validConditions(c httpapi.SearchConditions) bool {
	if _, err := tokenize(c.Q); c.Q != "" && err != nil {
		return false
	}
	if c.Lifecycle != "" && c.Lifecycle != "TEMPORARY" && c.Lifecycle != "PERMANENT" {
		return false
	}
	if c.Type != "" && c.Type != "TEXT" && c.Type != "MARKDOWN" && c.Type != "CODE" {
		return false
	}
	if c.Time != "all" && c.Time != "24h" && c.Time != "7d" && c.Time != "30d" && c.Time != "custom" {
		return false
	}
	if c.Timezone != "UTC" && c.Timezone != "Asia/Shanghai" {
		return false
	}
	if len(c.TagIds) > 100 {
		return false
	}
	for _, tag := range c.TagIds {
		if tag == uuid.Nil {
			return false
		}
	}
	if c.Time == "custom" {
		location := time.UTC
		if c.Timezone == "Asia/Shanghai" {
			location = time.FixedZone("UTC+8", 8*3600)
		}
		start, e1 := time.ParseInLocation("2006-01-02T15:04", c.From, location)
		end, e2 := time.ParseInLocation("2006-01-02T15:04", c.To, location)
		if e1 != nil || e2 != nil || !start.Before(end) {
			return false
		}
	} else if c.From != "" || c.To != "" {
		return false
	}
	return true
}
func (s *SavedRepository) reason(ctx context.Context, owner uuid.UUID, c httpapi.SearchConditions) (string, error) {
	if !validConditions(c) {
		return "保存的条件已失效，请修正后明确更新当前视图条件。", nil
	}
	for _, tag := range c.TagIds {
		var exists bool
		if err := s.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tags WHERE id=$1 AND user_id=$2)", tag, owner).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return "保存的标签已删除或不可用，请重新选择标签并更新当前视图条件。", nil
		}
	}
	return "", nil
}
func savedOwner(r *http.Request) uuid.UUID { a, _ := auth.FromContext(r.Context()); return a.User.ID }
func savedJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func savedError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		auth.WriteError(w, r, 404, "NOT_FOUND", "resource not found")
	} else {
		writeError(w, r, err)
	}
}
func (s *SavedRepository) list(ctx context.Context, owner uuid.UUID) ([]httpapi.SavedSearch, error) {
	rows, err := s.db.Query(ctx, "SELECT id,name,conditions,updated_at FROM saved_searches WHERE owner_id=$1 ORDER BY updated_at DESC,id", owner)
	if err != nil {
		return nil, err
	}
	out := []httpapi.SavedSearch{}
	for rows.Next() {
		var item httpapi.SavedSearch
		var raw []byte
		if err := rows.Scan(&item.Id, &item.Name, &raw, &item.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Conditions); err != nil {
			item.InvalidReason = "保存的条件无法读取，请删除或更新视图。"
		}
		out = append(out, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].InvalidReason == "" {
			reason, err := s.reason(ctx, owner, out[i].Conditions)
			if err != nil {
				return nil, err
			}
			out[i].InvalidReason = reason
		}
	}
	return out, nil
}
func (h *Handler) ListSavedSearches(w http.ResponseWriter, r *http.Request) {
	items, err := h.saved.list(r.Context(), savedOwner(r))
	if err != nil {
		savedError(w, r, err)
		return
	}
	savedJSON(w, 200, items)
}
func (h *Handler) save(w http.ResponseWriter, r *http.Request, id uuid.UUID, create bool) {
	var request httpapi.SavedSearchRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, r, ErrValidation)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, r, ErrValidation)
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if !utf8.ValidString(request.Name) || utf8.RuneCountInString(request.Name) < 1 || utf8.RuneCountInString(request.Name) > 100 || !validConditions(request.Conditions) {
		writeError(w, r, ErrValidation)
		return
	}
	owner := savedOwner(r)
	if !create {
		var exists bool
		err := h.saved.db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM saved_searches WHERE id=$1 AND owner_id=$2)", id, owner).Scan(&exists)
		if err != nil {
			savedError(w, r, err)
			return
		}
		if !exists {
			savedError(w, r, pgx.ErrNoRows)
			return
		}
	}
	reason, err := h.saved.reason(r.Context(), owner, request.Conditions)
	if err != nil {
		savedError(w, r, err)
		return
	}
	// Renaming an invalid view may preserve its exact conditions; changing them must repair it.
	if reason != "" {
		var old []byte
		if create || h.saved.db.QueryRow(r.Context(), "SELECT conditions FROM saved_searches WHERE id=$1 AND owner_id=$2", id, owner).Scan(&old) != nil {
			auth.WriteError(w, r, 422, "SAVED_SEARCH_INVALID", reason)
			return
		}
		var previous httpapi.SearchConditions
		_ = json.Unmarshal(old, &previous)
		a, _ := json.Marshal(previous)
		b, _ := json.Marshal(request.Conditions)
		if string(a) != string(b) {
			auth.WriteError(w, r, 422, "SAVED_SEARCH_INVALID", reason)
			return
		}
	}
	raw, _ := json.Marshal(request.Conditions)
	item := httpapi.SavedSearch{Name: request.Name, Conditions: request.Conditions, Id: id, InvalidReason: reason}
	query := "UPDATE saved_searches SET name=$3,conditions=$4,updated_at=now() WHERE id=$1 AND owner_id=$2 RETURNING updated_at"
	status := 200
	if create {
		query = "INSERT INTO saved_searches(id,owner_id,name,conditions) VALUES($1,$2,$3,$4) RETURNING updated_at"
		status = 201
	}
	if err := h.saved.db.QueryRow(r.Context(), query, id, owner, item.Name, raw).Scan(&item.UpdatedAt); err != nil {
		savedError(w, r, err)
		return
	}
	savedJSON(w, status, item)
}
func (h *Handler) CreateSavedSearch(w http.ResponseWriter, r *http.Request) {
	h.save(w, r, uuid.Must(uuid.NewV7()), true)
}
func (h *Handler) UpdateSavedSearch(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	h.save(w, r, id, false)
}
func (h *Handler) DeleteSavedSearch(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	result, err := h.saved.db.Exec(r.Context(), "DELETE FROM saved_searches WHERE id=$1 AND owner_id=$2", id, savedOwner(r))
	if err != nil {
		savedError(w, r, err)
		return
	}
	if result.RowsAffected() == 0 {
		savedError(w, r, pgx.ErrNoRows)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(204)
}
