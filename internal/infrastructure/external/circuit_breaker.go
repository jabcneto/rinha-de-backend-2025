package external

import (
	"fmt"
	"rinha-backend-clean/internal/domain/entities"
	"rinha-backend-clean/internal/infrastructure/logger"
	"sync"
	"time"
)

// Circuit Breaker States
const (
	CircuitClosed   = "closed"
	CircuitOpen     = "open"
	CircuitHalfOpen = "half-open"
)

// CircuitBreaker represents a circuit breaker for resilience
type CircuitBreaker struct {
	name               string
	state              string
	failureCount       int
	successCount       int
	lastFailureTime    time.Time
	lastSuccessTime    time.Time
	resetTimeout       time.Duration
	maxFailures        int
	failureWindow      time.Duration
	halfOpenMaxRetries int
	mutex              sync.Mutex

	// Failure tracking with time windows
	failures []time.Time

	// NOVA: Integração com Adaptive Processor
	adaptiveSelector *AdaptiveProcessorSelector
	processorType    entities.ProcessorType
	baseResetTimeout time.Duration // Timeout original para reset
}

// NewCircuitBreaker creates a new CircuitBreaker
func NewCircuitBreaker(name string, maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:               name,
		state:              CircuitClosed,
		maxFailures:        maxFailures,
		resetTimeout:       resetTimeout,
		baseResetTimeout:   resetTimeout,
		failureWindow:      30 * time.Second,
		halfOpenMaxRetries: 3, // Permitir algumas tentativas em half-open
		failures:           make([]time.Time, 0),
	}
}

// Execute executes the function with circuit breaker protection
func (cb *CircuitBreaker) Execute(f func() error) error {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	// Clean up old failures outside the failure window
	cb.cleanupOldFailures()

	switch cb.state {
	case CircuitClosed:
		// Normal operation
	case CircuitOpen:
		timeSinceFailure := time.Since(cb.lastFailureTime)

		// Implementar backoff exponencial baseado no número de falhas consecutivas
		dynamicTimeout := cb.calculateDynamicTimeout()

		if timeSinceFailure < dynamicTimeout {
			// Circuit ainda está aberto - implementar tentativas progressivas menos agressivas
			if cb.shouldAllowProgressiveAttempt() {
				logger.Debugf("Circuit breaker '%s' está OPEN, mas permitindo tentativa progressiva em %v",
					cb.name, dynamicTimeout-timeSinceFailure)
			} else {
				return fmt.Errorf("circuit breaker '%s' está OPEN. Tentando novamente em %v",
					cb.name, dynamicTimeout-timeSinceFailure)
			}
		} else {
			// Time to try half-open
			cb.state = CircuitHalfOpen
			cb.successCount = 0
			logger.Infof("Circuit breaker '%s' mudando para HALF-OPEN após %v", cb.name, timeSinceFailure)
		}
	case CircuitHalfOpen:
		// In half-open state, allow limited requests
	}

	err := f()
	if err != nil {
		cb.recordFailure()

		if cb.state == CircuitHalfOpen {
			// In half-open, any failure goes back to open with exponential backoff
			cb.state = CircuitOpen
			cb.lastFailureTime = time.Now()
			logger.Warnf("Circuit Breaker '%s' voltou para OPEN após falha em HALF-OPEN", cb.name)
		} else if len(cb.failures) >= cb.maxFailures {
			// In closed state, check if we exceeded failure threshold
			cb.state = CircuitOpen
			cb.lastFailureTime = time.Now()
			logger.Warnf("Circuit Breaker '%s' mudou para OPEN após %d falhas em %v",
				cb.name, len(cb.failures), cb.failureWindow)
		}
		return err
	}

	// Success
	cb.recordSuccess()

	if cb.state == CircuitHalfOpen {
		// Require fewer successes to close completely
		if cb.successCount >= 2 {
			cb.state = CircuitClosed
			cb.failures = make([]time.Time, 0) // Clear failure history
			logger.Infof("Circuit Breaker '%s' mudou para CLOSED após %d sucessos", cb.name, cb.successCount)
		}
	} else if cb.state == CircuitClosed {
		// In closed state, success helps reduce failure impact
		if len(cb.failures) > 0 && time.Since(cb.lastSuccessTime) < 3*time.Second {
			// Remove oldest failure for recent success
			if len(cb.failures) > 0 {
				cb.failures = cb.failures[1:]
			}
		}
	}

	return nil
}

// shouldAllowProgressiveAttempt determines if a progressive attempt should be allowed
func (cb *CircuitBreaker) shouldAllowProgressiveAttempt() bool {
	// OTIMIZAÇÃO: Aumentar chance de tentativas progressivas de 10% para 30%
	return time.Now().Nanosecond()%10 < 3 // 3 em 10 = 30%
}

// recordFailure records a failure with timestamp
func (cb *CircuitBreaker) recordFailure() {
	now := time.Now()
	cb.failures = append(cb.failures, now)
	cb.lastFailureTime = now
	cb.successCount = 0 // Reset success count on failure
}

