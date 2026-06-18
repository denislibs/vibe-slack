package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/messenger/backend/internal/conversations"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
)

type fakeConv struct {
	createErr error
	addErr    error
	created   *store.Conversation
	dmCreated bool
}

func (f *fakeConv) CreateChannel(_ context.Context, caller, ws, vis, name string) (*store.Conversation, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &store.Conversation{GroupID: "g1", WorkspaceID: ws, Type: "channel", Visibility: vis, Name: name, CreatedBy: caller}, nil
}
func (f *fakeConv) CreateDM(_ context.Context, caller, ws, q string) (*store.Conversation, bool, error) {
	if f.createErr != nil {
		return nil, false, f.createErr
	}
	return &store.Conversation{GroupID: "dm1", WorkspaceID: ws, Type: "dm", Visibility: "private", CreatedBy: caller}, true, nil
}
func (f *fakeConv) List(_ context.Context, caller, ws string) ([]store.Conversation, error) {
	return []store.Conversation{{GroupID: "g1", WorkspaceID: ws, Type: "channel", Visibility: "public", Name: "general"}}, nil
}
func (f *fakeConv) Get(_ context.Context, caller, g string) (*store.Conversation, error) {
	return &store.Conversation{GroupID: g, Type: "channel", Visibility: "public", Name: "general"}, nil
}
func (f *fakeConv) Members(_ context.Context, caller, g string) ([]string, error) {
	return []string{"u1", "u2"}, nil
}
func (f *fakeConv) Join(_ context.Context, caller, g string) error { return nil }
func (f *fakeConv) AddUser(_ context.Context, caller, g, q string) (*store.User, error) {
	if f.addErr != nil {
		return nil, f.addErr
	}
	return &store.User{ID: "bob", Username: "bob", Email: "b@c"}, nil
}
func (f *fakeConv) RemoveUser(_ context.Context, caller, g, target string) error { return nil }

type rosterAdd struct {
	group, device string
	seq           int64
}

type fakeRoster struct {
	added []rosterAdd
}

func (f *fakeRoster) AddMember(_ context.Context, group, device string, seq int64) error {
	f.added = append(f.added, rosterAdd{group, device, seq})
	return nil
}
func (f *fakeRoster) RemoveMember(_ context.Context, group, device string) error { return nil }

func withSessionDevice(r *http.Request, device string) *http.Request {
	ctx := context.WithValue(r.Context(), sessionCtxKey, sessionWithToken{Session: &session.Session{UserID: "caller", DeviceID: device}})
	return r.WithContext(ctx)
}

func TestConvCreateAddsCreatorDeviceToRoster(t *testing.T) {
	fr := &fakeRoster{}
	h := &conversationHandlers{svc: &fakeConv{}, roster: fr}
	req := withSessionDevice(httptest.NewRequest("POST", "/workspaces/w1/conversations",
		strings.NewReader(`{"type":"channel","visibility":"public","name":"general"}`)), "dev-123")
	req.SetPathValue("wsId", "w1")
	rec := httptest.NewRecorder()
	h.create(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if len(fr.added) != 1 || fr.added[0].group != "g1" || fr.added[0].device != "dev-123" || fr.added[0].seq != 0 {
		t.Fatalf("roster add = %+v, want one add of (g1, dev-123, 0)", fr.added)
	}
}

func TestConvCreateDMAddsCreatorDeviceToRoster(t *testing.T) {
	fr := &fakeRoster{}
	h := &conversationHandlers{svc: &fakeConv{}, roster: fr}
	req := withSessionDevice(httptest.NewRequest("POST", "/workspaces/w1/conversations",
		strings.NewReader(`{"type":"dm","email_or_username":"bob"}`)), "dev-123")
	req.SetPathValue("wsId", "w1")
	rec := httptest.NewRecorder()
	h.create(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if len(fr.added) != 1 || fr.added[0].group != "dm1" || fr.added[0].device != "dev-123" || fr.added[0].seq != 0 {
		t.Fatalf("roster add = %+v, want one add of (dm1, dev-123, 0)", fr.added)
	}
}

func TestConvCreateChannel(t *testing.T) {
	h := &conversationHandlers{svc: &fakeConv{}}
	req := withSession(httptest.NewRequest("POST", "/workspaces/w1/conversations",
		strings.NewReader(`{"type":"channel","visibility":"public","name":"general"}`)))
	req.SetPathValue("wsId", "w1")
	rec := httptest.NewRecorder()
	h.create(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var resp convResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.GroupID != "g1" || resp.Type != "channel" {
		t.Fatalf("resp %+v", resp)
	}
}

func TestConvCreateDMRoutes(t *testing.T) {
	h := &conversationHandlers{svc: &fakeConv{}}
	req := withSession(httptest.NewRequest("POST", "/workspaces/w1/conversations",
		strings.NewReader(`{"type":"dm","email_or_username":"bob"}`)))
	req.SetPathValue("wsId", "w1")
	rec := httptest.NewRecorder()
	h.create(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var resp convResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Type != "dm" || resp.GroupID != "dm1" {
		t.Fatalf("dm resp %+v", resp)
	}
}

func TestConvAddUserMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 200},
		{conversations.ErrNotMember, 404},
		{conversations.ErrForbidden, 403},
		{conversations.ErrInvalid, 400},
		{store.ErrConflict, 409},
		{store.ErrNotFound, 404},
	}
	for _, c := range cases {
		h := &conversationHandlers{svc: &fakeConv{addErr: c.err}}
		req := withSession(httptest.NewRequest("POST", "/conversations/g1/users", strings.NewReader(`{"email_or_username":"bob"}`)))
		req.SetPathValue("group", "g1")
		rec := httptest.NewRecorder()
		h.addUser(rec, req)
		if rec.Code != c.want {
			t.Fatalf("err %v → %d, want %d", c.err, rec.Code, c.want)
		}
	}
}
