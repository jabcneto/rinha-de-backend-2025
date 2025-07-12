package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

	// Adaptive processor selection
	adaptiveSelector *AdaptiveProcessorSelector
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
	HealthCheckInterval        = 2 * time.Second // Reduzido de 3s para 2s
	CircuitBreakerMaxFailures  = 3               // Reduzido de 5 para 3 para falhar mais rápido
	CircuitBreakerResetTimeout = 1 * time.Second // Reduzido de 2s para 1s para recuperar mais rápido
)

const (
	PaymentsPath = "/payments"
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
		MaxIdleConns:          200,              // Aumentado pool de conexões idle
		MaxIdleConnsPerHost:   100,              // Aumentado por host
		MaxConnsPerHost:       200,              // Aumentado máximo de conexões por host
		IdleConnTimeout:       30 * time.Second, // Aumentado timeout para conexões idle
		TLSHandshakeTimeout:   2 * time.Second,  // Aumentado para handshake TLS
		ExpectContinueTimeout: 1 * time.Second,  // Aumentado para expect continue
		DisableKeepAlives:     false,            // Manter conexões vivas
		DisableCompression:    true,             // Desabilitar compressão para performance
	}

	client := &http.Client{
		Timeout:   5 * time.Second, // Aumentado para 5 segundos para evitar timeouts
		Transport: transport,
	}

	paymentClient := &PaymentProcessorClient{
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

	// Initialize adaptive selector with reference to the client
	paymentClient.adaptiveSelector = NewAdaptiveProcessorSelector(paymentClient)

	return paymentClient
}

// ProcessPayment processes a payment through the appropriate processor
func (c *PaymentProcessorClient) ProcessPayment(ctx context.Context, payment *entities.Payment) (entities.ProcessorType, error) {
	// Create processor request
	request := &PaymentProcessorRequest{
		CorrelationID: payment.CorrelationID.String(),
		Amount:        payment.Amount,
		RequestedAt:   payment.RequestedAt,
	}

	// Use adaptive selection to choose the optimal processor
	optimalProcessor := c.adaptiveSelector.SelectOptimalProcessor(ctx)

	// Try the recommended processor first
	var primaryErr, secondaryErr error

	if optimalProcessor == entities.ProcessorTypeDefault {
		// Try default first (recommended)
		primaryErr = c.defaultProcessor.circuitBreaker.ExecuteWithAdaptiveIntegration(func() error {
			return c.processPaymentWithProcessor(ctx, request, c.defaultProcessor.url)
		})

		if primaryErr != nil {
			logger.Warnf("Default processor falhou, tentando fallback: %v", primaryErr)

			// Try fallback as secondary option
			secondaryErr = c.fallbackProcessor.circuitBreaker.ExecuteWithAdaptiveIntegration(func() error {
				return c.processPaymentWithProcessor(ctx, request, c.fallbackProcessor.url)
			})

			if secondaryErr != nil {
				// NOVA FUNCIONALIDADE: Fallback de emergência quando ambos circuit breakers estão fechados
				emergencyErr := c.tryEmergencyFallback(ctx, request, primaryErr, secondaryErr)
				if emergencyErr != nil {
					logger.Errorf("Todos os processadores falharam (incluindo emergência). Default: %v, Fallback: %v, Emergency: %v",
						primaryErr, secondaryErr, emergencyErr)
					return entities.ProcessorTypeDefault, primaryErr
				}

				logger.Infof("✅ Processamento de emergência bem-sucedido após falhas dos circuit breakers")
				return entities.ProcessorTypeDefault, nil // Sucesso na tentativa de emergência
			}

			return entities.ProcessorTypeFallback, nil
		}

		return entities.ProcessorTypeDefault, nil
	} else {
		// Try fallback first (recommended)
		primaryErr = c.fallbackProcessor.circuitBreaker.ExecuteWithAdaptiveIntegration(func() error {
			return c.processPaymentWithProcessor(ctx, request, c.fallbackProcessor.url)
		})

		if primaryErr != nil {
			logger.Warnf("Fallback processor falhou, tentando default: %v", primaryErr)

			// Try default as secondary option
			secondaryErr = c.defaultProcessor.circuitBreaker.ExecuteWithAdaptiveIntegration(func() error {
				return c.processPaymentWithProcessor(ctx, request, c.defaultProcessor.url)
			})

			if secondaryErr != nil {
				// NOVA FUNCIONALIDADE: Fallback de emergência quando ambos circuit breakers estão fechados
				emergencyErr := c.tryEmergencyFallback(ctx, request, primaryErr, secondaryErr)
				if emergencyErr != nil {
					logger.Errorf("Todos os processadores falharam (incluindo emergência). Fallback: %v, Default: %v, Emergency: %v",
						primaryErr, secondaryErr, emergencyErr)
					return entities.ProcessorTypeFallback, primaryErr
				}

				logger.Infof("✅ Processamento de emergência bem-sucedido após falhas dos circuit breakers")
				return entities.ProcessorTypeFallback, nil // Sucesso na tentativa de emergência
			}

			return entities.ProcessorTypeDefault, nil
		}

		return entities.ProcessorTypeFallback, nil
	}
}

