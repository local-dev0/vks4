package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	jwtauth "github.com/vks4/vks4/services/control-plane/internal/adapters/auth/jwt"
	"github.com/vks4/vks4/services/control-plane/internal/domain"
	"github.com/vks4/vks4/services/control-plane/internal/pkg/metrics"
)

type ctxKey string

const (
	ctxUserID ctxKey = "uid"
	ctxRole   ctxKey = "role"
	ctxEmail  ctxKey = "email"
)

func UserIDFromCtx(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(ctxUserID).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

func RoleFromCtx(ctx context.Context) domain.Role {
	if v, ok := ctx.Value(ctxRole).(string); ok {
		return domain.Role(v)
	}
	return ""
}

// Logging — структурный лог + Prometheus.
func Logging(log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sr := &statusRecorder{ResponseWriter: w, code: 200}
			next.ServeHTTP(sr, r)
			d := time.Since(start)
			path := chiRoutePattern(r)
			metrics.HTTPRequestsTotal.WithLabelValues(r.Method, path, http.StatusText(sr.code)).Inc()
			metrics.HTTPRequestDuration.WithLabelValues(r.Method, path).Observe(d.Seconds())
			log.Info("http",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", sr.code),
				zap.Duration("dur", d),
			)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(c int) {
	s.code = c
	s.ResponseWriter.WriteHeader(c)
}

// Auth — JWT verification (Bearer).
func Auth(issuer *jwtauth.Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
				return
			}
			tok := strings.TrimPrefix(h, "Bearer ")
			claims, err := issuer.Parse(tok)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
				return
			}
			ctx := context.WithValue(r.Context(), ctxUserID, claims.UserID)
			ctx = context.WithValue(ctx, ctxRole, claims.Role)
			ctx = context.WithValue(ctx, ctxEmail, claims.Email)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole — RBAC guard.
func RequireRole(roles ...domain.Role) func(http.Handler) http.Handler {
	allowed := map[domain.Role]struct{}{}
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := allowed[RoleFromCtx(r.Context())]; !ok {
				writeError(w, http.StatusForbidden, "forbidden", "insufficient role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
