package services

import (
	"context"

	"rinha-backend-clean/internal/domain/entities"
)

// QueueService defines the interface for payment queue operations
type QueueService interface {
	// Enqueue adds a payment to the processing queue
	Enqueue(ctx context.Context, payment *entities.Payment) error

	// StartProcessing starts the background payment processing
	StartProcessing(ctx context.Context, processor PaymentProcessorService, repository PaymentRepository) error

	// GetQueueSize returns the current queue size
	GetQueueSize() int

	// Stop stops the queue processing
	Stop(ctx context.Context) error
}

// PaymentRepository is imported here to avoid circular dependency
type PaymentRepository interface {
	UpdateSummary(ctx context.Context, processorType entities.ProcessorType, amount float64) error
	Update(ctx context.Context, payment *entities.Payment) error
}