// NOVA: processPaymentWithProcessor executa o processamento com um processador específico
func (c *PaymentProcessorClient) processPaymentWithProcessor(ctx context.Context, request *PaymentProcessorRequest, url string) error {
	startTime := time.Now()

	// Serialize request
	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("erro ao serializar request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", url+PaymentsPath, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("erro ao criar request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		logger.Warnf("Erro na requisição para %s: %v (tempo: %v)", url, err, time.Since(startTime))
		return err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			logger.Warnf("Erro ao fechar response body: %v", closeErr)
		}
	}()

	responseTime := time.Since(startTime)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
		logger.Warnf("Erro de status para %s: %v (tempo: %v)", url, err, responseTime)
		return err
	}

	logger.Debugf("Sucesso para %s (tempo: %v)", url, responseTime)
	return nil
}

// NOVA: StartIntegratedMonitoring inicia monitoramento integrado
func (c *PaymentProcessorClient) StartIntegratedMonitoring(ctx context.Context) {
	// Configurar integração dos circuit breakers com adaptive selector
	c.defaultProcessor.circuitBreaker.SetAdaptiveSelector(c.adaptiveSelector, entities.ProcessorTypeDefault)
	c.fallbackProcessor.circuitBreaker.SetAdaptiveSelector(c.adaptiveSelector, entities.ProcessorTypeFallback)

	// Iniciar monitoramento adaptativo
	c.adaptiveSelector.StartHealthMonitoring(ctx)

	// Iniciar health checks se método existir (será implementado nos métodos restantes)
	go c.periodicHealthCheck(ctx)

	logger.Info("🚀 Monitoramento integrado iniciado (Circuit Breaker + Adaptive Processor)")
}

// NOVA: periodicHealthCheck executa health checks periódicos
func (c *PaymentProcessorClient) periodicHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Parando health checks periódicos")
			return
		case <-ticker.C:
			c.performHealthChecks(ctx)
		}
	}
}

// NOVA: performHealthChecks executa verificações de saúde nos processadores
func (c *PaymentProcessorClient) performHealthChecks(ctx context.Context) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Health check para default processor
	go func() {
		start := time.Now()
		err := c.checkProcessorHealth(ctx, c.defaultProcessor.url)
		responseTime := time.Since(start)

		c.defaultProcessor.lastCheck = time.Now()
		c.defaultProcessor.isFailing = err != nil
		if err == nil {
			c.defaultProcessor.minResponseTime = responseTime.Milliseconds()
		}

		// Notificar adaptive selector
		c.adaptiveSelector.UpdateProcessorHealthDirect(entities.ProcessorTypeDefault, responseTime, err == nil)
	}()

	// Health check para fallback processor
	go func() {
		start := time.Now()
		err := c.checkProcessorHealth(ctx, c.fallbackProcessor.url)
		responseTime := time.Since(start)

		c.fallbackProcessor.lastCheck = time.Now()
		c.fallbackProcessor.isFailing = err != nil
		if err == nil {
			c.fallbackProcessor.minResponseTime = responseTime.Milliseconds()
		}

		// Notificar adaptive selector
		c.adaptiveSelector.UpdateProcessorHealthDirect(entities.ProcessorTypeFallback, responseTime, err == nil)
	}()
}

