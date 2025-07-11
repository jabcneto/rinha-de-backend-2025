package entities

import "time"

// PaymentSummary represents aggregated payment data
type PaymentSummary struct {
	Default  ProcessorSummary `json:"default"`
	Fallback ProcessorSummary `json:"fallback"`
}

// ProcessorSummary represents summary data for a specific processor
type ProcessorSummary struct {
	TotalRequests int64   `json:"totalRequests"`
	TotalAmount   float64 `json:"totalAmount"`
}

// PaymentSummaryFilter represents filters for payment summary queries
type PaymentSummaryFilter struct {
	From *time.Time
	To   *time.Time
}

// NewPaymentSummary creates a new payment summary with zero values
func NewPaymentSummary() *PaymentSummary {
	return &PaymentSummary{
		Default: ProcessorSummary{
			TotalRequests: 0,
			TotalAmount:   0.0,
		},
		Fallback: ProcessorSummary{
			TotalRequests: 0,
			TotalAmount:   0.0,
		},
	}
}

// AddPayment adds a payment to the summary
func (ps *PaymentSummary) AddPayment(processorType ProcessorType, amount float64) {
	switch processorType {
	case ProcessorTypeDefault:
		ps.Default.TotalRequests++
		ps.Default.TotalAmount += amount
	case ProcessorTypeFallback:
		ps.Fallback.TotalRequests++
		ps.Fallback.TotalAmount += amount
	}
}

// Reset resets all summary data to zero
func (ps *PaymentSummary) Reset() {
	ps.Default.TotalRequests = 0
	ps.Default.TotalAmount = 0.0
	ps.Fallback.TotalRequests = 0
	ps.Fallback.TotalAmount = 0.0
}
