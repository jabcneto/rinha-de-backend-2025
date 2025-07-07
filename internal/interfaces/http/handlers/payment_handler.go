package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"rinha-backend-clean/internal/application/dtos"
	"rinha-backend-clean/internal/application/usecases"
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
func (h *PaymentHandler) HandlePayments(w http.ResponseWriter, r *http.Request) {
	var req dtos.PaymentRequest

	// Parse JSON request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Execute use case
	response, err := h.processPaymentUC.Execute(r.Context(), &req)
	if err != nil {
		if validationErr, ok := err.(*dtos.ValidationError); ok {
			h.writeErrorResponse(w, validationErr.Message, http.StatusBadRequest)
			return
		}
		h.writeErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Return success response
	h.writeJSONResponse(w, response, http.StatusAccepted)
}

// HandlePaymentsSummary handles GET /payments-summary requests
func (h *PaymentHandler) HandlePaymentsSummary(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	req := &dtos.PaymentSummaryRequest{}

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if parsed, err := time.Parse("2006-01-02T15:04:05", fromStr); err != nil {
			h.writeErrorResponse(w, "Invalid 'from' timestamp format. Use yyyy-MM-ddTHH:mm:ss format.", http.StatusBadRequest)
			return
		} else {
			req.From = &parsed
		}
	}

	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if parsed, err := time.Parse("2006-01-02T15:04:05", toStr); err != nil {
			h.writeErrorResponse(w, "Invalid 'to' timestamp format. Use yyyy-MM-ddTHH:mm:ss format.", http.StatusBadRequest)
			return
		} else {
			req.To = &parsed
		}
	}

	// Execute use case (optimized with cache)
	response, err := h.getPaymentSummaryUC.Execute(r.Context(), req)
	if err != nil {
		h.writeErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Return JSON response
	h.writeJSONResponse(w, response, http.StatusOK)
}

// HandlePurgePayments handles POST /purge-payments requests
func (h *PaymentHandler) HandlePurgePayments(w http.ResponseWriter, r *http.Request) {
	// Execute use case
	if err := h.purgePaymentsUC.Execute(r.Context()); err != nil {
		h.writeErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Return success response
	response := map[string]string{"message": "All payments purged."}
	h.writeJSONResponse(w, response, http.StatusOK)
}

// writeJSONResponse writes a JSON response
func (h *PaymentHandler) writeJSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// writeErrorResponse writes an error response
func (h *PaymentHandler) writeErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
