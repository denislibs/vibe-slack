package ws

import (
	"net/http"
	"testing"
)

func TestTokenFromRequest(t *testing.T) {
	// header present → use it
	r1, _ := http.NewRequest("GET", "/ws", nil)
	r1.Header.Set("Authorization", "Bearer HTOK")
	if got := tokenFromRequest(r1); got != "HTOK" {
		t.Fatalf("header token: got %q", got)
	}
	// no header, query present → use query
	r2, _ := http.NewRequest("GET", "/ws?access_token=QTOK", nil)
	if got := tokenFromRequest(r2); got != "QTOK" {
		t.Fatalf("query token: got %q", got)
	}
	// header wins over query when both present
	r3, _ := http.NewRequest("GET", "/ws?access_token=QTOK", nil)
	r3.Header.Set("Authorization", "Bearer HTOK")
	if got := tokenFromRequest(r3); got != "HTOK" {
		t.Fatalf("header should win: got %q", got)
	}
	// neither → empty
	r4, _ := http.NewRequest("GET", "/ws", nil)
	if got := tokenFromRequest(r4); got != "" {
		t.Fatalf("none: got %q", got)
	}
	// malformed header (no Bearer prefix), no query → empty
	r5, _ := http.NewRequest("GET", "/ws", nil)
	r5.Header.Set("Authorization", "Basic xyz")
	if got := tokenFromRequest(r5); got != "" {
		t.Fatalf("non-bearer: got %q", got)
	}
	// cookie only → cookie token
	r6, _ := http.NewRequest("GET", "/ws", nil)
	r6.AddCookie(&http.Cookie{Name: "session", Value: "CTOK"})
	if got := tokenFromRequest(r6); got != "CTOK" {
		t.Fatalf("cookie token: got %q", got)
	}
	// precedence: header > query > cookie
	r7, _ := http.NewRequest("GET", "/ws?access_token=QTOK", nil)
	r7.Header.Set("Authorization", "Bearer HTOK")
	r7.AddCookie(&http.Cookie{Name: "session", Value: "CTOK"})
	if got := tokenFromRequest(r7); got != "HTOK" {
		t.Fatalf("header should win over query+cookie: got %q", got)
	}
	// query > cookie when no header
	r8, _ := http.NewRequest("GET", "/ws?access_token=QTOK", nil)
	r8.AddCookie(&http.Cookie{Name: "session", Value: "CTOK"})
	if got := tokenFromRequest(r8); got != "QTOK" {
		t.Fatalf("query should win over cookie: got %q", got)
	}
}