// NOVA: checkProcessorHealth verifica a saúde de um processador específico
func (c *PaymentProcessorClient) checkProcessorHealth(ctx context.Context, url string) error {
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(healthCtx, "GET", url+"/payments/service-health", nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			logger.Debugf("Erro ao fechar health check response: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}

	return nil
}

// NOVA: GetIntegratedStats retorna estatísticas completas da integração
func (c *PaymentProcessorClient) GetIntegratedStats() map[string]interface{} {
	stats := make(map[string]interface{})

	// Estatísticas do adaptive processor
	stats["adaptive_processor"] = c.adaptiveSelector.GetAllStats()

	// Estatísticas dos circuit breakers
	stats["circuit_breakers"] = map[string]interface{}{
		"default":  c.defaultProcessor.circuitBreaker.GetStats(),
		"fallback": c.fallbackProcessor.circuitBreaker.GetStats(),
	}

	// Recomendações integradas
	defaultRecommendations := c.defaultProcessor.circuitBreaker.GetRecommendations()
	fallbackRecommendations := c.fallbackProcessor.circuitBreaker.GetRecommendations()

	stats["recommendations"] = map[string]interface{}{
		"default":  defaultRecommendations,
		"fallback": fallbackRecommendations,
	}

	// Análise de switching
	shouldSwitch, newProcessor, reason := c.adaptiveSelector.ShouldSwitchProcessor(entities.ProcessorTypeDefault)
	stats["switching_analysis"] = map[string]interface{}{
		"should_switch_from_default": shouldSwitch,
		"recommended_processor":      string(newProcessor),
		"reason":                     reason,
	}

	return stats
}

// NOVA: GetHealthInsights retorna insights detalhados sobre a saúde
func (c *PaymentProcessorClient) GetHealthInsights() map[string]interface{} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	insights := make(map[string]interface{})

	// Status atual dos processadores
	insights["processor_status"] = map[string]interface{}{
		"default": map[string]interface{}{
			"is_failing":        c.defaultProcessor.isFailing,
			"min_response_time": c.defaultProcessor.minResponseTime,
			"last_check":        c.defaultProcessor.lastCheck.Format(time.RFC3339),
			"circuit_state":     c.defaultProcessor.circuitBreaker.GetState(),
			"is_healthy":        c.defaultProcessor.circuitBreaker.IsHealthy(),
		},
		"fallback": map[string]interface{}{
			"is_failing":        c.fallbackProcessor.isFailing,
			"min_response_time": c.fallbackProcessor.minResponseTime,
			"last_check":        c.fallbackProcessor.lastCheck.Format(time.RFC3339),
			"circuit_state":     c.fallbackProcessor.circuitBreaker.GetState(),
			"is_healthy":        c.fallbackProcessor.circuitBreaker.IsHealthy(),
		},
	}

	// Recomendação atual
	optimalProcessor := c.adaptiveSelector.SelectOptimalProcessor(context.Background())
	insights["current_recommendation"] = map[string]interface{}{
		"optimal_processor": string(optimalProcessor),
		"timestamp":         time.Now().Format(time.RFC3339),
	}

	// Métricas detalhadas do adaptive processor
	insights["adaptive_metrics"] = map[string]interface{}{
		"default":  c.adaptiveSelector.GetProcessorStats(entities.ProcessorTypeDefault),
		"fallback": c.adaptiveSelector.GetProcessorStats(entities.ProcessorTypeFallback),
	}

	return insights
}

// Implementar métodos restantes da interface services.PaymentProcessorService

// ProcessWithDefault processes payment with default processor
func (c *PaymentProcessorClient) ProcessWithDefault(ctx context.Context, payment *entities.Payment) error {
	request := &PaymentProcessorRequest{
		CorrelationID: payment.CorrelationID.String(),
		Amount:        payment.Amount,
		RequestedAt:   payment.RequestedAt,
	}

	return c.defaultProcessor.circuitBreaker.ExecuteWithAdaptiveIntegration(func() error {
		return c.processPaymentWithProcessor(ctx, request, c.defaultProcessor.url)
	})
}

// ProcessWithFallback processes payment with fallback processor
func (c *PaymentProcessorClient) ProcessWithFallback(ctx context.Context, payment *entities.Payment) error {
	request := &PaymentProcessorRequest{
		CorrelationID: payment.CorrelationID.String(),
		Amount:        payment.Amount,
		RequestedAt:   payment.RequestedAt,
	}

	return c.fallbackProcessor.circuitBreaker.ExecuteWithAdaptiveIntegration(func() error {
		return c.processPaymentWithProcessor(ctx, request, c.fallbackProcessor.url)
	})
}

// StartHealthChecks inicia os health checks periódicos
func (c *PaymentProcessorClient) StartHealthChecks(ctx context.Context) {
	// Iniciar monitoramento integrado que já inclui health checks
	c.StartIntegratedMonitoring(ctx)
}

// GetHealthStatus retorna o status de saúde dos processadores
func (c *PaymentProcessorClient) GetHealthStatus(_ context.Context) (*services.ProcessorHealthStatus, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return &services.ProcessorHealthStatus{
		Default: services.ProcessorHealth{
			IsHealthy:       !c.defaultProcessor.isFailing,
			MinResponseTime: c.defaultProcessor.minResponseTime,
		},
		Fallback: services.ProcessorHealth{
			IsHealthy:       !c.fallbackProcessor.isFailing,
			MinResponseTime: c.fallbackProcessor.minResponseTime,
		},
	}, nil
}

// NOVA: isCircuitBreakerError verifica se o erro é de circuit breaker fechado ou erro HTTP
func (c *PaymentProcessorClient) isCircuitBreakerError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Detectar erros de circuit breaker fechado
	if contains(errStr, "circuit breaker") && contains(errStr, "OPEN") {
		return true
	}

	// Detectar erros HTTP (status 4xx, 5xx) que também devem ativar modo emergência
	if contains(errStr, "status 4") || contains(errStr, "status 5") {
		return true
	}

	// Detectar erros de timeout/conexão que também devem ativar modo emergência
	if contains(errStr, "timeout") || contains(errStr, "connection") || contains(errStr, "no such host") {
		return true
	}

	return false
}

