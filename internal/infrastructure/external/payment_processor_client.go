package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"rinha-backend-clean/internal/domain/entities"
	"rinha-backend-clean/internal/domain/services"
	"rinha-backend-clean/internal/infrastructure/logger"
)

// PaymentProcessorClient implements the PaymentProcessorService interface
type PaymentProcessorClient struct {
	client *http.Client
	mutex  sync.RWMutex

	defaultProcessor struct {
		url             string
		isFailing       bool
		minResponseTime int64
		lastCheck       time.Time
		circuitBreaker  *CircuitBreaker
	}
	fallbackProcessor struct {
		url             string
		isFailing       bool
		minResponseTime int64
		lastCheck       time.Time
		circuitBreaker  *CircuitBreaker
	}
}

// PaymentProcessorRequest represents a request to payment processor
type PaymentProcessorRequest struct {
	CorrelationID string    `json:"correlationId"`
	Amount        float64   `json:"amount"`
	RequestedAt   time.Time `json:"requestedAt"`
}

// PaymentProcessorResponse represents a response from payment processor
type PaymentProcessorResponse struct {
	Message string `json:"message"`
}

// HealthCheckResponse represents health check response
type HealthCheckResponse struct {
	Failing         bool  `json:"failing"`
	MinResponseTime int64 `json:"minResponseTime"`
}

var (
	HealthCheckInterval        = 100 * time.Millisecond
	CircuitBreakerMaxFailures  = 10
	CircuitBreakerResetTimeout = 100 * time.Millisecond
)

const (
	HealthCheckPath = "/payments/service-health"
	PaymentsPath    = "/payments"
)

// NewPaymentProcessorClient creates a new payment processor client
func NewPaymentProcessorClient() services.PaymentProcessorService {
	// Get URLs from environment variables with defaults
	defaultURL := os.Getenv("PAYMENT_PROCESSOR_URL_DEFAULT")
	if defaultURL == "" {
		defaultURL = "http://payment-processor-default:8080"
	}

	fallbackURL := os.Getenv("PAYMENT_PROCESSOR_URL_FALLBACK")
	if fallbackURL == "" {
		fallbackURL = "http://payment-processor-fallback:8080"
	}

	// Create optimized HTTP client with connection pooling
	transport := &http.Transport{
		MaxIdleConns:          100,                    // Pool de conexões idle
		MaxIdleConnsPerHost:   50,                     // Por host
		MaxConnsPerHost:       100,                    // Máximo de conexões por host
		IdleConnTimeout:       100 * time.Millisecond, // Timeout para conexões idle
		TLSHandshakeTimeout:   100 * time.Millisecond,
		ExpectContinueTimeout: 100 * time.Millisecond,
		DisableKeepAlives:     false, // Manter conexões vivas
		DisableCompression:    true,  // Desabilitar compressão para performance
	}

	client := &http.Client{
		Timeout:   100 * time.Millisecond, // Reduzido para 100ms
		Transport: transport,
	}

	return &PaymentProcessorClient{
		client: client,
		defaultProcessor: struct {
			url             string
			isFailing       bool
			minResponseTime int64
			lastCheck       time.Time
			circuitBreaker  *CircuitBreaker
		}{
			url:            defaultURL,
			circuitBreaker: NewCircuitBreaker("default-processor", CircuitBreakerMaxFailures, CircuitBreakerResetTimeout),
		},
		fallbackProcessor: struct {
			url             string
			isFailing       bool
			minResponseTime int64
			lastCheck       time.Time
			circuitBreaker  *CircuitBreaker
		}{
			url:            fallbackURL,
			circuitBreaker: NewCircuitBreaker("fallback-processor", CircuitBreakerMaxFailures, CircuitBreakerResetTimeout),
		},
	}
}

// ProcessPayment processes a payment through the appropriate processor
func (c *PaymentProcessorClient) ProcessPayment(ctx context.Context, payment *entities.Payment) (entities.ProcessorType, error) {
	// Create processor request
	request := &PaymentProcessorRequest{
		CorrelationID: payment.CorrelationID.String(),
		Amount:        payment.Amount,
		RequestedAt:   payment.RequestedAt,
	}

	// Try default processor first
	err := c.defaultProcessor.circuitBreaker.Execute(func() error {
		return c.sendPaymentToURL(ctx, c.defaultProcessor.url, request)
	})

	if err == nil {
		return entities.ProcessorTypeDefault, nil
	}

	logger.Infof("Falha ao enviar para o processador default: %v. Tentando fallback...", err)

	// Try fallback processor
	err = c.fallbackProcessor.circuitBreaker.Execute(func() error {
		return c.sendPaymentToURL(ctx, c.fallbackProcessor.url, request)
	})

	if err == nil {
		return entities.ProcessorTypeFallback, nil
	}

	return "", fmt.Errorf("falha ao enviar pagamento para ambos os processadores: %w", err)
}

