package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	appmw "gold-track-be/internal/middleware"
	"gold-track-be/internal/service"
)

// NewRouter wires global middleware (request id, centralized recovery +
// error response, request logging) and all route registrations.
//
// /api/auth/login is intentionally outside the JWTAuth group — a client
// can't present a token before it has one. Every other /api route should be
// registered behind r.Use(appmw.JWTAuth(authService)), optionally chained
// with appmw.RequireRole(...) for role-restricted actions.
func NewRouter(
	logger *slog.Logger,
	healthHandler *HealthHandler,
	authHandler *AuthHandler,
	authService service.AuthService,
	userHandler *UserHandler,
	categoryHandler *CategoryHandler,
	brandHandler *BrandHandler,
	productHandler *ProductHandler,
	supplierHandler *SupplierHandler,
	stockItemHandler *StockItemHandler,
	customerHandler *CustomerHandler,
) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(appmw.Recoverer(logger))
	r.Use(appmw.RequestLogger(logger))

	r.Get("/health", healthHandler.Check)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/login", authHandler.Login)

		r.Group(func(r chi.Router) {
			r.Use(appmw.JWTAuth(authService))
			r.Post("/auth/logout", authHandler.Logout)

			r.Get("/products", productHandler.List)
			r.Get("/products/{id}", productHandler.Get)
			r.Get("/products/{productId}/stock-items", stockItemHandler.ListByProduct)
			r.Get("/stock-items/{id}", stockItemHandler.Get)
			r.Get("/stock-items/{id}/label", stockItemHandler.GetLabel)
			r.Route("/customers", func(r chi.Router) {
				r.Get("/", customerHandler.List)
				r.Post("/", customerHandler.Create)
				r.Get("/{id}", customerHandler.Get)
			})

			r.Group(func(r chi.Router) {
				r.Use(appmw.RequireRole("SUPER_ADMIN"))
				r.Route("/users", func(r chi.Router) {
					r.Get("/", userHandler.List)
					r.Post("/", userHandler.Create)
					r.Get("/{id}", userHandler.Get)
					r.Put("/{id}", userHandler.Update)
					r.Delete("/{id}", userHandler.Delete)
				})
			})

			r.Group(func(r chi.Router) {
				r.Use(appmw.RequireRole("ADMIN", "SUPER_ADMIN"))
				r.Route("/categories", func(r chi.Router) {
					r.Get("/", categoryHandler.List)
					r.Post("/", categoryHandler.Create)
					r.Get("/{id}", categoryHandler.Get)
					r.Put("/{id}", categoryHandler.Update)
					r.Delete("/{id}", categoryHandler.Delete)
				})
				r.Route("/brands", func(r chi.Router) {
					r.Get("/", brandHandler.List)
					r.Post("/", brandHandler.Create)
					r.Get("/{id}", brandHandler.Get)
					r.Put("/{id}", brandHandler.Update)
					r.Delete("/{id}", brandHandler.Delete)
				})
				r.Post("/products", productHandler.Create)
				r.Put("/products/{id}", productHandler.Update)
				r.Delete("/products/{id}", productHandler.Delete)
				r.Post("/products/{productId}/stock-items", stockItemHandler.Create)
				r.Put("/stock-items/{id}", stockItemHandler.Update)
				r.Delete("/stock-items/{id}", stockItemHandler.Delete)
				r.Route("/suppliers", func(r chi.Router) {
					r.Get("/", supplierHandler.List)
					r.Post("/", supplierHandler.Create)
					r.Get("/{id}", supplierHandler.Get)
					r.Put("/{id}", supplierHandler.Update)
					r.Delete("/{id}", supplierHandler.Delete)
				})
				r.Put("/customers/{id}", customerHandler.Update)
				r.Delete("/customers/{id}", customerHandler.Delete)
			})
		})
	})

	return r
}
