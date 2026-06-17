package httpapi

import (
	"crypto/ed25519"
	"net/http"

	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/conversations"
	"github.com/messenger/backend/internal/devices"
	"github.com/messenger/backend/internal/keypackages"
	"github.com/messenger/backend/internal/kt"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
	"github.com/messenger/backend/internal/workspace"
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

func NewRouterFull(svc *as.Service, sess *session.Manager, devSvc *devices.Service, kpSvc *keypackages.Service, rl *session.RateLimiter, rosterRepo *store.RosterRepo, ktSvc *kt.Service, ktPub ed25519.PublicKey, wsSvc *workspace.Service, convSvc *conversations.Service, userRepo *store.UserRepo, deviceRepo *store.DeviceRepo, wsRepo *store.WorkspaceRepo, convRepo *store.ConvRepo, complianceDeviceID string) http.Handler {
	mux := http.NewServeMux()
	ah := &authHandlers{svc: svc, sess: sess}
	dh := &deviceHandlers{svc: devSvc, kp: kpSvc, sess: sess}
	kh := &keypackageHandlers{svc: kpSvc}
	rh := &rosterHandlers{roster: rosterRepo, members: convSvc}
	kth := &ktHandlers{svc: ktSvc, pubKey: ktPub}
	wh := &workspaceHandlers{svc: wsSvc}
	ch := &conversationHandlers{svc: convSvc}
	kmh := &keyMaterialHandlers{users: userRepo, roles: wsRepo, devices: deviceRepo, keyPkgs: kpSvc}
	gih := &groupInfoHandlers{conv: convSvc, store: convRepo, compliance: kpSvc, complianceDeviceID: complianceDeviceID}
	rlmw := rateLimitMW(rl)

	mux.Handle("POST /auth/register/start", rlmw(http.HandlerFunc(ah.registerStart)))
	mux.Handle("POST /auth/login/start", rlmw(http.HandlerFunc(ah.loginStart)))
	mux.HandleFunc("POST /auth/register/finish", ah.registerFinish)
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

	// Device-roster mutations are gated to user-members of the conversation
	// (see rosterHandlers); non-members receive 404.
	mux.Handle("POST /conversations/{group}/members", auth(http.HandlerFunc(rh.addMember)))
	mux.Handle("DELETE /conversations/{group}/members/{device}", auth(http.HandlerFunc(rh.removeMember)))

	mux.Handle("GET /kt/pubkey", auth(http.HandlerFunc(kth.pubkey)))
	mux.Handle("GET /kt/sth", auth(http.HandlerFunc(kth.sth)))
	mux.Handle("GET /kt/key/{identity}", auth(http.HandlerFunc(kth.key)))
	mux.Handle("GET /kt/proof/inclusion", auth(http.HandlerFunc(kth.inclusion)))
	mux.Handle("GET /kt/proof/consistency", auth(http.HandlerFunc(kth.consistency)))

	mux.Handle("POST /workspaces", auth(http.HandlerFunc(wh.create)))
	mux.Handle("GET /workspaces", auth(http.HandlerFunc(wh.list)))
	mux.Handle("GET /workspaces/{id}/members", auth(http.HandlerFunc(wh.members)))
	mux.Handle("GET /workspaces/{id}/members/search", auth(http.HandlerFunc(wh.search)))
	mux.Handle("POST /workspaces/{id}/members", auth(http.HandlerFunc(wh.addMember)))
	mux.Handle("PATCH /workspaces/{id}/members/{user}", auth(http.HandlerFunc(wh.setRole)))
	mux.Handle("DELETE /workspaces/{id}/members/me", auth(http.HandlerFunc(wh.leave)))
	mux.Handle("DELETE /workspaces/{id}/members/{user}", auth(http.HandlerFunc(wh.removeMember)))

	mux.Handle("GET /workspaces/{wsId}/users/{identity}/key-material", auth(http.HandlerFunc(kmh.get)))

	mux.Handle("POST /workspaces/{wsId}/conversations", auth(http.HandlerFunc(ch.create)))
	mux.Handle("GET /workspaces/{wsId}/conversations", auth(http.HandlerFunc(ch.list)))
	mux.Handle("GET /conversations/{group}", auth(http.HandlerFunc(ch.get)))
	mux.Handle("POST /conversations/{group}/join", auth(http.HandlerFunc(ch.join)))
	mux.Handle("POST /conversations/{group}/users", auth(http.HandlerFunc(ch.addUser)))
	mux.Handle("DELETE /conversations/{group}/users/{userId}", auth(http.HandlerFunc(ch.removeUser)))

	mux.Handle("PUT /conversations/{group}/group-info", auth(http.HandlerFunc(gih.putGroupInfo)))
	mux.Handle("GET /conversations/{group}/group-info", auth(http.HandlerFunc(gih.getGroupInfo)))
	mux.Handle("GET /keypackages/compliance", auth(http.HandlerFunc(gih.complianceKeyPackage)))

	return recoverMW(mux)
}
