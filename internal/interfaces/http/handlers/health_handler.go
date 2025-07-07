package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"rinha-backend-clean/internal/domain/services"
	"rinha-backend-clean/internal/infrastructure/queue"
)

// HealthHandler handles health check requests
type HealthHandler struct {
	queueService      services.QueueService
	retryQueueService services.RetryQueueService
	autoScaler        *queue.AutoScaler
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(
	queueService services.QueueService,
	retryQueueService services.RetryQueueService,
	autoScaler *queue.AutoScaler,
) *HealthHandler {
	return &HealthHandler{
		queueService:      queueService,
		retryQueueService: retryQueueService,
		autoScaler:        autoScaler,
	}
}

// HandleHealth handles GET /health requests
func (h *HealthHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	// Get queue statistics
	queueStats := map[string]interface{}{
		"queue_size": h.queueService.GetQueueSize(),
	}

	retryStats := h.retryQueueService.GetQueueStats()
	scalingMetrics := h.autoScaler.GetMetrics()

	response := map[string]interface{}{
		"status":      "healthy",
		"timestamp":   time.Now().Format(time.RFC3339),
		"queue_stats": queueStats,
		"retry_stats": retryStats,
		"scaling_metrics": map[string]interface{}{
			"current_workers":     scalingMetrics.CurrentQueueSize, // This should be current workers count
			"queue_utilization":   scalingMetrics.QueueUtilization,
			"processing_rate":     scalingMetrics.ProcessingRate,
			"worker_efficiency":   scalingMetrics.WorkerEfficiency,
			"total_scale_ups":     scalingMetrics.TotalScaleUps,
			"total_scale_downs":   scalingMetrics.TotalScaleDowns,
			"last_scaling_action": scalingMetrics.LastScalingDecision.Format(time.RFC3339),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandleScalingMetrics handles GET /scaling-metrics requests
func (h *HealthHandler) HandleScalingMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.autoScaler.GetMetrics()
	retryStats := h.retryQueueService.GetQueueStats()

	response := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"auto_scaling": map[string]interface{}{
			"queue_utilization":     metrics.QueueUtilization * 100, // Convert to percentage
			"current_queue_size":    metrics.CurrentQueueSize,
			"processing_rate":       metrics.ProcessingRate,
			"worker_efficiency":     metrics.WorkerEfficiency,
			"scale_up_count":        metrics.TotalScaleUps,
			"scale_down_count":      metrics.TotalScaleDowns,
			"last_scaling_decision": metrics.LastScalingDecision.Format(time.RFC3339),
		},
		"queue_details": map[string]interface{}{
			"main_queue_size":      h.queueService.GetQueueSize(),
			"default_retry_size":   retryStats["default_queue_size"],
			"fallback_retry_size":  retryStats["fallback_queue_size"],
			"permanent_retry_size": retryStats["permanent_queue_size"],
			"total_workers":        retryStats["worker_count"],
		},
		"performance": map[string]interface{}{
			"max_default_retries":   retryStats["max_default_retries"],
			"max_fallback_retries":  retryStats["max_fallback_retries"],
			"max_permanent_retries": retryStats["max_permanent_retries"],
			"retry_intervals": map[string]interface{}{
				"default_interval":   retryStats["ticker_interval"],
				"permanent_interval": retryStats["permanent_ticker_interval"],
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
