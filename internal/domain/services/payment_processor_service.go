package services

import (
	"context"

	"rinha-backend-clean/internal/domain/entities"
)

// PaymentProcessorService defines the interface for payment processing
type PaymentProcessorService interface {
	// ProcessPayment processes a payment through the appropriate processor
	ProcessPayment(ctx context.Context, payment *entities.Payment) (entities.ProcessorType, error)

	// ProcessWithDefault processes a payment specifically with the default processor
	ProcessWithDefault(ctx context.Context, payment *entities.Payment) error

	// ProcessWithFallback processes a payment specifically with the fallback processor
	ProcessWithFallback(ctx context.Context, payment *entities.Payment) error

	// GetHealthStatus returns the health status of processors
	GetHealthStatus(ctx context.Context) (*ProcessorHealthStatus, error)

	// StartHealthChecks starts background health checking
	StartHealthChecks(ctx context.Context)
}

// ProcessorHealthStatus represents the health status of payment processors
type ProcessorHealthStatus struct {
	Default  ProcessorHealth `json:"default"`
	Fallback ProcessorHealth `json:"fallback"`
}

// ProcessorHealth represents health information for a single processor
type ProcessorHealth struct {
	IsHealthy       bool  `json:"isHealthy"`
	MinResponseTime int64 `json:"minResponseTime"`
	LastChecked     int64 `json:"lastChecked"`
}
