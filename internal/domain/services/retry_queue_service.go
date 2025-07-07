package services

import (
	"context"
	"rinha-backend-clean/internal/domain/entities"
)

// RetryQueueService defines the interface for retry queue operations
type RetryQueueService interface {
	// EnqueueForDefaultRetry enqueues a payment for retry with default processor
	EnqueueForDefaultRetry(ctx context.Context, payment *entities.Payment) error

	// EnqueueForFallbackRetry enqueues a payment for retry with fallback processor
	EnqueueForFallbackRetry(ctx context.Context, payment *entities.Payment) error

	// EnqueueForPermanentRetry enqueues a payment for permanent retry with longer backoff
	EnqueueForPermanentRetry(ctx context.Context, payment *entities.Payment) error

	// StartRetryProcessing starts the retry processing workers
	StartRetryProcessing(processor PaymentProcessorService, repository PaymentRepository) error

	// Stop stops all retry processing
	Stop() error

	// GetQueueStats returns statistics about the retry queues
	GetQueueStats() map[string]interface{}
}
