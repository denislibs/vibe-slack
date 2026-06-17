package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/messenger/backend/internal/session"
)

type ctxKey string

const sessionCtxKey ctxKey = "session"

const sessionCookie = "session"

func setSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: int((24 * time.Hour) / time.Second),
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
}

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

// tokenFromHTTP returns the session token from the Authorization: Bearer header,
// falling back to the `session` cookie (set HttpOnly on login). Header wins.
func tokenFromHTTP(r *http.Request) string {
	authz := r.Header.Get("Authorization")
	if t := strings.TrimPrefix(authz, "Bearer "); authz != "" && t != authz {
		return t
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

func authMW(sess *session.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := tokenFromHTTP(r)
			if token == "" {
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

func rateLimitMW(rl *session.RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.Header.Get("X-Forwarded-For")
			if ip == "" {
				ip = r.RemoteAddr
			}
			ok, err := rl.Allow(r.Context(), "auth:"+ip)
			if err == nil && !ok {
				writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
