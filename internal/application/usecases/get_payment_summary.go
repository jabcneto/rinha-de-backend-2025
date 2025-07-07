package usecases

import (
	"context"
	"log"

	"rinha-backend-clean/internal/application/dtos"
	"rinha-backend-clean/internal/domain/entities"
	"rinha-backend-clean/internal/domain/repositories"
)

// GetPaymentSummaryUseCase handles payment summary retrieval
type GetPaymentSummaryUseCase struct {
	paymentRepo repositories.PaymentRepository
}

// NewGetPaymentSummaryUseCase creates a new get payment summary use case
func NewGetPaymentSummaryUseCase(paymentRepo repositories.PaymentRepository) *GetPaymentSummaryUseCase {
	return &GetPaymentSummaryUseCase{
		paymentRepo: paymentRepo,
	}
}

// Execute retrieves payment summary with optional time filters
func (uc *GetPaymentSummaryUseCase) Execute(ctx context.Context, req *dtos.PaymentSummaryRequest) (*dtos.PaymentSummaryResponse, error) {
	// Create filter from request
	filter := &entities.PaymentSummaryFilter{
		From: req.From,
		To:   req.To,
	}

	// Try to get from cache first (non-blocking)
	summary, err := uc.paymentRepo.GetSummaryFromCache(ctx)
	if err != nil {
		// If cache fails, fallback to database (may block)
		log.Printf("Cache miss, consultando banco de dados: %v", err)
		summary, err = uc.paymentRepo.GetSummary(ctx, filter)
		if err != nil {
			log.Printf("Erro ao obter resumo de pagamentos: %v", err)
			return nil, err
		}
	}

	// Convert to response DTO
	response := &dtos.PaymentSummaryResponse{
		Default: dtos.ProcessorSummaryResponse{
			TotalRequests: summary.Default.TotalRequests,
			TotalAmount:   summary.Default.TotalAmount,
		},
		Fallback: dtos.ProcessorSummaryResponse{
			TotalRequests: summary.Fallback.TotalRequests,
			TotalAmount:   summary.Fallback.TotalAmount,
		},
	}

	log.Printf("Resumo de pagamentos retornado - Default: %d req / R$ %.2f, Fallback: %d req / R$ %.2f",
		response.Default.TotalRequests, response.Default.TotalAmount,
		response.Fallback.TotalRequests, response.Fallback.TotalAmount)

	return response, nil
}
