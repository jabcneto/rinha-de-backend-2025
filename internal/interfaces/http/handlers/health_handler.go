package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"rinha-backend-clean/internal/domain/services"
)

// HealthHandler handles health check requests
type HealthHandler struct {
	queueService services.QueueService
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(queueService services.QueueService) *HealthHandler {
	return &HealthHandler{
		queueService: queueService,
	}
}

// HandleHealth handles GET /health requests
func (h *HealthHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":     "healthy",
		"timestamp":  time.Now().Format(time.RFC3339),
		"queue_size": h.queueService.GetQueueSize(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
