package services

import (
	"context"

	"github.com/google/uuid"
	"rinha-backend-clean/internal/domain/entities"
)

// QueueService defines the interface for payment queue operations
type QueueService interface {
	// Enqueue adds a payment to the processing queue
	Enqueue(ctx context.Context, payment *entities.Payment) error

	// StartProcessing starts the background payment processing
	StartProcessing(ctx context.Context, processor PaymentProcessorService, repository PaymentRepository) error

	// SetRetryQueueService sets the retry queue service for handling failed payments
	SetRetryQueueService(retryQueueService RetryQueueService)

	// GetQueueSize returns the current queue size
	GetQueueSize() int

	// Stop stops the queue processing
	Stop(ctx context.Context) error
}

// PaymentRepository is imported here to avoid circular dependency
type PaymentRepository interface {
	// Save saves a payment to the repository
	Save(ctx context.Context, payment *entities.Payment) error
	// UpdateSummary updates the payment summary
	UpdateSummary(ctx context.Context, processorType entities.ProcessorType, amount float64) error
	// Update updates an existing payment
	Update(ctx context.Context, payment *entities.Payment) error
	// FindByCorrelationID finds a payment by its correlation ID
	FindByCorrelationID(ctx context.Context, correlationID uuid.UUID) (*entities.Payment, error)
}
