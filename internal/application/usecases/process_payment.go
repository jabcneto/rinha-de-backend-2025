package usecases

import (
	"context"
	"rinha-backend-clean/internal/application/dtos"
	"rinha-backend-clean/internal/domain/entities"
	"rinha-backend-clean/internal/domain/repositories"
	"rinha-backend-clean/internal/domain/services"
	"rinha-backend-clean/internal/infrastructure/logger"

	"github.com/google/uuid"
)

// ProcessPaymentUseCase handles payment processing business logic
type ProcessPaymentUseCase struct {
	paymentRepo  repositories.PaymentRepository
	queueService services.QueueService
}

// NewProcessPaymentUseCase creates a new process payment use case
func NewProcessPaymentUseCase(
	paymentRepo repositories.PaymentRepository,
	queueService services.QueueService,
) *ProcessPaymentUseCase {
	return &ProcessPaymentUseCase{
		paymentRepo:  paymentRepo,
		queueService: queueService,
	}
}

// Execute processes a payment request
func (uc *ProcessPaymentUseCase) Execute(ctx context.Context, req *dtos.PaymentRequest) (*dtos.PaymentResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		logger.WithField("correlation_id", req.CorrelationID).
			WithField("validation_error", err.Error()).
			Warn("Payment validation failed")
		return nil, err
	}

	// Parse correlation ID
	correlationID, err := uuid.Parse(req.CorrelationID)
	if err != nil {
		logger.WithField("correlation_id", req.CorrelationID).
			WithError(err).
			Error("Invalid correlation ID format")
		return nil, dtos.ErrInvalidCorrelationID
	}

	// Create payment entity
	payment := entities.NewPayment(correlationID, req.Amount)

	// Save payment immediately to database with PENDING status
	if err := uc.paymentRepo.Save(ctx, payment); err != nil {
		logger.WithField("correlation_id", payment.CorrelationID).
			WithField("amount", payment.Amount).
			WithError(err).
			Error("Failed to save payment to database")
		return nil, err
	}

	// Enqueue payment for async processing
	if err := uc.queueService.Enqueue(ctx, payment); err != nil {
		logger.WithField("correlation_id", payment.CorrelationID).
			WithField("amount", payment.Amount).
			WithError(err).
			Error("Failed to enqueue payment for processing")
		// Even if enqueue fails, payment is already saved in DB
		return nil, err
	}

	logger.WithField("correlation_id", payment.CorrelationID).
		WithField("amount", payment.Amount).
		WithField("status", "queued").
		Info("Payment saved and queued for processing")

	// Return success response
	return &dtos.PaymentResponse{
		Message:       "payment received and queued for processing",
		CorrelationID: req.CorrelationID,
	}, nil
}
