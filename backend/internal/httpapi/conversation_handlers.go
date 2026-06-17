package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/messenger/backend/internal/conversations"
	"github.com/messenger/backend/internal/store"
)

type convService interface {
	CreateChannel(ctx context.Context, callerID, wsID, visibility, name string) (*store.Conversation, error)
	CreateDM(ctx context.Context, callerID, wsID, emailOrUsername string) (*store.Conversation, bool, error)
	List(ctx context.Context, callerID, wsID string) ([]store.Conversation, error)
	Get(ctx context.Context, callerID, groupID string) (*store.Conversation, error)
	Members(ctx context.Context, callerID, groupID string) ([]string, error)
	Join(ctx context.Context, callerID, groupID string) error
	AddUser(ctx context.Context, callerID, groupID, emailOrUsername string) (*store.User, error)
	RemoveUser(ctx context.Context, callerID, groupID, targetUserID string) error
}

type conversationHandlers struct{ svc convService }

func writeConvErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, conversations.ErrNotMember):
		writeError(w, http.StatusNotFound, "not_found", "conversation not found")
	case errors.Is(err, conversations.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "not allowed")
	case errors.Is(err, conversations.ErrInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", "invalid input")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "already a member")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "conversation error")
	}
}

func conv2resp(c *store.Conversation) convResp {
	return convResp{GroupID: c.GroupID, Type: c.Type, Visibility: c.Visibility, Name: c.Name}
}

func (h *conversationHandlers) create(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	wsID := r.PathValue("wsId")
	var req createConvReq
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Type {
	case "dm":
		c, _, err := h.svc.CreateDM(r.Context(), swt.Session.UserID, wsID, req.EmailOrUsername)
		if err != nil {
			writeConvErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, conv2resp(c))
	case "channel":
		c, err := h.svc.CreateChannel(r.Context(), swt.Session.UserID, wsID, req.Visibility, req.Name)
		if err != nil {
			writeConvErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, conv2resp(c))
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "type must be dm or channel")
	}
}

func (h *conversationHandlers) list(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	cs, err := h.svc.List(r.Context(), swt.Session.UserID, r.PathValue("wsId"))
	if err != nil {
		writeConvErr(w, err)
		return
	}
	out := make([]convResp, 0, len(cs))
	for i := range cs {
		out = append(out, conv2resp(&cs[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *conversationHandlers) get(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	g := r.PathValue("group")
	c, err := h.svc.Get(r.Context(), swt.Session.UserID, g)
	if err != nil {
		writeConvErr(w, err)
		return
	}
	members, err := h.svc.Members(r.Context(), swt.Session.UserID, g)
	if err != nil {
		writeConvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group_id": c.GroupID, "type": c.Type, "visibility": c.Visibility, "name": c.Name, "members": members,
	})
}

func (h *conversationHandlers) join(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	if err := h.svc.Join(r.Context(), swt.Session.UserID, r.PathValue("group")); err != nil {
		writeConvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *conversationHandlers) addUser(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	var req addConvUserReq
	if !decodeJSON(w, r, &req) {
		return
	}
	u, err := h.svc.AddUser(r.Context(), swt.Session.UserID, r.PathValue("group"), req.EmailOrUsername)
	if err != nil {
		writeConvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, wsMemberResp{UserID: u.ID, Username: u.Username, Email: u.Email, Role: "member"})
}

func (h *conversationHandlers) removeUser(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	if err := h.svc.RemoveUser(r.Context(), swt.Session.UserID, r.PathValue("group"), r.PathValue("userId")); err != nil {
		writeConvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
