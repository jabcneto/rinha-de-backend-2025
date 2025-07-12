package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/valyala/fasthttp"
	"rinha-backend-clean/internal/domain/services"
	"rinha-backend-clean/internal/infrastructure/external"
	"rinha-backend-clean/internal/infrastructure/queue"
)

// HealthHandler handles health check requests
type HealthHandler struct {
	queueService      services.QueueService
	retryQueueService services.RetryQueueService
	autoScaler        *queue.AutoScaler
	processorService  services.PaymentProcessorService
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(
	queueService services.QueueService,
	retryQueueService services.RetryQueueService,
	autoScaler *queue.AutoScaler,
	processorService services.PaymentProcessorService,
) *HealthHandler {
	return &HealthHandler{
		queueService:      queueService,
		retryQueueService: retryQueueService,
		autoScaler:        autoScaler,
		processorService:  processorService,
	}
}

// HandleHealth handles GET /health requests
func (h *HealthHandler) HandleHealth(ctx *fasthttp.RequestCtx) {
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
			"current_workers":     scalingMetrics.CurrentQueueSize,
			"queue_utilization":   scalingMetrics.QueueUtilization,
			"processing_rate":     scalingMetrics.ProcessingRate,
			"worker_efficiency":   scalingMetrics.WorkerEfficiency,
			"total_scale_ups":     scalingMetrics.TotalScaleUps,
			"total_scale_downs":   scalingMetrics.TotalScaleDowns,
			"last_scaling_action": scalingMetrics.LastScalingDecision.Format(time.RFC3339),
		},
	}
	jsonResp, _ := json.Marshal(response)
	ctx.SetContentType("application/json")
	ctx.SetBody(jsonResp)
}

// HandleScalingMetrics handles GET /scaling-metrics requests
func (h *HealthHandler) HandleScalingMetrics(ctx *fasthttp.RequestCtx) {
	metrics := h.autoScaler.GetMetrics()
	retryStats := h.retryQueueService.GetQueueStats()

	response := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"auto_scaling": map[string]interface{}{
			"queue_utilization":     metrics.QueueUtilization * 100,
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

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	jsonResp, _ := json.Marshal(response)
	ctx.SetBody(jsonResp)
}

// HandleCircuitBreakerStatus handles GET /circuit-breakers requests
func (h *HealthHandler) HandleCircuitBreakerStatus(ctx *fasthttp.RequestCtx) {
	// Type assertion to get the concrete type that has GetIntegratedStats
	if processorClient, ok := h.processorService.(*external.PaymentProcessorClient); ok {
		stats := processorClient.GetIntegratedStats()

		// Get processor health status
		healthStatus, _ := h.processorService.GetHealthStatus(context.Background())

		response := map[string]interface{}{
			"timestamp":        time.Now().Format(time.RFC3339),
			"circuit_breakers": stats["circuit_breakers"],
			"processor_health": healthStatus,
			"recommendations":  h.generateCircuitBreakerRecommendations(stats),
			"integrated_stats": stats,
		}

		ctx.SetContentType("application/json")
		ctx.SetStatusCode(fasthttp.StatusOK)
		jsonResp, _ := json.Marshal(response)
		ctx.SetBody(jsonResp)
	} else {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"error": "Circuit breaker stats not available"}`)
	}
}

// generateCircuitBreakerRecommendations provides optimization suggestions
func (h *HealthHandler) generateCircuitBreakerRecommendations(stats map[string]interface{}) []string {
	recommendations := make([]string, 0)

	for processorName, processorStats := range stats {
		if processorMap, ok := processorStats.(map[string]interface{}); ok {
			state := processorMap["state"].(string)
			failureCount := int(processorMap["failure_count"].(float64))
			timeToResetMs := int64(processorMap["time_to_reset_ms"].(float64))

			switch state {
			case "open":
				if timeToResetMs > 1000 { // Mais de 1 segundo
					recommendations = append(recommendations,
						fmt.Sprintf("%s: Circuit breaker aberto há muito tempo (%dms). Considere reduzir reset_timeout",
							processorName, timeToResetMs))
				}
				if failureCount > 5 {
					recommendations = append(recommendations,
						fmt.Sprintf("%s: Muitas falhas (%d). Verifique a saúde do serviço",
							processorName, failureCount))
				}
			case "half-open":
				recommendations = append(recommendations,
					fmt.Sprintf("%s: Em estado half-open. Monitorar próximas tentativas", processorName))
			case "closed":
				if failureCount > 0 {
					recommendations = append(recommendations,
						fmt.Sprintf("%s: %d falhas recentes, mas circuit breaker fechado. Performance OK",
							processorName, failureCount))
				}
			}
		}
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "Todos os circuit breakers estão funcionando normalmente")
	}

	return recommendations
}
