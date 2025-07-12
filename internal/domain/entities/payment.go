package entities

import (
	"time"

	"github.com/google/uuid"
)

// Payment represents a payment entity in the domain
type Payment struct {
	ID            uuid.UUID
	CorrelationID uuid.UUID
	Amount        float64
	Status        PaymentStatus
	ProcessorType ProcessorType
	RequestedAt   time.Time
	ProcessedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// Retry control fields
	RetryCount  int
	NextRetryAt *time.Time
	LastError   string
}

// PaymentStatus represents the status of a payment
type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "pending"
	PaymentStatusProcessed PaymentStatus = "processed"
	PaymentStatusFailed    PaymentStatus = "failed"
	PaymentStatusDiscarded PaymentStatus = "discarded"
)

// ProcessorType represents the type of payment processor
type ProcessorType string

const (
	ProcessorTypeDefault  ProcessorType = "default"
	ProcessorTypeFallback ProcessorType = "fallback"
)

// NewPayment creates a new payment entity
func NewPayment(correlationID uuid.UUID, amount float64) *Payment {
	now := time.Now()
	return &Payment{
		ID:            uuid.New(),
		CorrelationID: correlationID,
		Amount:        amount,
		Status:        PaymentStatusPending,
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// MarkAsProcessed marks the payment as processed
func (p *Payment) MarkAsProcessed(processorType ProcessorType) {
	now := time.Now()
	p.Status = PaymentStatusProcessed
	p.ProcessorType = processorType
	p.ProcessedAt = &now
	p.UpdatedAt = now
}

// MarkAsFailed marks the payment as failed
func (p *Payment) MarkAsFailed() {
	p.Status = PaymentStatusFailed
	p.UpdatedAt = time.Now()
}

// MarkAsDiscarded marks the payment as failed (descartado)
func (p *Payment) MarkAsDiscarded() {
	p.Status = PaymentStatusFailed
	p.UpdatedAt = time.Now()
}

// IsValid validates the payment entity
func (p *Payment) IsValid() bool {
	return p.CorrelationID != uuid.Nil && p.Amount > 0
}

// MarkForRetry marks the payment for retry with exponential backoff
func (p *Payment) MarkForRetry(err error, maxRetries int) bool {
	p.RetryCount++
	p.LastError = err.Error()
	p.UpdatedAt = time.Now()

	if p.RetryCount > maxRetries {
		p.MarkAsDiscarded()
		return false
	}

	// Exponential backoff: 2^retryCount seconds
	backoffSeconds := 1 << p.RetryCount // 2, 4, 8, 16, 32 seconds
	nextRetry := time.Now().Add(time.Duration(backoffSeconds) * time.Second)
	p.NextRetryAt = &nextRetry

	return true
}

// IsReadyForRetry checks if the payment is ready for retry
func (p *Payment) IsReadyForRetry() bool {
	return p.NextRetryAt != nil && time.Now().After(*p.NextRetryAt)
}

// ResetRetryInfo resets retry information when payment is successful
func (p *Payment) ResetRetryInfo() {
	p.RetryCount = 0
	p.NextRetryAt = nil
	p.LastError = ""
}
