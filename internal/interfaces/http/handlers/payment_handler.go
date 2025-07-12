package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/valyala/fasthttp"
	"rinha-backend-clean/internal/application/dtos"
	"rinha-backend-clean/internal/application/usecases"
	"rinha-backend-clean/internal/infrastructure/logger"
)

// PaymentHandler handles payment-related HTTP requests
type PaymentHandler struct {
	processPaymentUC    *usecases.ProcessPaymentUseCase
	getPaymentSummaryUC *usecases.GetPaymentSummaryUseCase
	purgePaymentsUC     *usecases.PurgePaymentsUseCase
}

// NewPaymentHandler creates a new payment handler
func NewPaymentHandler(
	processPaymentUC *usecases.ProcessPaymentUseCase,
	getPaymentSummaryUC *usecases.GetPaymentSummaryUseCase,
	purgePaymentsUC *usecases.PurgePaymentsUseCase,
) *PaymentHandler {
	return &PaymentHandler{
		processPaymentUC:    processPaymentUC,
		getPaymentSummaryUC: getPaymentSummaryUC,
		purgePaymentsUC:     purgePaymentsUC,
	}
}

// HandlePayments handles POST /payments requests
func (h *PaymentHandler) HandlePayments(ctx *fasthttp.RequestCtx) {
	// Set response headers early for better performance
	ctx.SetContentType("application/json")

	var req dtos.PaymentRequest

	// Parse JSON request with timeout protection
	body := ctx.PostBody()
	if len(body) == 0 {
		h.writeErrorResponse(ctx, "Empty request body", fasthttp.StatusBadRequest)
		return
	}

	if len(body) > 1024*10 { // 10KB limit para evitar abuse
		h.writeErrorResponse(ctx, "Request body too large", fasthttp.StatusRequestEntityTooLarge)
		return
	}

	// Parse JSON request
	if err := json.Unmarshal(body, &req); err != nil {
		logger.WithField("error", err.Error()).
			WithField("path", string(ctx.Path())).
			Debug("Invalid JSON in payment request") // Mudou de Warn para Debug para reduzir logs
		h.writeErrorResponse(ctx, "Invalid JSON", fasthttp.StatusBadRequest)
		return
	}

	// Execute use case with context timeout
	response, err := h.processPaymentUC.Execute(ctx, &req)
	if err != nil {
		var validationErr *dtos.ValidationError
		if errors.As(err, &validationErr) {
			logger.WithField("validation_error", validationErr.Message).
				WithField("payment_id", req.CorrelationID).
				Debug("Payment validation failed") // Mudou de Warn para Debug
			h.writeErrorResponse(ctx, validationErr.Message, fasthttp.StatusBadRequest)
			return
		}

		logger.WithError(err).
			WithField("payment_id", req.CorrelationID).
			Error("Failed to process payment")
		h.writeErrorResponse(ctx, "Internal server error", fasthttp.StatusInternalServerError)
		return
	}

	// Return success response - removido log verbose
	h.writeJSONResponse(ctx, response, fasthttp.StatusAccepted)
}

// HandlePaymentsSummary handles GET /payments-summary requests
func (h *PaymentHandler) HandlePaymentsSummary(ctx *fasthttp.RequestCtx) {
	// Parse query parameters
	req := &dtos.PaymentSummaryRequest{}

	fromStr := string(ctx.QueryArgs().Peek("from"))
	if fromStr != "" {
		if parsed, err := h.parseTimestamp(fromStr); err != nil {
			logger.WithField("timestamp", fromStr).
				WithField("error", err.Error()).
				Warn("Invalid 'from' timestamp format")
			h.writeErrorResponse(ctx, "Invalid 'from' timestamp format. Use yyyy-MM-ddTHH:mm:ss or ISO format.", fasthttp.StatusBadRequest)
			return
		} else {
			req.From = &parsed
		}
	}

	toStr := string(ctx.QueryArgs().Peek("to"))
	if toStr != "" {
		if parsed, err := h.parseTimestamp(toStr); err != nil {
			logger.WithField("timestamp", toStr).
				WithField("error", err.Error()).
				Warn("Invalid 'to' timestamp format")
			h.writeErrorResponse(ctx, "Invalid 'to' timestamp format. Use yyyy-MM-ddTHH:mm:ss or ISO format.", fasthttp.StatusBadRequest)
			return
		} else {
			req.To = &parsed
		}
	}

	// Execute use case (optimized with cache)
	response, err := h.getPaymentSummaryUC.Execute(ctx, req)
	if err != nil {
		logger.WithError(err).Error("Failed to get payment summary")
		h.writeErrorResponse(ctx, "Internal server error", fasthttp.StatusInternalServerError)
		return
	}

	// Return JSON response
	logger.Debug("Payment summary retrieved successfully")
	h.writeJSONResponse(ctx, response, fasthttp.StatusOK)
}

// HandlePurgePayments handles POST /purge-payments requests
func (h *PaymentHandler) HandlePurgePayments(ctx *fasthttp.RequestCtx) {
	// Execute use case
	if err := h.purgePaymentsUC.Execute(ctx); err != nil {
		logger.WithError(err).Error("Failed to purge payments")
		h.writeErrorResponse(ctx, "Internal server error", fasthttp.StatusInternalServerError)
		return
	}

	// Return success response
	logger.Info("All payments purged successfully")
	response := map[string]string{"message": "All payments purged."}
	h.writeJSONResponse(ctx, response, fasthttp.StatusOK)
}

// writeJSONResponse writes a JSON response
func (h *PaymentHandler) writeJSONResponse(ctx *fasthttp.RequestCtx, data interface{}, statusCode int) {
	ctx.SetStatusCode(statusCode)
	ctx.SetContentType("application/json")
	jsonBytes, _ := json.Marshal(data)
	ctx.SetBody(jsonBytes)
}

// writeErrorResponse writes an error response
func (h *PaymentHandler) writeErrorResponse(ctx *fasthttp.RequestCtx, message string, statusCode int) {
	ctx.SetStatusCode(statusCode)
	ctx.SetContentType("application/json")
	jsonBytes, _ := json.Marshal(map[string]string{"error": message})
	ctx.SetBody(jsonBytes)
}

// parseTimestamp attempts to parse timestamp in multiple formats
func (h *PaymentHandler) parseTimestamp(timeStr string) (time.Time, error) {
	// Try different formats
	formats := []string{
		"2006-01-02T15:04:05",      // yyyy-MM-ddTHH:mm:ss
		"2006-01-02T15:04:05.000Z", // ISO with milliseconds and Z
		"2006-01-02T15:04:05Z",     // ISO without milliseconds but with Z
		time.RFC3339,               // RFC3339 format
		time.RFC3339Nano,           // RFC3339 with nanoseconds
	}

	for _, format := range formats {
		if parsed, err := time.Parse(format, timeStr); err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported timestamp format: %s", timeStr)
}
