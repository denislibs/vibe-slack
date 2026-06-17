package httpapi

import (
	"context"
	"net/http"
)

// rosterStore is the subset of *store.RosterRepo the handlers need.
type rosterStore interface {
	AddMember(ctx context.Context, groupID, deviceID string, joinSeq int64) error
	RemoveMember(ctx context.Context, groupID, deviceID string) error
}

// convMembership reports whether a user is a member of a conversation. Satisfied
// by *conversations.Service.
type convMembership interface {
	IsMember(ctx context.Context, groupID, userID string) (bool, error)
}

type rosterHandlers struct {
	roster  rosterStore
	members convMembership
}

// addMember adds a device to a conversation's routing roster. join_seq is the
// last message seq that existed when the device joined; the device receives only
// messages with seq > join_seq (see delivery service). The caller (committer)
// supplies join_seq = current max seq at join time. The roster API stores
// whatever join_seq the caller passes verbatim.
//
// Only a user-member of the conversation may mutate its roster. Non-members get
// 404 (not 403) so conversation existence is not leaked.
func (h *rosterHandlers) addMember(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("group")
	var req addMemberReq
	if !decodeJSON(w, r, &req) {
		return
	}
	swt := sessionFrom(r.Context())
	ok, err := h.members.IsMember(r.Context(), groupID, swt.Session.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "membership check failed")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "conversation not found")
		return
	}
	if err := h.roster.AddMember(r.Context(), groupID, req.DeviceID, req.JoinSeq); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "add member failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *rosterHandlers) removeMember(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("group")
	deviceID := r.PathValue("device")
	swt := sessionFrom(r.Context())
	ok, err := h.members.IsMember(r.Context(), groupID, swt.Session.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "membership check failed")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "conversation not found")
		return
	}
	if err := h.roster.RemoveMember(r.Context(), groupID, deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "remove member failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
