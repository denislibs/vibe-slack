package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/messenger/backend/internal/conversations"
	"github.com/messenger/backend/internal/store"
)

type fakeGIConv struct {
	member bool
	getErr error
}

func (f *fakeGIConv) IsMember(context.Context, string, string) (bool, error) {
	return f.member, nil
}
func (f *fakeGIConv) Get(context.Context, string, string) (*store.Conversation, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &store.Conversation{GroupID: "g1", Type: "channel", Visibility: "public", Name: "general"}, nil
}

type fakeGIStore struct {
	saved   []byte
	setErr  error
	info    []byte
	infoErr error
}

func (f *fakeGIStore) SetGroupInfo(_ context.Context, _ string, info []byte) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.saved = info
	return nil
}
func (f *fakeGIStore) GroupInfo(context.Context, string) ([]byte, error) {
	if f.infoErr != nil {
		return nil, f.infoErr
	}
	return f.info, nil
}

type fakeComplianceKP struct {
	kp  []byte
	err error
}

func (f *fakeComplianceKP) Consume(context.Context, string) ([]byte, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	return f.kp, false, nil
}

func TestPutGroupInfoNonMember(t *testing.T) {
	h := &groupInfoHandlers{conv: &fakeGIConv{member: false}, store: &fakeGIStore{}}
	req := withSession(httptest.NewRequest("PUT", "/conversations/g1/group-info",
		strings.NewReader(`{"group_info":"AAAA"}`)))
	req.SetPathValue("group", "g1")
	rec := httptest.NewRecorder()
	h.putGroupInfo(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPutGroupInfoMember(t *testing.T) {
	gs := &fakeGIStore{}
	h := &groupInfoHandlers{conv: &fakeGIConv{member: true}, store: gs}
	req := withSession(httptest.NewRequest("PUT", "/conversations/g1/group-info",
		strings.NewReader(`{"group_info":"aGVsbG8="}`)))
	req.SetPathValue("group", "g1")
	rec := httptest.NewRecorder()
	h.putGroupInfo(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if string(gs.saved) != "hello" {
		t.Fatalf("expected store to record %q, got %q", "hello", string(gs.saved))
	}
}

func TestGetGroupInfoMember(t *testing.T) {
	h := &groupInfoHandlers{conv: &fakeGIConv{member: true}, store: &fakeGIStore{info: []byte("world")}}
	req := withSession(httptest.NewRequest("GET", "/conversations/g1/group-info", nil))
	req.SetPathValue("group", "g1")
	rec := httptest.NewRecorder()
	h.getGroupInfo(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp groupInfoResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	b, _ := base64.StdEncoding.DecodeString(resp.GroupInfo)
	if string(b) != "world" {
		t.Fatalf("expected %q, got %q", "world", string(b))
	}
}

func TestGetGroupInfoUnpublished(t *testing.T) {
	h := &groupInfoHandlers{conv: &fakeGIConv{member: true}, store: &fakeGIStore{infoErr: store.ErrNotFound}}
	req := withSession(httptest.NewRequest("GET", "/conversations/g1/group-info", nil))
	req.SetPathValue("group", "g1")
	rec := httptest.NewRecorder()
	h.getGroupInfo(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error != "not_published" {
		t.Fatalf("expected not_published, got %q", resp.Error)
	}
}

func TestGetGroupInfoNotVisible(t *testing.T) {
	h := &groupInfoHandlers{conv: &fakeGIConv{getErr: conversations.ErrNotMember}, store: &fakeGIStore{}}
	req := withSession(httptest.NewRequest("GET", "/conversations/g1/group-info", nil))
	req.SetPathValue("group", "g1")
	rec := httptest.NewRecorder()
	h.getGroupInfo(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestComplianceKPUnconfigured(t *testing.T) {
	h := &groupInfoHandlers{compliance: &fakeComplianceKP{}, complianceDeviceID: ""}
	req := withSession(httptest.NewRequest("GET", "/keypackages/compliance", nil))
	rec := httptest.NewRecorder()
	h.complianceKeyPackage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	var resp errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error != "compliance_not_configured" {
		t.Fatalf("expected compliance_not_configured, got %q", resp.Error)
	}
}

func TestComplianceKPConfigured(t *testing.T) {
	h := &groupInfoHandlers{compliance: &fakeComplianceKP{kp: []byte("kp")}, complianceDeviceID: "dev-compliance"}
	req := withSession(httptest.NewRequest("GET", "/keypackages/compliance", nil))
	rec := httptest.NewRecorder()
	h.complianceKeyPackage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp complianceKPResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	b, _ := base64.StdEncoding.DecodeString(resp.KeyPackage)
	if string(b) != "kp" {
		t.Fatalf("expected %q, got %q", "kp", string(b))
	}
}