// NOVA: tryEmergencyFallback tenta processar diretamente quando ambos circuit breakers estão fechados
func (c *PaymentProcessorClient) tryEmergencyFallback(ctx context.Context, request *PaymentProcessorRequest, primaryErr, secondaryErr error) error {
	logger.Warnf("🚨 MODO EMERGÊNCIA: Ambos processadores falharam, tentando acesso direto")

	// Verificar se devemos ativar modo emergência
	primaryShouldEmergency := c.isCircuitBreakerError(primaryErr) || c.isHTTPError(primaryErr)
	secondaryShouldEmergency := c.isCircuitBreakerError(secondaryErr) || c.isHTTPError(secondaryErr)

	// Ativar modo emergência se pelo menos um dos erros justifica
	if !primaryShouldEmergency && !secondaryShouldEmergency {
		logger.Debugf("Erros não justificam modo emergência. Primary: %v, Secondary: %v", primaryErr, secondaryErr)
		return fmt.Errorf("falhas não relacionadas a circuit breaker ou problemas de rede")
	}

	logger.Infof("🔥 Ativando processamento de emergência - bypassing circuit breakers")

	// Criar contexto com timeout mais longo para emergência (aumentado de 3s para 8s)
	emergencyCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	// Tentar processadores múltiplas vezes com backoff
	var emergencyErrors []error

	// 1. Tentar o processador que teve melhor performance recente
	bestProcessor := c.selectBestProcessorForEmergency()

	// Implementar retry com backoff no modo emergência
	maxRetries := 3
	baseDelay := 100 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		logger.Infof("🔧 Tentativa de emergência #%d", attempt)

		if bestProcessor == entities.ProcessorTypeDefault {
			// Tentar default primeiro
			logger.Infof("⚡ Emergência: tentando DEFAULT (tentativa %d/%d)", attempt, maxRetries)
			err := c.processPaymentDirectlyWithRetry(emergencyCtx, request, c.defaultProcessor.url, "default", attempt)
			if err == nil {
				c.notifyEmergencySuccess(entities.ProcessorTypeDefault)
				logger.Infof("✅ SUCESSO emergência com DEFAULT na tentativa %d", attempt)
				return nil
			}
			emergencyErrors = append(emergencyErrors, fmt.Errorf("default emergency attempt %d: %w", attempt, err))

			// Se default falhou, tentar fallback
			logger.Infof("⚡ Emergência: tentando FALLBACK (tentativa %d/%d)", attempt, maxRetries)
			err = c.processPaymentDirectlyWithRetry(emergencyCtx, request, c.fallbackProcessor.url, "fallback", attempt)
			if err == nil {
				c.notifyEmergencySuccess(entities.ProcessorTypeFallback)
				logger.Infof("✅ SUCESSO emergência com FALLBACK na tentativa %d", attempt)
				return nil
			}
			emergencyErrors = append(emergencyErrors, fmt.Errorf("fallback emergency attempt %d: %w", attempt, err))

		} else {
			// Tentar fallback primeiro
			logger.Infof("⚡ Emergência: tentando FALLBACK (tentativa %d/%d)", attempt, maxRetries)
			err := c.processPaymentDirectlyWithRetry(emergencyCtx, request, c.fallbackProcessor.url, "fallback", attempt)
			if err == nil {
				c.notifyEmergencySuccess(entities.ProcessorTypeFallback)
				logger.Infof("✅ SUCESSO emergência com FALLBACK na tentativa %d", attempt)
				return nil
			}
			emergencyErrors = append(emergencyErrors, fmt.Errorf("fallback emergency attempt %d: %w", attempt, err))

			// Se fallback falhou, tentar default
			logger.Infof("⚡ Emergência: tentando DEFAULT (tentativa %d/%d)", attempt, maxRetries)
			err = c.processPaymentDirectlyWithRetry(emergencyCtx, request, c.defaultProcessor.url, "default", attempt)
			if err == nil {
				c.notifyEmergencySuccess(entities.ProcessorTypeDefault)
				logger.Infof("✅ SUCESSO emergência com DEFAULT na tentativa %d", attempt)
				return nil
			}
			emergencyErrors = append(emergencyErrors, fmt.Errorf("default emergency attempt %d: %w", attempt, err))
		}

		// Backoff exponencial entre tentativas (se não for a última tentativa)
		if attempt < maxRetries {
			delay := time.Duration(attempt) * baseDelay
			logger.Infof("⏳ Aguardando %v antes da próxima tentativa de emergência", delay)
			time.Sleep(delay)
		}
	}

	// Se chegou aqui, todas as tentativas de emergência falharam
	logger.Errorf("❌ Todas as tentativas de emergência falharam após %d tentativas: %v", maxRetries, emergencyErrors)
	return fmt.Errorf("emergency fallback failed after %d attempts - all processors unreachable: %v", maxRetries, emergencyErrors)
}

