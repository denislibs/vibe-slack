package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/messenger/backend/internal/store"
)

type fakeKMUsers struct {
	user *store.User
	err  error
}

func (f *fakeKMUsers) FindByEmailOrUsername(context.Context, string) (*store.User, error) {
	return f.user, f.err
}

type fakeKMRoles struct {
	// errFor maps a userID to the error RoleOf should return for it.
	errFor map[string]error
}

func (f *fakeKMRoles) RoleOf(_ context.Context, _ string, userID string) (string, error) {
	if err := f.errFor[userID]; err != nil {
		return "", err
	}
	return store.RoleMember, nil
}

type fakeKMDevices struct {
	devs []store.DeviceKey
	err  error
}

func (f *fakeKMDevices) ActiveDevicesWithKeys(context.Context, string) ([]store.DeviceKey, error) {
	return f.devs, f.err
}

type fakeKMKeyPackages struct {
	byDevice map[string][]byte
	err      error
}

func (f *fakeKMKeyPackages) Consume(_ context.Context, deviceID string) ([]byte, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	kp, ok := f.byDevice[deviceID]
	if !ok {
		return nil, false, store.ErrNotFound
	}
	return kp, false, nil
}

func TestKeyMaterialCallerNotInWorkspace(t *testing.T) {
	h := &keyMaterialHandlers{
		users:   &fakeKMUsers{user: &store.User{ID: "target"}},
		roles:   &fakeKMRoles{errFor: map[string]error{"caller": store.ErrNotFound}},
		devices: &fakeKMDevices{},
		keyPkgs: &fakeKMKeyPackages{},
	}
	req := withSession(httptest.NewRequest("GET", "/workspaces/w1/users/bob/key-material", nil))
	req.SetPathValue("wsId", "w1")
	req.SetPathValue("identity", "bob")
	rec := httptest.NewRecorder()
	h.get(rec, req)
	if rec.Code != 404 {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestKeyMaterialTargetNotFound(t *testing.T) {
	h := &keyMaterialHandlers{
		users:   &fakeKMUsers{err: store.ErrNotFound},
		roles:   &fakeKMRoles{},
		devices: &fakeKMDevices{},
		keyPkgs: &fakeKMKeyPackages{},
	}
	req := withSession(httptest.NewRequest("GET", "/workspaces/w1/users/ghost/key-material", nil))
	req.SetPathValue("wsId", "w1")
	req.SetPathValue("identity", "ghost")
	rec := httptest.NewRecorder()
	h.get(rec, req)
	if rec.Code != 404 {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestKeyMaterialTargetNotInWorkspace(t *testing.T) {
	h := &keyMaterialHandlers{
		users:   &fakeKMUsers{user: &store.User{ID: "target"}},
		roles:   &fakeKMRoles{errFor: map[string]error{"target": store.ErrNotFound}},
		devices: &fakeKMDevices{},
		keyPkgs: &fakeKMKeyPackages{},
	}
	req := withSession(httptest.NewRequest("GET", "/workspaces/w1/users/bob/key-material", nil))
	req.SetPathValue("wsId", "w1")
	req.SetPathValue("identity", "bob")
	rec := httptest.NewRecorder()
	h.get(rec, req)
	if rec.Code != 404 {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestKeyMaterialHappyPath(t *testing.T) {
	h := &keyMaterialHandlers{
		users: &fakeKMUsers{user: &store.User{ID: "target"}},
		roles: &fakeKMRoles{},
		devices: &fakeKMDevices{devs: []store.DeviceKey{
			{DeviceID: "d1", SigningPublicKey: []byte("sign-1")},
			{DeviceID: "d2", SigningPublicKey: []byte("sign-2")},
		}},
		keyPkgs: &fakeKMKeyPackages{byDevice: map[string][]byte{
			"d1": []byte("kp-1"),
			"d2": []byte("kp-2"),
		}},
	}
	req := withSession(httptest.NewRequest("GET", "/workspaces/w1/users/bob/key-material", nil))
	req.SetPathValue("wsId", "w1")
	req.SetPathValue("identity", "bob")
	rec := httptest.NewRecorder()
	h.get(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp []keyMaterialResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resp))
	}
	byID := map[string]keyMaterialResp{}
	for _, e := range resp {
		byID[e.DeviceID] = e
	}
	want := map[string]struct{ sign, kp string }{
		"d1": {"sign-1", "kp-1"},
		"d2": {"sign-2", "kp-2"},
	}
	for id, w := range want {
		e := byID[id]
		gotSign, _ := base64.StdEncoding.DecodeString(e.SigningPublicKey)
		gotKP, _ := base64.StdEncoding.DecodeString(e.KeyPackage)
		if string(gotSign) != w.sign {
			t.Fatalf("%s signing key = %q, want %q", id, gotSign, w.sign)
		}
		if string(gotKP) != w.kp {
			t.Fatalf("%s key package = %q, want %q", id, gotKP, w.kp)
		}
	}
}
