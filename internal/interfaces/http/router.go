package http

import (
	"rinha-backend-clean/internal/interfaces/http/handlers"
	"rinha-backend-clean/internal/interfaces/middleware"

	"github.com/gorilla/mux"
)

// Router sets up HTTP routes
type Router struct {
	paymentHandler *handlers.PaymentHandler
	healthHandler  *handlers.HealthHandler
}

// NewRouter creates a new HTTP router
func NewRouter(paymentHandler *handlers.PaymentHandler, healthHandler *handlers.HealthHandler) *Router {
	return &Router{
		paymentHandler: paymentHandler,
		healthHandler:  healthHandler,
	}
}

// SetupRoutes sets up all HTTP routes
func (rt *Router) SetupRoutes() *mux.Router {
	r := mux.NewRouter()

	// Apply middleware
	r.Use(middleware.LoggingMiddleware)
	r.Use(middleware.RecoveryMiddleware)
	r.Use(middleware.CORSMiddleware)

	// Payment routes
	r.HandleFunc("/payments", rt.paymentHandler.HandlePayments).Methods("POST")
	r.HandleFunc("/payments-summary", rt.paymentHandler.HandlePaymentsSummary).Methods("GET")
	r.HandleFunc("/purge-payments", rt.paymentHandler.HandlePurgePayments).Methods("POST")

	// Health check route
	r.HandleFunc("/health", rt.healthHandler.HandleHealth).Methods("GET")

	return r
}
