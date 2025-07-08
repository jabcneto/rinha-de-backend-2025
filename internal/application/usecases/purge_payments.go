package usecases

import (
	"context"

	"rinha-backend-clean/internal/domain/repositories"
	"rinha-backend-clean/internal/infrastructure/logger"
)

// PurgePaymentsUseCase handles payment data purging
type PurgePaymentsUseCase struct {
	paymentRepo repositories.PaymentRepository
}

// NewPurgePaymentsUseCase creates a new purge payments use case
func NewPurgePaymentsUseCase(paymentRepo repositories.PaymentRepository) *PurgePaymentsUseCase {
	return &PurgePaymentsUseCase{
		paymentRepo: paymentRepo,
	}
}

// Execute purges all payment data
func (uc *PurgePaymentsUseCase) Execute(ctx context.Context) error {
	err := uc.paymentRepo.PurgeSummary(ctx)
	if err != nil {
		logger.Errorf("Erro ao limpar dados de pagamentos: %v", err)
		return err
	}

	logger.Info("Dados de pagamentos limpos com sucesso")
	return nil
}
