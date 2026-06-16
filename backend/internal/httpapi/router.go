package httpapi

import (
	"net/http"

	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/devices"
	"github.com/messenger/backend/internal/keypackages"
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

func NewRouterFull(svc *as.Service, sess *session.Manager, devSvc *devices.Service, kpSvc *keypackages.Service) http.Handler {
	mux := http.NewServeMux()
	ah := &authHandlers{svc: svc, sess: sess}
	dh := &deviceHandlers{svc: devSvc, kp: kpSvc, sess: sess}
	kh := &keypackageHandlers{svc: kpSvc}

	mux.HandleFunc("POST /auth/register/start", ah.registerStart)
	mux.HandleFunc("POST /auth/register/finish", ah.registerFinish)
	mux.HandleFunc("POST /auth/login/start", ah.loginStart)
	mux.HandleFunc("POST /auth/login/finish", ah.loginFinish)

	auth := authMW(sess)
	mux.Handle("GET /auth/session", auth(http.HandlerFunc(ah.session)))
	mux.Handle("POST /auth/logout", auth(http.HandlerFunc(ah.logout)))
	mux.Handle("POST /auth/logout/all", auth(http.HandlerFunc(ah.logoutAll)))

	mux.Handle("POST /devices", auth(http.HandlerFunc(dh.enroll)))
	mux.Handle("GET /devices", auth(http.HandlerFunc(dh.list)))
	mux.Handle("POST /devices/{id}/revoke", auth(http.HandlerFunc(dh.revoke)))

	mux.Handle("POST /keypackages", auth(http.HandlerFunc(kh.upload)))
	mux.Handle("GET /keypackages/count", auth(http.HandlerFunc(kh.count)))
	mux.Handle("GET /keypackages/{device_id}", auth(http.HandlerFunc(kh.consume)))

	return recoverMW(mux)
}
