package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenFromHTTP(t *testing.T) {
	// header present → header token
	r1 := httptest.NewRequest("GET", "/x", nil)
	r1.Header.Set("Authorization", "Bearer HTOK")
	if got := tokenFromHTTP(r1); got != "HTOK" {
		t.Fatalf("header token: got %q", got)
	}
	// no header, session cookie present → cookie value
	r2 := httptest.NewRequest("GET", "/x", nil)
	r2.AddCookie(&http.Cookie{Name: "session", Value: "CTOK"})
	if got := tokenFromHTTP(r2); got != "CTOK" {
		t.Fatalf("cookie token: got %q", got)
	}
	// both present → header wins
	r3 := httptest.NewRequest("GET", "/x", nil)
	r3.Header.Set("Authorization", "Bearer HTOK")
	r3.AddCookie(&http.Cookie{Name: "session", Value: "CTOK"})
	if got := tokenFromHTTP(r3); got != "HTOK" {
		t.Fatalf("header should win: got %q", got)
	}
	// neither → empty
	r4 := httptest.NewRequest("GET", "/x", nil)
	if got := tokenFromHTTP(r4); got != "" {
		t.Fatalf("none: got %q", got)
	}
	// malformed header, no cookie → empty
	r5 := httptest.NewRequest("GET", "/x", nil)
	r5.Header.Set("Authorization", "Basic xyz")
	if got := tokenFromHTTP(r5); got != "" {
		t.Fatalf("non-bearer: got %q", got)
	}
}