// NOVA: processPaymentDirectly processa pagamento diretamente sem circuit breaker
func (c *PaymentProcessorClient) processPaymentDirectly(ctx context.Context, request *PaymentProcessorRequest, url, processorName string) error {
	startTime := time.Now()

	logger.Infof("⚡ Processamento direto (emergência) para %s: %s", processorName, url)

	// Serialize request
	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("erro ao serializar request de emergência: %w", err)
	}

	// Create HTTP request com timeout mais agressivo
	req, err := http.NewRequestWithContext(ctx, "POST", url+PaymentsPath, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("erro ao criar request de emergência: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emergency-Request", "true") // Header para identificar requisições de emergência

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		logger.Warnf("❌ Erro na requisição de emergência para %s: %v (tempo: %v)", url, err, time.Since(startTime))
		return err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			logger.Warnf("Erro ao fechar response body de emergência: %v", closeErr)
		}
	}()

	responseTime := time.Since(startTime)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
		logger.Warnf("❌ Erro de status na emergência para %s: %v (tempo: %v)", url, err, responseTime)
		return err
	}

	logger.Infof("✅ SUCESSO na emergência para %s (tempo: %v)", processorName, responseTime)
	return nil
}

// NOVA: processPaymentDirectlyWithRetry processa pagamento diretamente com configurações de retry
func (c *PaymentProcessorClient) processPaymentDirectlyWithRetry(ctx context.Context, request *PaymentProcessorRequest, url, processorName string, attempt int) error {
	startTime := time.Now()

	logger.Infof("⚡ Processamento direto (emergência #%d) para %s: %s", attempt, processorName, url)

	// Serialize request
	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("erro ao serializar request de emergência: %w", err)
	}

	// Timeout progressivo: primeiro tentativa mais agressiva, depois mais tolerante
	var requestTimeout time.Duration
	switch attempt {
	case 1:
		requestTimeout = 2 * time.Second // Primeira tentativa: agressiva
	case 2:
		requestTimeout = 4 * time.Second // Segunda tentativa: moderada
	default:
		requestTimeout = 6 * time.Second // Terceira tentativa: conservadora
	}

	// Create HTTP request com timeout progressivo
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", url+PaymentsPath, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("erro ao criar request de emergência: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emergency-Request", "true")
	req.Header.Set("X-Emergency-Attempt", fmt.Sprintf("%d", attempt)) // Header para debug

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		logger.Warnf("❌ Erro na requisição de emergência #%d para %s: %v (tempo: %v, timeout: %v)",
			attempt, url, err, time.Since(startTime), requestTimeout)
		return err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			logger.Warnf("Erro ao fechar response body de emergência: %v", closeErr)
		}
	}()

	responseTime := time.Since(startTime)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
		logger.Warnf("❌ Erro de status na emergência #%d para %s: %v (tempo: %v)",
			attempt, url, err, responseTime)
		return err
	}

	logger.Infof("✅ SUCESSO na emergência #%d para %s (tempo: %v)", attempt, processorName, responseTime)
	return nil
}

