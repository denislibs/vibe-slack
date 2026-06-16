package httpapi

import (
	"errors"
	"net/http"

	"github.com/messenger/backend/internal/devices"
	"github.com/messenger/backend/internal/keypackages"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
)

type deviceHandlers struct {
	svc  *devices.Service
	kp   *keypackages.Service
	sess *session.Manager
}

func (h *deviceHandlers) enroll(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	var req enrollDeviceReq
	if !decodeJSON(w, r, &req) {
		return
	}
	pub, ok := decodeB64(w, req.SigningPublicKey)
	if !ok {
		return
	}
	dev, err := h.svc.Enroll(r.Context(), swt.Session.UserID, pub, req.Label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "enroll failed")
		return
	}
	if len(req.InitialKeyPackages) > 0 {
		pkgs := make([][]byte, 0, len(req.InitialKeyPackages))
		for _, s := range req.InitialKeyPackages {
			b, ok := decodeB64(w, s)
			if !ok {
				return
			}
			pkgs = append(pkgs, b)
		}
		if err := h.kp.Upload(r.Context(), dev.ID, pkgs); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "key package upload failed")
			return
		}
	}
	// Bind the new device to the caller's session (satisfies device_enroll_required).
	if err := h.sess.BindDevice(r.Context(), swt.Token, dev.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "session bind failed")
		return
	}
	writeJSON(w, http.StatusOK, enrollDeviceResp{DeviceID: dev.ID})
}

func (h *deviceHandlers) list(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	devs, err := h.svc.List(r.Context(), swt.Session.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list failed")
		return
	}
	out := make([]deviceItem, 0, len(devs))
	for _, d := range devs {
		out = append(out, deviceItem{DeviceID: d.ID, Label: d.Label, Status: d.Status})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *deviceHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	id := r.PathValue("id")
	if err := h.svc.Revoke(r.Context(), swt.Session.UserID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "device not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "revoke failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
