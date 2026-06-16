package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestLogoutRevokesImmediately(t *testing.T) {
	h, sess := newServer(t)
	token, _ := sess.Issue(context.Background(), "user-x", "")
	if serve(h, httptestNewGet("/auth/session", token)).Code != http.StatusOK {
		t.Fatal("expected 200 with valid token")
	}
	if postJSON(t, h, "/auth/logout", nil, token).Code != http.StatusOK {
		t.Fatal("logout failed")
	}
	if serve(h, httptestNewGet("/auth/session", token)).Code != http.StatusUnauthorized {
		t.Fatal("expected 401 after logout")
	}
}

func TestLoginIndistinguishableForUnknownUser(t *testing.T) {
	h, _ := newServer(t)
	rec := postJSON(t, h, "/auth/login/start",
		map[string]string{"email": "ghost@corp", "ke1": b64(make([]byte, 96))}, "")
	if rec.Code == http.StatusNotFound {
		t.Fatal("unknown user must not be distinguishable via 404")
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if msg, _ := body["message"].(string); msg == "user not found" {
		t.Fatal("error message leaks user existence")
	}
}
