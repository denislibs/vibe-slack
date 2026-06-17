package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The roster route must be registered and auth-protected (401 without a token,
// not 404 — proving it exists).
func TestRosterRouteIsRegisteredAndProtected(t *testing.T) {
	h, _ := newRosterServer(t)
	req := httptest.NewRequest(http.MethodPost, "/conversations/g1/members", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatal("roster route must be registered (got 404)")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}