// recordSuccess records a success with timestamp
func (cb *CircuitBreaker) recordSuccess() {
	cb.lastSuccessTime = time.Now()
	cb.successCount++
}

// cleanupOldFailures removes failures outside the time window
func (cb *CircuitBreaker) cleanupOldFailures() {
	now := time.Now()
	cutoff := now.Add(-cb.failureWindow)

	// Remove failures older than the window
	validFailures := make([]time.Time, 0)
	for _, failure := range cb.failures {
		if failure.After(cutoff) {
			validFailures = append(validFailures, failure)
		}
	}
	cb.failures = validFailures
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() string {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	return cb.state
}

// GetFailureCount returns the current failure count in the window
func (cb *CircuitBreaker) GetFailureCount() int {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	cb.cleanupOldFailures()
	return len(cb.failures)
}

// GetStats returns detailed statistics about the circuit breaker
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	cb.cleanupOldFailures()

	stats := map[string]interface{}{
		"name":           cb.name,
		"state":          cb.state,
		"failure_count":  len(cb.failures),
		"success_count":  cb.successCount,
		"max_failures":   cb.maxFailures,
		"reset_timeout":  cb.resetTimeout.String(),
		"failure_window": cb.failureWindow.String(),
	}

	if !cb.lastFailureTime.IsZero() {
		stats["last_failure"] = cb.lastFailureTime.Format(time.RFC3339)
		stats["time_since_failure"] = time.Since(cb.lastFailureTime).String()
	}

	if !cb.lastSuccessTime.IsZero() {
		stats["last_success"] = cb.lastSuccessTime.Format(time.RFC3339)
		stats["time_since_success"] = time.Since(cb.lastSuccessTime).String()
	}

	// NOVA: Adicionar informações do adaptive processor se disponível
	if cb.adaptiveSelector != nil && cb.processorType != "" {
		adaptiveStats := cb.adaptiveSelector.GetProcessorStats(cb.processorType)
		stats["adaptive_metrics"] = adaptiveStats
	}

	return stats
}

// SetAdaptiveSelector integrates the circuit breaker with adaptive processor
func (cb *CircuitBreaker) SetAdaptiveSelector(selector *AdaptiveProcessorSelector, processorType entities.ProcessorType) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.adaptiveSelector = selector
	cb.processorType = processorType
	cb.baseResetTimeout = cb.resetTimeout
}

// ExecuteWithAdaptiveIntegration executa com integração ao adaptive processor
func (cb *CircuitBreaker) ExecuteWithAdaptiveIntegration(f func() error) error {
	startTime := time.Now()

	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	// Clean old failures outside the window
	cb.cleanupOldFailures()

	// NOVA: Consultar adaptive processor para timeouts dinâmicos
	if cb.adaptiveSelector != nil {
		cb.adjustTimeoutsBasedOnAdaptiveMetrics()
	}

	switch cb.state {
	case CircuitOpen:
		timeSinceFailure := time.Since(cb.lastFailureTime)

		if timeSinceFailure > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			cb.successCount = 0
			logger.Infof("Circuit Breaker '%s' mudou para HALF-OPEN após %v", cb.name, timeSinceFailure)
		} else if timeSinceFailure > cb.resetTimeout/4 {
			if cb.shouldAllowProgressiveAttempt() {
				logger.Warnf("Circuit Breaker '%s' permitindo tentativa progressiva após %v", cb.name, timeSinceFailure)
			} else {
				return fmt.Errorf("circuit breaker '%s' está OPEN. Tentando novamente em %v",
					cb.name, cb.resetTimeout-timeSinceFailure)
			}
		} else {
			return fmt.Errorf("circuit breaker '%s' está OPEN. Tentando novamente em %v",
				cb.name, cb.resetTimeout-timeSinceFailure)
		}
	case CircuitHalfOpen:
		// Allow limited requests in half-open state
	}

	err := f()
	responseTime := time.Since(startTime)

	// NOVA: Notificar o adaptive processor sobre o resultado
	if cb.adaptiveSelector != nil {
		cb.notifyAdaptiveProcessor(err, responseTime)
	}

	if err != nil {
		cb.recordFailure()

		if cb.state == CircuitHalfOpen {
			cb.state = CircuitOpen
			cb.lastFailureTime = time.Now()
			if cb.resetTimeout > 200*time.Millisecond {
				cb.resetTimeout = cb.resetTimeout * 2 / 3
			}
			logger.Infof("Circuit Breaker '%s' voltou para OPEN após falha em HALF-OPEN (timeout reduzido para %v)",
				cb.name, cb.resetTimeout)
		} else if len(cb.failures) >= cb.maxFailures {
			cb.state = CircuitOpen
			cb.lastFailureTime = time.Now()
			logger.Infof("Circuit Breaker '%s' mudou para OPEN após %d falhas em %v",
				cb.name, len(cb.failures), cb.failureWindow)
		}
		return err
	}

	// Success
	cb.recordSuccess()

	if cb.state == CircuitHalfOpen {
		if cb.successCount >= 1 {
			cb.state = CircuitClosed
			cb.failures = make([]time.Time, 0)
			cb.resetTimeout = cb.baseResetTimeout
			logger.Infof("Circuit Breaker '%s' mudou para CLOSED após %d sucesso (timeout restaurado)",
				cb.name, cb.successCount)
		}
	} else if cb.state == CircuitClosed {
		if len(cb.failures) > 0 && time.Since(cb.lastSuccessTime) < 2*time.Second {
			if len(cb.failures) > 0 {
				cb.failures = cb.failures[1:]
				if time.Since(cb.lastSuccessTime) < 500*time.Millisecond && len(cb.failures) > 0 {
					cb.failures = cb.failures[1:]
				}
			}
		}
	}

	return nil
}

