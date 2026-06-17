package httpapi

import (
	"net/http"

	"github.com/messenger/backend/internal/store"
)

type rosterHandlers struct {
	roster *store.RosterRepo
}

// addMember adds a device to a conversation's routing roster. join_seq is the
// last message seq that existed when the device joined; the device receives only
// messages with seq > join_seq (see delivery service). The caller (committer)
// supplies join_seq = current max seq at join time. The roster API stores
// whatever join_seq the caller passes verbatim.
func (h *rosterHandlers) addMember(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("group")
	var req addMemberReq
	if !decodeJSON(w, r, &req) {
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
	if err := h.roster.RemoveMember(r.Context(), groupID, deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "remove member failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
