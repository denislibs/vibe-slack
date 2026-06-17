package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKTRouteRegisteredAndProtected(t *testing.T) {
	h, _ := newKTServer(t)
	req := httptest.NewRequest(http.MethodGet, "/kt/sth", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatal("kt route must be registered (got 404)")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}
