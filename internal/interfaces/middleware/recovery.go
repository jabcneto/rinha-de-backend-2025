package middleware

import (
	"net/http"
	"rinha-backend-clean/internal/infrastructure/logger"
	"runtime/debug"
)

// RecoveryMiddleware recovers from panics and logs them
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// Log the panic with stack trace
				logger.WithField("stack", string(debug.Stack())).
					WithField("panic", err).
					WithField("method", r.Method).
					WithField("path", r.URL.Path).
					Error("Panic recovered in HTTP handler")

				// Return 500 status
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
