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
}

// PaymentStatus represents the status of a payment
type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "pending"
	PaymentStatusProcessed PaymentStatus = "processed"
	PaymentStatusFailed    PaymentStatus = "failed"
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

// IsValid validates the payment entity
func (p *Payment) IsValid() bool {
	return p.CorrelationID != uuid.Nil && p.Amount > 0
}