// adjustTimeoutsBasedOnAdaptiveMetrics ajusta timeouts baseado nas métricas adaptativas
func (cb *CircuitBreaker) adjustTimeoutsBasedOnAdaptiveMetrics() {
	if cb.adaptiveSelector == nil {
		return
	}

	// Obter recomendações do adaptive processor
	recommendations := cb.adaptiveSelector.GetRecommendationsForProcessor(cb.processorType)

	if timeoutMs, ok := recommendations["suggested_timeout_ms"]; ok {
		if timeout, ok := timeoutMs.(int); ok {
			suggestedTimeout := time.Duration(timeout) * time.Millisecond

			// Ajustar resetTimeout baseado na recomendação
			if suggestedTimeout < cb.baseResetTimeout {
				cb.resetTimeout = suggestedTimeout
			} else {
				cb.resetTimeout = cb.baseResetTimeout
			}
		}
	}

	if maxFailures, ok := recommendations["suggested_max_failures"]; ok {
		if failures, ok := maxFailures.(int); ok {
			cb.maxFailures = failures
		}
	}
}

// notifyAdaptiveProcessor notifica o adaptive processor sobre o resultado
func (cb *CircuitBreaker) notifyAdaptiveProcessor(err error, responseTime time.Duration) {
	if cb.adaptiveSelector == nil {
		return
	}

	cb.adaptiveSelector.mutex.Lock()
	defer cb.adaptiveSelector.mutex.Unlock()

	score, exists := cb.adaptiveSelector.processors[cb.processorType]
	if !exists {
		return
	}

	// Atualizar métricas baseado no resultado
	score.ResponseTime = responseTime.Milliseconds()
	score.LastUpdated = time.Now()

	if err != nil {
		score.ConsecutiveFails++
		score.IsFailing = true
		score.IsHealthy = false

		// Penalizar weight por falha
		score.Weight *= 0.9
		if score.Weight < 0.1 {
			score.Weight = 0.1
		}

		logger.Debugf("Circuit Breaker notificou falha para %s (fails consecutivos: %d, weight: %.3f)",
			cb.processorType, score.ConsecutiveFails, score.Weight)
	} else {
		// Sucesso - resetar falhas consecutivas e melhorar weight
		score.ConsecutiveFails = 0
		score.IsFailing = false
		score.IsHealthy = true

		// Melhorar weight por sucesso
		score.Weight *= 1.05
		if score.Weight > 1.0 {
			score.Weight = 1.0
		}

		logger.Debugf("Circuit Breaker notificou sucesso para %s (response: %dms, weight: %.3f)",
			cb.processorType, responseTime.Milliseconds(), score.Weight)
	}

	// Recalcular health score
	cb.adaptiveSelector.recalculateHealthScore(score)
}

// IsHealthy retorna se o circuit breaker está em estado saudável
func (cb *CircuitBreaker) IsHealthy() bool {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	return cb.state == CircuitClosed
}

// GetRecommendations retorna recomendações baseadas no estado atual
func (cb *CircuitBreaker) GetRecommendations() map[string]interface{} {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	recommendations := make(map[string]interface{})

	switch cb.state {
	case CircuitOpen:
		recommendations["action"] = "wait_or_switch"
		recommendations["message"] = "Circuit breaker aberto - considere usar processador alternativo"
		recommendations["wait_time"] = cb.resetTimeout.String()
	case CircuitHalfOpen:
		recommendations["action"] = "monitor_closely"
		recommendations["message"] = "Circuit breaker em teste - monitorar próximas requisições"
	case CircuitClosed:
		if len(cb.failures) > cb.maxFailures/2 {
			recommendations["action"] = "caution"
			recommendations["message"] = "Muitas falhas recentes - possível degradação"
		} else {
			recommendations["action"] = "normal"
			recommendations["message"] = "Circuit breaker operando normalmente"
		}
	}

	recommendations["failure_rate"] = float64(len(cb.failures)) / float64(cb.maxFailures)

	return recommendations
}

// calculateDynamicTimeout calcula um timeout dinâmico baseado nas falhas consecutivas
func (cb *CircuitBreaker) calculateDynamicTimeout() time.Duration {
	// Backoff exponencial: 100ms, 200ms, 400ms, 800ms, 1600ms, ...
	baseDelay := 100 * time.Millisecond
	maxDelay := 5 * time.Second

	// Delay atual baseado no número de falhas consecutivas
	delay := time.Duration(cb.failureCount) * baseDelay

	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}
