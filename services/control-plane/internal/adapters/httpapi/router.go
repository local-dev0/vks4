package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"

	jwtauth "github.com/vks4/vks4/services/control-plane/internal/adapters/auth/jwt"
	"github.com/vks4/vks4/services/control-plane/internal/domain"
)

type Deps struct {
	Log     *zap.Logger
	JWT     *jwtauth.Issuer
	Auth    *AuthHandlers
	Rooms   *RoomHandlers
	Users   *UserHandlers
	Recs    *RecordingHandlers
	System  *SystemHandlers
	Layouts *LayoutHandlers
}

func Router(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	r.Use(Logging(d.Log))

	r.Get("/health", d.System.Health)

	r.Route("/api/v1", func(api chi.Router) {
		// public
		api.Post("/auth/login", d.Auth.Login)
		api.Post("/auth/refresh", d.Auth.Refresh)

		// protected
		api.Group(func(p chi.Router) {
			p.Use(Auth(d.JWT))

			p.Get("/me", d.Auth.Me)
			p.Post("/auth/logout", d.Auth.Logout)

			// rooms (admin/operator/moderator)
			p.Route("/rooms", func(r chi.Router) {
				r.Use(RequireRole(domain.RoleAdmin, domain.RoleOperator, domain.RoleModerator, domain.RoleViewer))
				r.Get("/", d.Rooms.List)
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator)).Post("/", d.Rooms.Create)
				r.Get("/{id}", d.Rooms.Get)
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator)).Patch("/{id}", d.Rooms.Update)
				r.With(RequireRole(domain.RoleAdmin)).Delete("/{id}", d.Rooms.Delete)
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator, domain.RoleModerator)).Post("/{id}/lock", d.Rooms.Lock)
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator, domain.RoleModerator)).Post("/{id}/unlock", d.Rooms.Unlock)
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator)).Put("/{id}/layout", d.Rooms.Layout)
				r.Get("/{id}/slots", d.Rooms.GetSlots)
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator, domain.RoleModerator)).Put("/{id}/slots", d.Rooms.Slots)
				r.Get("/{id}/participants", d.Rooms.Participants)
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator, domain.RoleModerator)).
					Delete("/{id}/participants/{peerId}", d.Rooms.Kick)

				// recording
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator)).Post("/{id}/recording", d.Recs.Start)
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator)).Delete("/{id}/recording", d.Recs.Stop)
			})

			// recordings
			p.Route("/recordings", func(r chi.Router) {
				r.Get("/", d.Recs.List)
				r.Get("/{id}", d.Recs.Get)
				r.Get("/{id}/download", d.Recs.Download)
				r.With(RequireRole(domain.RoleAdmin)).Delete("/{id}", d.Recs.Delete)
			})

			// layout templates
			p.Route("/layout-templates", func(r chi.Router) {
				r.Use(RequireRole(domain.RoleAdmin, domain.RoleOperator, domain.RoleModerator, domain.RoleViewer))
				r.Get("/", d.Layouts.List)
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator)).Post("/", d.Layouts.Create)
				r.Get("/{id}", d.Layouts.Get)
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator)).Patch("/{id}", d.Layouts.Patch)
				r.With(RequireRole(domain.RoleAdmin, domain.RoleOperator)).Delete("/{id}", d.Layouts.Delete)
			})

			// users (admin only)
			p.Route("/users", func(r chi.Router) {
				r.Use(RequireRole(domain.RoleAdmin))
				r.Get("/", d.Users.List)
				r.Post("/", d.Users.Create)
				r.Patch("/{id}", d.Users.Patch)
				r.Delete("/{id}", d.Users.Delete)
			})

			// monitoring
			p.Get("/monitoring/system", d.System.MonitoringSystem)

			// audit
			p.With(RequireRole(domain.RoleAdmin)).Get("/audit", d.System.Audit)

			// system health (auth required)
			p.Get("/health", d.System.Health)
		})
	})
	return r
}