// ProcessWithDefault processes a payment specifically with the default processor
func (c *PaymentProcessorClient) ProcessWithDefault(ctx context.Context, payment *entities.Payment) error {
	request := &PaymentProcessorRequest{
		CorrelationID: payment.CorrelationID.String(),
		Amount:        payment.Amount,
		RequestedAt:   payment.RequestedAt,
	}

	return c.defaultProcessor.circuitBreaker.Execute(func() error {
		return c.sendPaymentToURL(ctx, c.defaultProcessor.url, request)
	})
}

// ProcessWithFallback processes a payment specifically with the fallback processor
func (c *PaymentProcessorClient) ProcessWithFallback(ctx context.Context, payment *entities.Payment) error {
	request := &PaymentProcessorRequest{
		CorrelationID: payment.CorrelationID.String(),
		Amount:        payment.Amount,
		RequestedAt:   payment.RequestedAt,
	}

	return c.fallbackProcessor.circuitBreaker.Execute(func() error {
		return c.sendPaymentToURL(ctx, c.fallbackProcessor.url, request)
	})
}

// sendPaymentToURL sends payment to a specific processor URL
func (c *PaymentProcessorClient) sendPaymentToURL(ctx context.Context, targetURL string, request *PaymentProcessorRequest) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("erro ao serializar payload de pagamento: %w", err)
	}

	maxRetries := 3
	backoff := 100 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", targetURL+PaymentsPath, bytes.NewBuffer(payload))
		if err != nil {
			return fmt.Errorf("erro ao criar requisição: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			return fmt.Errorf("erro ao enviar pagamento para %s: %w", targetURL, err)
		}
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				logger.Warnf("Erro ao fechar o corpo da resposta: %v", cerr)
			}
		}()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			logger.Infof("Pagamento %s processado com sucesso por %s", request.CorrelationID, targetURL)
			return nil
		}

		if resp.StatusCode == 429 {
			retryAfter := resp.Header.Get("Retry-After")
			wait := backoff
			if retryAfter != "" {
				if secs, err := strconv.Atoi(retryAfter); err == nil {
					wait = time.Duration(secs) * 100 * time.Millisecond
				}
			}
			logger.Warnf("Recebido 429 do processador %s. Aguardando %v antes de tentar novamente...", targetURL, wait)
			time.Sleep(wait)
			backoff *= 2
			continue
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("processador de pagamento %s retornou status %d: %s", targetURL, resp.StatusCode, string(bodyBytes))
	}
	return fmt.Errorf("falha ao enviar pagamento para %s após %d tentativas", targetURL, maxRetries)
}

// GetHealthStatus returns the health status of processors
func (c *PaymentProcessorClient) GetHealthStatus(ctx context.Context) (*services.ProcessorHealthStatus, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return &services.ProcessorHealthStatus{
		Default: services.ProcessorHealth{
			IsHealthy:       !c.defaultProcessor.isFailing,
			MinResponseTime: c.defaultProcessor.minResponseTime,
			LastChecked:     c.defaultProcessor.lastCheck.Unix(),
		},
		Fallback: services.ProcessorHealth{
			IsHealthy:       !c.fallbackProcessor.isFailing,
			MinResponseTime: c.fallbackProcessor.minResponseTime,
			LastChecked:     c.fallbackProcessor.lastCheck.Unix(),
		},
	}, nil
}

// StartHealthChecks starts background health checking
func (c *PaymentProcessorClient) StartHealthChecks(ctx context.Context) {
	go c.runHealthCheck(ctx, true)
	go c.runHealthCheck(ctx, false)
}

// runHealthCheck runs health check for a specific processor
func (c *PaymentProcessorClient) runHealthCheck(ctx context.Context, isDefault bool) {
	ticker := time.NewTicker(HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			processor := &c.defaultProcessor
			processorName := "default"
			if !isDefault {
				processor = &c.fallbackProcessor
				processorName = "fallback"
			}

			health, err := c.getHealthCheck(ctx, processor.url)

			c.mutex.Lock()
			processor.lastCheck = time.Now()
			if err != nil {
				processor.isFailing = true
				logger.Errorf("Health check para %s falhou: %v", processorName, err)
			} else {
				wasFailingBefore := processor.isFailing
				processor.isFailing = health.Failing
				processor.minResponseTime = health.MinResponseTime

				if wasFailingBefore != health.Failing || health.Failing {
					logger.Infof("Health check para %s: Failing=%t, MinResponseTime=%d", processorName, health.Failing, health.MinResponseTime)
				}
			}
			c.mutex.Unlock()

		case <-ctx.Done():
			logger.Infof("Health check para %s parado", map[bool]string{true: "default", false: "fallback"}[isDefault])
			return
		}
	}
}

// getHealthCheck performs health check for a processor
func (c *PaymentProcessorClient) getHealthCheck(ctx context.Context, processorURL string) (*HealthCheckResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", processorURL+HealthCheckPath, nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição de health check: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao fazer requisição de health check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health check retornou status inesperado: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler resposta do health check: %w", err)
	}

	var healthCheck HealthCheckResponse
	if err := json.Unmarshal(body, &healthCheck); err != nil {
		return nil, fmt.Errorf("erro ao decodificar resposta do health check: %w", err)
	}

	return &healthCheck, nil
}
