package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
	"github.com/google/uuid"
)

type Handler struct {
	service *Service
	csrf    *CSRF
	cookies CookiePolicy
}

func NewHandler(service *Service, csrf *CSRF, cookies CookiePolicy) *Handler {
	return &Handler{service: service, csrf: csrf, cookies: cookies}
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, httpapi.Error{Code: code, Message: message, TraceId: httpx.TraceID(r), Details: nil})
}
func mapError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrRateLimited):
		WriteError(w, r, http.StatusTooManyRequests, "AUTH_RATE_LIMITED", "too many attempts")
	case errors.Is(err, ErrInvalidCredentials):
		WriteError(w, r, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "invalid username or password")
	case errors.Is(err, ErrTOTPCodeInvalid):
		WriteError(w, r, http.StatusUnauthorized, "TOTP_INVALID", "invalid totp code")
	case errors.Is(err, ErrTOTPChallengeExpired):
		WriteError(w, r, http.StatusGone, "TOTP_CHALLENGE_EXPIRED", "totp challenge expired")
	case errors.Is(err, ErrTOTPAlreadyEnabled):
		WriteError(w, r, http.StatusConflict, "TOTP_ALREADY_ENABLED", "totp is already enabled")
	case errors.Is(err, ErrTOTPNotEnabled):
		WriteError(w, r, http.StatusNotFound, "TOTP_NOT_ENROLLED", "totp is not enrolled")
	case errors.Is(err, ErrForbidden):
		WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "forbidden")
	case errors.Is(err, ErrNotFound):
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, ErrValidation):
		WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "invalid request")
	default:
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}
func requestInput(r *http.Request) LoginInput {
	info, _ := RequestInfo(r.Context())
	return LoginInput{ClientIP: info.ClientIP, UserAgent: r.UserAgent(), TraceID: httpx.TraceID(r)}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var body httpapi.LoginRequest
	if !decode(w, r, &body) {
		return
	}
	input := requestInput(r)
	input.Username, input.Password = body.Username, body.Password
	if body.DeviceName != nil {
		input.DeviceName = *body.DeviceName
	}
	if body.DeviceId != nil {
		id := uuid.UUID(*body.DeviceId)
		input.DeviceID = &id
	}
	result, err := h.service.Login(r.Context(), input)
	if err != nil {
		mapError(w, r, err)
		return
	}
	if result.Challenge != nil {
		// A TOTP-gated login never receives a session cookie here; the
		// challenge is the only thing the browser learns.
		writeJSON(w, http.StatusAccepted, httpapi.TOTPLoginChallenge{ChallengeToken: result.Challenge.Token, ExpiresAt: result.Challenge.ExpiresAt.UTC()})
		return
	}
	h.cookies.Set(w, result.RawToken, result.Session.AbsoluteExpiresAt)
	writeJSON(w, http.StatusOK, h.bootstrap(result.Authentication))
}

func (h *Handler) CompleteLoginTOTP(w http.ResponseWriter, r *http.Request) {
	var body httpapi.TOTPChallengeRequest
	if !decode(w, r, &body) {
		return
	}
	result, err := h.service.CompleteLoginTOTP(r.Context(), body.ChallengeToken, body.Code, requestInput(r))
	if err != nil {
		mapError(w, r, err)
		return
	}
	h.cookies.Set(w, result.RawToken, result.Session.AbsoluteExpiresAt)
	writeJSON(w, http.StatusOK, h.bootstrap(result.Authentication))
}

func (h *Handler) GetTOTPStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := FromContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	enabled, err := h.service.TOTPStatus(r.Context(), actor.Authentication)
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, httpapi.TOTPStatus{Enabled: enabled})
}

func (h *Handler) StartTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	actor, ok := FromContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	enrollment, err := h.service.StartTOTPEnrollment(r.Context(), actor.Authentication, requestInput(r))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, httpapi.TOTPEnrollmentPending{Secret: enrollment.Secret, OtpauthUrl: enrollment.OtpauthURL, Digits: httpapi.TOTPEnrollmentPendingDigits(enrollment.Digits), PeriodSeconds: httpapi.TOTPEnrollmentPendingPeriodSeconds(enrollment.PeriodSeconds), Algorithm: httpapi.TOTPEnrollmentPendingAlgorithm(enrollment.Algorithm)})
}

