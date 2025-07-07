package usecases

import (
	"context"
	"log"
	"rinha-backend-clean/internal/application/dtos"
	"rinha-backend-clean/internal/domain/entities"
	"rinha-backend-clean/internal/domain/repositories"
	"rinha-backend-clean/internal/domain/services"

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
		return nil, err
	}
	// Parse correlation ID
	correlationID, err := uuid.Parse(req.CorrelationID)
	if err != nil {
		return nil, dtos.ErrInvalidCorrelationID
	}

	// Create payment entity
	payment := entities.NewPayment(correlationID, req.Amount)

	// Enqueue payment for async processing
	if err := uc.queueService.Enqueue(ctx, payment); err != nil {
		log.Printf("Erro ao enfileirar pagamento %s: %v", payment.CorrelationID, err)
		return nil, err
	}

	log.Printf("Pagamento recebido e enfileirado: %s - R$ %.2f", payment.CorrelationID, payment.Amount)

	// Return success response
	return &dtos.PaymentResponse{
		Message:       "payment received and queued for processing",
		CorrelationID: req.CorrelationID,
	}, nil
}
