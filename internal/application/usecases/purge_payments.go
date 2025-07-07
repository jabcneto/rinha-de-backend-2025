package usecases

import (
	"context"
	"log"

	"rinha-backend-clean/internal/domain/repositories"
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
		log.Printf("Erro ao limpar dados de pagamentos: %v", err)
		return err
	}

	log.Println("Dados de pagamentos limpos com sucesso")
	return nil
}
