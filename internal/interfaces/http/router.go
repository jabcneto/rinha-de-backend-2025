package http

import (
	"github.com/valyala/fasthttp"
	"rinha-backend-clean/internal/interfaces/http/handlers"
)

// Router sets up HTTP routes
type Router struct {
	paymentHandler *handlers.PaymentHandler
	healthHandler  *handlers.HealthHandler
}

// NewRouter cria um novo roteador fasthttp
func NewRouter(paymentHandler *handlers.PaymentHandler, healthHandler *handlers.HealthHandler) *Router {
	return &Router{
		paymentHandler: paymentHandler,
		healthHandler:  healthHandler,
	}
}

// SetupRoutes configura todas as rotas HTTP usando fasthttp
func (rt *Router) Handler(ctx *fasthttp.RequestCtx) {
	switch string(ctx.Path()) {
	case "/payments":
		if string(ctx.Method()) == fasthttp.MethodPost {
			rt.paymentHandler.HandlePayments(ctx)
			return
		}
	case "/payments-summary":
		if string(ctx.Method()) == fasthttp.MethodGet {
			rt.paymentHandler.HandlePaymentsSummary(ctx)
			return
		}
	case "/purge-payments":
		if string(ctx.Method()) == fasthttp.MethodPost {
			rt.paymentHandler.HandlePurgePayments(ctx)
			return
		}
	case "/health":
		if string(ctx.Method()) == fasthttp.MethodGet {
			rt.healthHandler.HandleHealth(ctx)
			return
		}
	case "/scaling-metrics":
		if string(ctx.Method()) == fasthttp.MethodGet {
			rt.healthHandler.HandleScalingMetrics(ctx)
			return
		}
	case "/circuit-breakers":
		if string(ctx.Method()) == fasthttp.MethodGet {
			rt.healthHandler.HandleCircuitBreakerStatus(ctx)
			return
		}
	}
	ctx.SetStatusCode(fasthttp.StatusNotFound)
	ctx.SetBodyString(`{"error": "Not Found"}`)
}
