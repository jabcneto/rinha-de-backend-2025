package dtos

import (
	"time"

	"github.com/google/uuid"
)

// PaymentRequest represents a payment request from the API
type PaymentRequest struct {
	CorrelationID string  `json:"correlationId"`
	Amount        float64 `json:"amount"`
}

// PaymentResponse represents a payment response to the API
type PaymentResponse struct {
	Message       string `json:"message"`
	CorrelationID string `json:"correlationId"`
}

// PaymentSummaryRequest represents a request for payment summary
type PaymentSummaryRequest struct {
	From *time.Time
	To   *time.Time
}

// PaymentSummaryResponse represents the payment summary response
type PaymentSummaryResponse struct {
	Default  ProcessorSummaryResponse `json:"default"`
	Fallback ProcessorSummaryResponse `json:"fallback"`
}

// ProcessorSummaryResponse represents summary data for a processor
type ProcessorSummaryResponse struct {
	TotalRequests int64   `json:"totalRequests"`
	TotalAmount   float64 `json:"totalAmount"`
}

// Validate validates the payment request
func (pr *PaymentRequest) Validate() error {
	if pr.CorrelationID == "" {
		return ErrCorrelationIDRequired
	}

	if _, err := uuid.Parse(pr.CorrelationID); err != nil {
		return ErrInvalidCorrelationID
	}

	if pr.Amount <= 0 {
		return ErrInvalidAmount
	}

	return nil
}

// Custom errors
var (
	ErrCorrelationIDRequired = &ValidationError{Message: "correlationId is required"}
	ErrInvalidCorrelationID  = &ValidationError{Message: "correlationId must be a valid UUID"}
	ErrInvalidAmount         = &ValidationError{Message: "amount must be greater than 0"}
)

// ValidationError represents a validation error
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}
