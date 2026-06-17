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
}
