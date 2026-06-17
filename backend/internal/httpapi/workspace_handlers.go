package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/messenger/backend/internal/store"
	"github.com/messenger/backend/internal/workspace"
)

// wsService is the workspace behaviour the HTTP layer needs (*workspace.Service satisfies it).
type wsService interface {
	Create(ctx context.Context, ownerUserID, name string) (*store.Workspace, error)
	ListForUser(ctx context.Context, userID string) ([]store.WorkspaceWithRole, error)
	Members(ctx context.Context, callerID, workspaceID string) ([]store.WorkspaceMember, error)
	SearchMembers(ctx context.Context, callerID, wsID, q string) ([]store.WorkspaceMember, error)
	AddMember(ctx context.Context, callerID, workspaceID, emailOrUsername string) (*store.WorkspaceMember, error)
	SetRole(ctx context.Context, callerID, workspaceID, targetUserID, role string) error
	RemoveMember(ctx context.Context, callerID, workspaceID, targetUserID string) error
	Leave(ctx context.Context, callerID, workspaceID string) error
}

type workspaceHandlers struct{ svc wsService }

func writeWorkspaceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspace.ErrNotMember):
		writeError(w, http.StatusNotFound, "not_found", "workspace not found")
	case errors.Is(err, workspace.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient role")
	case errors.Is(err, workspace.ErrInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", "invalid input")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "already a member")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "workspace error")
	}
}

func (h *workspaceHandlers) create(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	var req createWorkspaceReq
	if !decodeJSON(w, r, &req) {
		return
	}
	ws, err := h.svc.Create(r.Context(), swt.Session.UserID, req.Name)
	if err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspaceResp{ID: ws.ID, Name: ws.Name, Slug: ws.Slug, Role: store.RoleOwner})
}

func (h *workspaceHandlers) list(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	ls, err := h.svc.ListForUser(r.Context(), swt.Session.UserID)
	if err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	out := make([]workspaceResp, 0, len(ls))
	for _, x := range ls {
		out = append(out, workspaceResp{ID: x.ID, Name: x.Name, Slug: x.Slug, Role: x.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *workspaceHandlers) members(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	ms, err := h.svc.Members(r.Context(), swt.Session.UserID, r.PathValue("id"))
	if err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	out := make([]wsMemberResp, 0, len(ms))
	for _, m := range ms {
		out = append(out, wsMemberResp{UserID: m.UserID, Username: m.Username, Email: m.Email, Role: m.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *workspaceHandlers) search(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	ms, err := h.svc.SearchMembers(r.Context(), swt.Session.UserID, r.PathValue("id"), r.URL.Query().Get("q"))
	if err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	out := make([]wsMemberResp, 0, len(ms))
	for _, m := range ms {
		out = append(out, wsMemberResp{UserID: m.UserID, Username: m.Username, Email: m.Email, Role: m.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *workspaceHandlers) addMember(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	var req addWSMemberReq
	if !decodeJSON(w, r, &req) {
		return
	}
	m, err := h.svc.AddMember(r.Context(), swt.Session.UserID, r.PathValue("id"), req.EmailOrUsername)
	if err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, wsMemberResp{UserID: m.UserID, Username: m.Username, Email: m.Email, Role: m.Role})
}

func (h *workspaceHandlers) setRole(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	var req setWSRoleReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.SetRole(r.Context(), swt.Session.UserID, r.PathValue("id"), r.PathValue("user"), req.Role); err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *workspaceHandlers) removeMember(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	if err := h.svc.RemoveMember(r.Context(), swt.Session.UserID, r.PathValue("id"), r.PathValue("user")); err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *workspaceHandlers) leave(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	if err := h.svc.Leave(r.Context(), swt.Session.UserID, r.PathValue("id")); err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
