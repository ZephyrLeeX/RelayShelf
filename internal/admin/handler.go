package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
	"github.com/ZephyrLeeX/RelayShelf/internal/users"
	"github.com/google/uuid"
)

type Handler struct {
	users       *users.AdminService
	authService *auth.Service
	status      *StatusService
}

func NewHandler(users *users.AdminService, authService *auth.Service, status *StatusService) *Handler {
	return &Handler{users: users, authService: authService, status: status}
}

func (h *Handler) GetStorageStatus(w http.ResponseWriter, r *http.Request) {
	write(w, http.StatusOK, storageDTO(h.status.Storage(r.Context())))
}
func (h *Handler) GetAdminStatus(w http.ResponseWriter, r *http.Request) {
	write(w, http.StatusOK, statusDTO(h.status.Status(r.Context())))
}

func (h *Handler) ListAdminUsers(w http.ResponseWriter, r *http.Request, params httpapi.ListAdminUsersParams) {
	filter := users.ListFilter{}
	if params.Cursor != nil {
		decoded, err := users.DecodeCursor(*params.Cursor)
		if err != nil {
			listError(w, r, err)
			return
		}
		filter.Cursor = &decoded
	}
	if params.Limit != nil {
		filter.Limit = *params.Limit
	}
	page, err := h.users.List(r.Context(), filter)
	if err != nil {
		listError(w, r, err)
		return
	}
	out := make([]httpapi.AdminUser, 0, len(page.Items))
	for _, row := range page.Items {
		out = append(out, userDTO(row))
	}
	write(w, http.StatusOK, httpapi.AdminUserList{Items: out, NextCursor: page.NextCursor})
}

func (h *Handler) CreateAdminUser(w http.ResponseWriter, r *http.Request) {
	var body httpapi.CreateAdminUserRequest
	if !decode(w, r, &body) {
		return
	}
	user, err := h.users.Create(r.Context(), actor(r), body.Username, body.DisplayName, body.Password, body.IsAdmin)
	if err != nil {
		userError(w, r, err)
		return
	}
	write(w, http.StatusCreated, userDTO(user))
}

func (h *Handler) DisableAdminUser(w http.ResponseWriter, r *http.Request, userID httpapi.UserId) {
	if err := h.users.Disable(r.Context(), actor(r), uuid.UUID(userID)); err != nil {
		userError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ResetAdminUserPassword(w http.ResponseWriter, r *http.Request, userID httpapi.UserId) {
	var body httpapi.ResetAdminUserPasswordRequest
	if !decode(w, r, &body) {
		return
	}
	a, _ := auth.FromContext(r.Context())
	info, _ := auth.RequestInfo(r.Context())
	input := auth.LoginInput{ClientIP: info.ClientIP, UserAgent: r.UserAgent(), TraceID: httpx.TraceID(r)}
	if err := h.authService.ResetPasswordByAdmin(r.Context(), a.Authentication, uuid.UUID(userID), body.NewPassword, input); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			auth.WriteError(w, r, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
		} else if errors.Is(err, auth.ErrValidation) {
			auth.WriteError(w, r, http.StatusUnprocessableEntity, "USER_INVALID", "user data is invalid")
		} else if errors.Is(err, auth.ErrForbidden) {
			auth.WriteError(w, r, http.StatusForbidden, "ADMIN_REQUIRED", "administrator access required")
		} else {
			internalError(w, r)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteAdminUser(w http.ResponseWriter, r *http.Request, userID httpapi.UserId) {
	if err := h.users.Delete(r.Context(), actor(r), uuid.UUID(userID)); err != nil {
		userError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func actor(r *http.Request) audit.Actor {
	a, _ := auth.FromContext(r.Context())
	info, _ := auth.RequestInfo(r.Context())
	return audit.Actor{UserID: a.User.ID, DeviceID: a.Device.ID, SessionID: a.Session.ID, IP: info.ClientIP, UserAgent: r.UserAgent(), TraceID: httpx.TraceID(r)}
}
func userDTO(user users.User) httpapi.AdminUser {
	return httpapi.AdminUser{Id: user.ID, Username: user.Username, DisplayName: user.DisplayName, IsAdmin: user.IsAdmin, Status: httpapi.AdminUserStatus(user.Status), CreatedAt: user.CreatedAt.UTC(), UpdatedAt: user.UpdatedAt.UTC()}
}
func storageDTO(value StorageStatus) httpapi.StorageStatus {
	reasons := make([]httpapi.StorageStatusDegradedReasons, 0, len(value.DegradedReasons))
	for _, reason := range value.DegradedReasons {
		reasons = append(reasons, httpapi.StorageStatusDegradedReasons(reason))
	}
	return httpapi.StorageStatus{State: httpapi.HealthState(value.State), LogicalUsageBytes: value.LogicalUsageBytes, MaxStorageBytes: value.MaxStorageBytes, ThresholdState: httpapi.StorageThresholdState(value.ThresholdState), NasAvailableBytes: value.NASAvailableBytes, NasTotalBytes: value.NASTotalBytes, StagingUsageBytes: value.StagingUsageBytes, StagingAvailableBytes: value.StagingAvailableBytes, StagingTotalBytes: value.StagingTotalBytes, DegradedReasons: reasons}
}
func statusDTO(value AdminStatus) httpapi.AdminStatus {
	failed := make([]httpapi.FailedJob, 0, len(value.FailedJobs))
	for _, job := range value.FailedJobs {
		failed = append(failed, httpapi.FailedJob{Id: job.ID, Type: job.Type, SubjectType: job.SubjectType, Attempts: job.Attempts, ErrorCode: job.ErrorCode, UpdatedAt: job.UpdatedAt.UTC()})
	}
	return httpapi.AdminStatus{State: httpapi.HealthState(value.State), DatabaseState: httpapi.HealthState(value.DatabaseState), Build: httpapi.BuildInfo{Version: value.Build.Version, GitCommit: value.Build.GitCommit, BuildTime: value.Build.BuildTime}, Migration: httpapi.MigrationStatus{CurrentVersion: value.Migration.Current, LatestVersion: value.Migration.Latest, Compatible: value.Migration.Compatible}, FailedJobs: failed, Storage: storageDTO(value.Storage), Security: httpapi.AdminSecurityStatus{ActiveAdmins: value.Security.ActiveAdmins, ActiveAdminsWithoutTOTP: value.Security.ActiveAdminsWithoutTOTP, AdminTotpSatisfied: value.Security.AdminTotpSatisfied}}
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
		return false
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
		return false
	}
	return true
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func internalError(w http.ResponseWriter, r *http.Request) {
	auth.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
}
func userError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, users.ErrNotFound):
		auth.WriteError(w, r, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
	case errors.Is(err, users.ErrUsernameTaken):
		auth.WriteError(w, r, http.StatusConflict, "USERNAME_ALREADY_EXISTS", "username already exists")
	case errors.Is(err, users.ErrInvalidUsername), errors.Is(err, users.ErrInvalidPassword):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "USER_INVALID", "user data is invalid")
	default:
		internalError(w, r)
	}
}

func listError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, users.ErrCursorInvalid):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "USER_LIST_CURSOR_INVALID", "user list cursor is invalid")
	case errors.Is(err, users.ErrInvalidList):
		auth.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
	default:
		internalError(w, r)
	}
}
