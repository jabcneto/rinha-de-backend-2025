package external

import (
	"fmt"
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
	name            string
	state           string
	failureCount    int
	lastFailureTime time.Time
	resetTimeout    time.Duration
	maxFailures     int
	mutex           sync.Mutex
}

// NewCircuitBreaker creates a new CircuitBreaker
func NewCircuitBreaker(name string, maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:         name,
		state:        CircuitClosed,
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
	}
}

// Execute executes the given function with circuit breaker protection
func (cb *CircuitBreaker) Execute(f func() error) error {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	switch cb.state {
	case CircuitOpen:
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			// Time to try again, move to Half-Open
			cb.state = CircuitHalfOpen
			cb.failureCount = 0
			logger.Infof("Circuit Breaker '%s' mudou para HALF-OPEN", cb.name)
		} else {
			return fmt.Errorf("circuit breaker '%s' está OPEN. Não executando a função", cb.name)
		}
	case CircuitHalfOpen:
		// Allow one request to go through
		// If it fails, go back to Open
		// If it succeeds, go back to Closed
	}

	err := f()
	if err != nil {
		cb.failureCount++
		cb.lastFailureTime = time.Now()
		logger.Infof("Circuit Breaker '%s' falhou. Contagem de falhas: %d", cb.name, cb.failureCount)

		if cb.state == CircuitHalfOpen || cb.failureCount >= cb.maxFailures {
			cb.state = CircuitOpen
			logger.Infof("Circuit Breaker '%s' mudou para OPEN", cb.name)
		}
		return err
	}

	// Success
	cb.failureCount = 0
	if cb.state == CircuitHalfOpen {
		cb.state = CircuitClosed
		logger.Infof("Circuit Breaker '%s' mudou para CLOSED", cb.name)
	}

	return nil
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() string {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	return cb.state
}

// GetFailureCount returns the current failure count
func (cb *CircuitBreaker) GetFailureCount() int {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	return cb.failureCount
}
