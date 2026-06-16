package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/messenger/backend/internal/session"
)

type ctxKey string

const sessionCtxKey ctxKey = "session"

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorResponse{Error: code, Message: msg})
}

func recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v", rec)
				writeError(w, http.StatusInternalServerError, "internal", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type sessionWithToken struct {
	Session *session.Session
	Token   string
}

func authMW(sess *session.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := r.Header.Get("Authorization")
			token := strings.TrimPrefix(authz, "Bearer ")
			if token == "" || token == authz {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
				return
			}
			s, err := sess.Validate(r.Context(), token)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid session")
				return
			}
			ctx := context.WithValue(r.Context(), sessionCtxKey, sessionWithToken{Session: s, Token: token})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func sessionFrom(ctx context.Context) sessionWithToken {
	v, _ := ctx.Value(sessionCtxKey).(sessionWithToken)
	return v
}
