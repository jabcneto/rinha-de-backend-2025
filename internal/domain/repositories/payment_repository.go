package repositories

import (
	"context"

	"rinha-backend-clean/internal/domain/entities"

	"github.com/google/uuid"
)

// PaymentRepository defines the interface for payment data access
type PaymentRepository interface {
	// Save saves a payment to the repository
	Save(ctx context.Context, payment *entities.Payment) error

	// FindByCorrelationID finds a payment by its correlation ID
	FindByCorrelationID(ctx context.Context, correlationID uuid.UUID) (*entities.Payment, error)

	// Update updates an existing payment
	Update(ctx context.Context, payment *entities.Payment) error

	// GetSummary returns payment summary with optional time filters
	GetSummary(ctx context.Context, filter *entities.PaymentSummaryFilter) (*entities.PaymentSummary, error)

	// UpdateSummary updates the payment summary (for performance optimization)
	UpdateSummary(ctx context.Context, processorType entities.ProcessorType, amount float64) error

	// PurgeSummary resets all summary data
	PurgeSummary(ctx context.Context) error

	// GetSummaryFromCache returns cached summary data (non-blocking)
	GetSummaryFromCache(ctx context.Context) (*entities.PaymentSummary, error)
}
