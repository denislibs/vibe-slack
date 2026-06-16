package httpapi

import (
	"net/http"

	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/session"
)

// NewRouter wires the auth endpoints. Device/keypackage routes are added later.
func NewRouter(svc *as.Service, sess *session.Manager) http.Handler {
	mux := http.NewServeMux()
	ah := &authHandlers{svc: svc, sess: sess}

	mux.HandleFunc("POST /auth/register/start", ah.registerStart)
	mux.HandleFunc("POST /auth/register/finish", ah.registerFinish)
	mux.HandleFunc("POST /auth/login/start", ah.loginStart)
	mux.HandleFunc("POST /auth/login/finish", ah.loginFinish)

	auth := authMW(sess)
	mux.Handle("GET /auth/session", auth(http.HandlerFunc(ah.session)))
	mux.Handle("POST /auth/logout", auth(http.HandlerFunc(ah.logout)))
	mux.Handle("POST /auth/logout/all", auth(http.HandlerFunc(ah.logoutAll)))

	return recoverMW(mux)
}
