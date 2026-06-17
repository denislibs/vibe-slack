package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/messenger/backend/internal/store"
)

type giConvService interface {
	IsMember(ctx context.Context, groupID, userID string) (bool, error)
	Get(ctx context.Context, callerID, groupID string) (*store.Conversation, error)
}
type giStore interface {
	SetGroupInfo(ctx context.Context, groupID string, info []byte) error
	GroupInfo(ctx context.Context, groupID string) ([]byte, error)
}
type complianceKP interface {
	Consume(ctx context.Context, deviceID string) ([]byte, bool, error)
}

type groupInfoHandlers struct {
	conv               giConvService
	store              giStore
	compliance         complianceKP
	complianceDeviceID string
}

func (h *groupInfoHandlers) putGroupInfo(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	group := r.PathValue("group")
	ok, err := h.conv.IsMember(r.Context(), group, swt.Session.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "membership check failed")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "conversation not found")
		return
	}
	var req putGroupInfoReq
	if !decodeJSON(w, r, &req) {
		return
	}
	info, ok := decodeB64(w, req.GroupInfo)
	if !ok {
		return
	}
	if err := h.store.SetGroupInfo(r.Context(), group, info); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "conversation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "set group info failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *groupInfoHandlers) getGroupInfo(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	group := r.PathValue("group")
	if _, err := h.conv.Get(r.Context(), swt.Session.UserID, group); err != nil {
		writeConvErr(w, err)
		return
	}
	info, err := h.store.GroupInfo(r.Context(), group)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_published", "group info not published")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "get group info failed")
		return
	}
	writeJSON(w, http.StatusOK, groupInfoResp{GroupInfo: b64enc(info)})
}

func (h *groupInfoHandlers) complianceKeyPackage(w http.ResponseWriter, r *http.Request) {
	if h.complianceDeviceID == "" {
		writeError(w, http.StatusNotFound, "compliance_not_configured", "compliance device not configured")
		return
	}
	kp, _, err := h.compliance.Consume(r.Context(), h.complianceDeviceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no_keypackage", "no key package available")
		return
	}
	writeJSON(w, http.StatusOK, complianceKPResp{KeyPackage: b64enc(kp)})
}