func (h *Handler) ConfirmTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	actor, ok := FromContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	var body httpapi.TOTPCodeRequest
	if !decode(w, r, &body) {
		return
	}
	if err := h.service.ConfirmTOTPEnrollment(r.Context(), actor.Authentication, body.Code, requestInput(r)); err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, httpapi.TOTPStatus{Enabled: true})
}

func (h *Handler) DisableTOTP(w http.ResponseWriter, r *http.Request) {
	actor, ok := FromContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	var body httpapi.TOTPCodeRequest
	if !decode(w, r, &body) {
		return
	}
	if err := h.service.DisableTOTP(r.Context(), actor.Authentication, body.Code, requestInput(r)); err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, httpapi.TOTPStatus{Enabled: false})
}
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	actor, ok := FromContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	if err := h.service.Logout(r.Context(), actor.Authentication, requestInput(r)); err != nil {
		mapError(w, r, err)
		return
	}
	h.cookies.Clear(w)
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) GetAuthSession(w http.ResponseWriter, r *http.Request) {
	actor, ok := FromContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, h.bootstrap(actor.Authentication))
}
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	actor, ok := FromContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}
	var body httpapi.PasswordChangeRequest
	if !decode(w, r, &body) {
		return
	}
	if err := h.service.ChangePassword(r.Context(), actor.Authentication, body.CurrentPassword, body.NewPassword, requestInput(r)); err != nil {
		mapError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	actor, _ := FromContext(r.Context())
	rows, err := h.service.ListSessions(r.Context(), actor.Authentication)
	if err != nil {
		mapError(w, r, err)
		return
	}
	out := make([]httpapi.Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, apiSession(row, row.ID == actor.Session.ID))
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *Handler) RevokeSession(w http.ResponseWriter, r *http.Request, sessionID httpapi.SessionId) {
	actor, _ := FromContext(r.Context())
	id := uuid.UUID(sessionID)
	revoked, err := h.service.Revoke(r.Context(), actor.Authentication, id, requestInput(r))
	if err != nil {
		mapError(w, r, err)
		return
	}
	if !revoked {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
		return
	}
	if id == actor.Session.ID {
		h.cookies.Clear(w)
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	actor, _ := FromContext(r.Context())
	rows, err := h.service.ListDevices(r.Context(), actor.Authentication)
	if err != nil {
		mapError(w, r, err)
		return
	}
	out := make([]httpapi.Device, 0, len(rows))
	for _, row := range rows {
		out = append(out, apiDevice(row))
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *Handler) RenameDevice(w http.ResponseWriter, r *http.Request, deviceID httpapi.DeviceId) {
	actor, _ := FromContext(r.Context())
	var body httpapi.RenameDeviceRequest
	if !decode(w, r, &body) {
		return
	}
	row, err := h.service.RenameDevice(r.Context(), actor.Authentication, uuid.UUID(deviceID), body.Name)
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apiDevice(row))
}

func (h *Handler) bootstrap(a Authentication) httpapi.AuthBootstrap {
	return httpapi.AuthBootstrap{User: httpapi.User{Id: a.User.ID, Username: a.User.Username, DisplayName: a.User.DisplayName, IsAdmin: a.User.IsAdmin}, Device: apiDevice(a.Device), Session: apiSession(a.Session, true), CsrfToken: h.csrf.Token(a.Session.ID)}
}
func apiDevice(d Device) httpapi.Device {
	return httpapi.Device{Id: d.ID, Name: d.Name, UserAgent: d.UserAgent, FirstSeenAt: d.FirstSeenAt.UTC(), LastSeenAt: d.LastSeenAt.UTC()}
}
func apiSession(s Session, current bool) httpapi.Session {
	var ip *string
	if s.LastIP != "" {
		value := s.LastIP
		ip = &value
	}
	return httpapi.Session{Id: s.ID, DeviceId: s.DeviceID, CreatedAt: s.CreatedAt.UTC(), LastSeenAt: s.LastSeenAt.UTC(), ExpiresAt: s.ExpiresAt.UTC(), AbsoluteExpiresAt: s.AbsoluteExpiresAt.UTC(), LastIp: ip, Current: current}
}