// NOVA: isHTTPError verifica se é um erro HTTP que justifica modo emergência
func (c *PaymentProcessorClient) isHTTPError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Detectar erros HTTP de servidor (5xx) ou cliente críticos (4xx)
	if contains(errStr, "status 5") {
		return true // Status 5xx sempre ativa emergência
	}

	// Alguns status 4xx críticos que também justificam emergência
	if contains(errStr, "status 408") || // Request Timeout
		contains(errStr, "status 429") || // Too Many Requests
		contains(errStr, "status 503") || // Service Unavailable
		contains(errStr, "status 502") || // Bad Gateway
		contains(errStr, "status 504") { // Gateway Timeout
		return true
	}

	return false
}

// NOVA: selectBestProcessorForEmergency seleciona o melhor processador para emergência
func (c *PaymentProcessorClient) selectBestProcessorForEmergency() entities.ProcessorType {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// Usar informações do adaptive selector para determinar qual processador usar
	defaultStats := c.adaptiveSelector.GetProcessorStats(entities.ProcessorTypeDefault)
	fallbackStats := c.adaptiveSelector.GetProcessorStats(entities.ProcessorTypeFallback)

	// Verificar qual teve melhor performance recente
	if defaultResponseTime, ok := defaultStats["response_time_ms"].(int64); ok {
		if fallbackResponseTime, ok := fallbackStats["response_time_ms"].(int64); ok {
			// Se default teve response time melhor (menor), preferir default
			if defaultResponseTime > 0 && (fallbackResponseTime == 0 || defaultResponseTime < fallbackResponseTime) {
				logger.Debugf("Selecionando DEFAULT para emergência (response time: %dms vs %dms)",
					defaultResponseTime, fallbackResponseTime)
				return entities.ProcessorTypeDefault
			}
		}
	}

	// Por padrão, usar fallback em emergências (mais conservador)
	logger.Debugf("Selecionando FALLBACK para emergência (padrão conservador)")
	return entities.ProcessorTypeFallback
}

// NOVA: notifyEmergencySuccess notifica sobre sucesso em modo emergência
func (c *PaymentProcessorClient) notifyEmergencySuccess(processorType entities.ProcessorType) {
	logger.Infof("🎉 Sucesso no modo emergência com processador %s", processorType)

	// Notificar o adaptive selector sobre o sucesso de emergência
	c.adaptiveSelector.UpdateProcessorHealthDirect(processorType, 100*time.Millisecond, true)

	// Consideração: talvez seja hora de reabrir os circuit breakers mais agressivamente
	c.considerCircuitBreakerReset(processorType)
}

// NOVA: considerCircuitBreakerReset considera resetar circuit breaker após sucesso de emergência
func (c *PaymentProcessorClient) considerCircuitBreakerReset(processorType entities.ProcessorType) {
	var cb *CircuitBreaker

	switch processorType {
	case entities.ProcessorTypeDefault:
		cb = c.defaultProcessor.circuitBreaker
	case entities.ProcessorTypeFallback:
		cb = c.fallbackProcessor.circuitBreaker
	default:
		return
	}

	// Se o circuit breaker está fechado mas acabamos de ter sucesso direto,
	// reduzir o timeout para próxima tentativa
	if cb.GetState() == CircuitOpen {
		logger.Infof("💡 Considerando reset acelerado do circuit breaker %s após sucesso de emergência", processorType)
		// O circuit breaker pode ser reaberto mais cedo na próxima tentativa
	}
}

// NOVA: contains verifica se uma string contém uma substring (helper function)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsSubstring(s, substr))))
}

// NOVA: containsSubstring helper para verificar substring
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
