package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
	"github.com/messenger/backend/internal/workspace"
)

type fakeWS struct {
	createErr  error
	addErr     error
	addResult  *store.WorkspaceMember
	searchErr  error
	searchResp []store.WorkspaceMember
	searchQ    string
}

func (f *fakeWS) Create(_ context.Context, owner, name string) (*store.Workspace, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &store.Workspace{ID: "w1", Name: name, Slug: "w1", OwnerUserID: owner}, nil
}
func (f *fakeWS) ListForUser(context.Context, string) ([]store.WorkspaceWithRole, error) {
	return []store.WorkspaceWithRole{{ID: "w1", Name: "W", Slug: "w1", Role: "owner"}}, nil
}
func (f *fakeWS) Members(context.Context, string, string) ([]store.WorkspaceMember, error) {
	return []store.WorkspaceMember{{UserID: "u1", Username: "a", Email: "a@c", Role: "owner"}}, nil
}
func (f *fakeWS) AddMember(context.Context, string, string, string) (*store.WorkspaceMember, error) {
	if f.addErr != nil {
		return nil, f.addErr
	}
	return f.addResult, nil
}
func (f *fakeWS) SearchMembers(_ context.Context, _, _, q string) ([]store.WorkspaceMember, error) {
	f.searchQ = q
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.searchResp, nil
}
func (f *fakeWS) SetRole(context.Context, string, string, string, string) error { return nil }
func (f *fakeWS) RemoveMember(context.Context, string, string, string) error    { return nil }
func (f *fakeWS) Leave(context.Context, string, string) error                   { return nil }

func withSession(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), sessionCtxKey, sessionWithToken{Session: &session.Session{UserID: "caller"}})
	return r.WithContext(ctx)
}

func TestWorkspaceCreate(t *testing.T) {
	h := &workspaceHandlers{svc: &fakeWS{}}
	req := withSession(httptest.NewRequest("POST", "/workspaces", strings.NewReader(`{"name":"Acme"}`)))
	rec := httptest.NewRecorder()
	h.create(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var resp workspaceResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Role != "owner" || resp.ID != "w1" {
		t.Fatalf("resp %+v", resp)
	}
}

func TestWorkspaceAddMemberMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 200},
		{workspace.ErrNotMember, 404},
		{workspace.ErrForbidden, 403},
		{store.ErrNotFound, 404},
		{store.ErrConflict, 409},
	}
	for _, c := range cases {
		h := &workspaceHandlers{svc: &fakeWS{addErr: c.err, addResult: &store.WorkspaceMember{UserID: "bob", Role: "member"}}}
		req := withSession(httptest.NewRequest("POST", "/workspaces/w1/members", strings.NewReader(`{"email_or_username":"bob"}`)))
		req.SetPathValue("id", "w1")
		rec := httptest.NewRecorder()
		h.addMember(rec, req)
		if rec.Code != c.want {
			t.Fatalf("err %v → status %d, want %d", c.err, rec.Code, c.want)
		}
	}
}

func TestWorkspaceSearch(t *testing.T) {
	f := &fakeWS{searchResp: []store.WorkspaceMember{
		{UserID: "u1", Username: "alice", Email: "alice@corp", Role: "member"},
	}}
	h := &workspaceHandlers{svc: f}
	req := withSession(httptest.NewRequest("GET", "/workspaces/w1/members/search?q=al", nil))
	req.SetPathValue("id", "w1")
	rec := httptest.NewRecorder()
	h.search(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if f.searchQ != "al" {
		t.Fatalf("query passed through = %q, want %q", f.searchQ, "al")
	}
	var resp []wsMemberResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp) != 1 || resp[0].Username != "alice" || resp[0].UserID != "u1" {
		t.Fatalf("resp %+v", resp)
	}
}

func TestWorkspaceSearchNotMember(t *testing.T) {
	h := &workspaceHandlers{svc: &fakeWS{searchErr: workspace.ErrNotMember}}
	req := withSession(httptest.NewRequest("GET", "/workspaces/w1/members/search?q=al", nil))
	req.SetPathValue("id", "w1")
	rec := httptest.NewRecorder()
	h.search(rec, req)
	if rec.Code != 404 {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestWorkspaceCreateInvalid(t *testing.T) {
	h := &workspaceHandlers{svc: &fakeWS{createErr: workspace.ErrInvalid}}
	req := withSession(httptest.NewRequest("POST", "/workspaces", strings.NewReader(`{"name":""}`)))
	rec := httptest.NewRecorder()
	h.create(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	_ = errors.Is
}
