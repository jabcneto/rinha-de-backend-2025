package usecases

import (
	"context"
	"rinha-backend-clean/internal/infrastructure/logger"

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

	if req.From != nil || req.To != nil {
		logger.GetLogger().Info("Filtros de data detectados, consultando banco de dados diretamente")
		summary, err := uc.paymentRepo.GetSummary(ctx, filter)
		if err != nil {
			logger.GetLogger().Errorf("Erro ao obter resumo de pagamentos com filtros: %v", err)
			return nil, err
		}

		// Convert to response DTO
		return &dtos.PaymentSummaryResponse{
			Default: dtos.ProcessorSummaryResponse{
				TotalRequests: summary.Default.TotalRequests,
				TotalAmount:   summary.Default.TotalAmount,
			},
			Fallback: dtos.ProcessorSummaryResponse{
				TotalRequests: summary.Fallback.TotalRequests,
				TotalAmount:   summary.Fallback.TotalAmount,
			},
		}, nil
	}

	// No filters, try to get from cache first (non-blocking)
	summary, err := uc.paymentRepo.GetSummaryFromCache(ctx)
	if err != nil {
		// If cache fails, fallback to database (may block)
		logger.GetLogger().Warnf("Cache miss, consultando banco de dados: %v", err)
		summary, err = uc.paymentRepo.GetSummary(ctx, filter)
		if err != nil {
			logger.GetLogger().Errorf("Erro ao obter resumo de pagamentos: %v", err)
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

	logger.GetLogger().Infof("Resumo de pagamentos retornado - Default: %d req / R$ %.2f, Fallback: %d req / R$ %.2f",
		response.Default.TotalRequests, response.Default.TotalAmount,
		response.Fallback.TotalRequests, response.Fallback.TotalAmount)

	return response, nil
}
