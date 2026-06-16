package httpapi

import (
	"net/http"

	"github.com/messenger/backend/internal/keypackages"
)

type keypackageHandlers struct {
	svc *keypackages.Service
}

func (h *keypackageHandlers) upload(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	var req uploadKeyPackagesReq
	if !decodeJSON(w, r, &req) {
		return
	}
	pkgs := make([][]byte, 0, len(req.KeyPackages))
	for _, s := range req.KeyPackages {
		b, ok := decodeB64(w, s)
		if !ok {
			return
		}
		pkgs = append(pkgs, b)
	}
	if err := h.svc.Upload(r.Context(), swt.Session.DeviceID, pkgs); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "upload failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *keypackageHandlers) count(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	n, err := h.svc.Count(r.Context(), swt.Session.DeviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "count failed")
		return
	}
	writeJSON(w, http.StatusOK, countResp{Available: n})
}

func (h *keypackageHandlers) consume(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")
	pkg, isLast, err := h.svc.Consume(r.Context(), deviceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no_keypackage", "no key package available")
		return
	}
	writeJSON(w, http.StatusOK, keyPackageResp{KeyPackage: b64enc(pkg), IsLastResort: isLast})
}
