package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
)

type authHandlers struct {
	svc  *as.Service
	sess *session.Manager
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return false
	}
	return true
}

func decodeB64(w http.ResponseWriter, s string) ([]byte, bool) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid base64")
		return nil, false
	}
	return b, true
}

func b64enc(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func (h *authHandlers) registerStart(w http.ResponseWriter, r *http.Request) {
	var req registerStartReq
	if !decodeJSON(w, r, &req) {
		return
	}
	reqBytes, ok := decodeB64(w, req.OpaqueRegistrationRequest)
	if !ok {
		return
	}
	resp, err := h.svc.RegisterStart(r.Context(), req.Email, reqBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "opaque_error", "registration failed")
		return
	}
	writeJSON(w, http.StatusOK, registerStartResp{OpaqueRegistrationResponse: b64enc(resp)})
}

func (h *authHandlers) registerFinish(w http.ResponseWriter, r *http.Request) {
	var req registerFinishReq
	if !decodeJSON(w, r, &req) {
		return
	}
	rec, ok := decodeB64(w, req.OpaqueRegistrationRecord)
	if !ok {
		return
	}
	err := h.svc.RegisterFinish(r.Context(), req.Email, rec)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "account already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not store record")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *authHandlers) loginStart(w http.ResponseWriter, r *http.Request) {
	var req loginStartReq
	if !decodeJSON(w, r, &req) {
		return
	}
	ke1, ok := decodeB64(w, req.KE1)
	if !ok {
		return
	}
	loginID, ke2, err := h.svc.LoginStart(r.Context(), req.Email, ke1)
	if err != nil {
		writeError(w, http.StatusBadRequest, "opaque_error", "login failed")
		return
	}
	writeJSON(w, http.StatusOK, loginStartResp{LoginID: loginID, KE2: b64enc(ke2)})
}

func (h *authHandlers) loginFinish(w http.ResponseWriter, r *http.Request) {
	var req loginFinishReq
	if !decodeJSON(w, r, &req) {
		return
	}
	ke3, ok := decodeB64(w, req.KE3)
	if !ok {
		return
	}
	token, enroll, err := h.svc.LoginFinish(r.Context(), req.LoginID, ke3)
	if errors.Is(err, as.ErrAuthFailed) {
		writeError(w, http.StatusUnauthorized, "auth_failed", "invalid credentials")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "login error")
		return
	}
	writeJSON(w, http.StatusOK, loginFinishResp{SessionToken: token, DeviceEnrollRequired: enroll})
}

func (h *authHandlers) session(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	writeJSON(w, http.StatusOK, sessionResp{UserID: swt.Session.UserID, DeviceID: swt.Session.DeviceID})
}

func (h *authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	_ = h.sess.Revoke(r.Context(), swt.Token)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *authHandlers) logoutAll(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	_ = h.sess.RevokeAll(r.Context(), swt.Session.UserID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
