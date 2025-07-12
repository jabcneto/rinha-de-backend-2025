package queue

import (
	"context"
	"rinha-backend-clean/internal/domain/entities"
	"rinha-backend-clean/internal/domain/services"
	"rinha-backend-clean/internal/infrastructure/logger"
	"sync"
	"time"
)

// RetryQueueImpl implements the RetryQueueService interface
type RetryQueueImpl struct {
	defaultRetryQueue   chan *entities.Payment
	fallbackRetryQueue  chan *entities.Payment
	permanentRetryQueue chan *entities.Payment
	workerCount         int
	workers             []chan struct{}
	wg                  sync.WaitGroup
	ctx                 context.Context
	cancel              context.CancelFunc
	mutex               sync.RWMutex

	// Retry configuration
	maxDefaultRetries       int
	maxFallbackRetries      int
	maxPermanentRetries     int
	tickerInterval          time.Duration
	permanentTickerInterval time.Duration
}

const (
	DefaultMaxRetries      = 2
	FallbackMaxRetries     = 2
	RetryTickerInterval    = 2 * time.Second
	PermanentMaxRetries    = 8
	PermanentRetryInterval = 10 * time.Second
)

// NewRetryQueue creates a new retry queue service
func NewRetryQueue(bufferSize, workerCount int) services.RetryQueueService {
	ctx, cancel := context.WithCancel(context.Background())

	return &RetryQueueImpl{
		defaultRetryQueue:       make(chan *entities.Payment, bufferSize),
		fallbackRetryQueue:      make(chan *entities.Payment, bufferSize),
		permanentRetryQueue:     make(chan *entities.Payment, bufferSize), // Inicializa a nova fila
		workerCount:             workerCount,
		workers:                 make([]chan struct{}, workerCount*3), // 3x workers para todas as filas
		ctx:                     ctx,
		cancel:                  cancel,
		maxDefaultRetries:       DefaultMaxRetries,
		maxFallbackRetries:      FallbackMaxRetries,
		maxPermanentRetries:     PermanentMaxRetries,
		tickerInterval:          RetryTickerInterval,
		permanentTickerInterval: PermanentRetryInterval,
	}
}

// EnqueueForDefaultRetry enqueues a payment for retry with default processor
func (rq *RetryQueueImpl) EnqueueForDefaultRetry(ctx context.Context, payment *entities.Payment) error {
	select {
	case rq.defaultRetryQueue <- payment:
		logger.Infof("Pagamento %s enfileirado para retry no processador default (tentativa %d)",
			payment.CorrelationID, payment.RetryCount)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		logger.Warnf("AVISO: Fila de retry default cheia! Pagamento %s descartado", payment.CorrelationID)
		return nil
	}
}

// EnqueueForFallbackRetry enqueues a payment for retry with fallback processor
func (rq *RetryQueueImpl) EnqueueForFallbackRetry(ctx context.Context, payment *entities.Payment) error {
	select {
	case rq.fallbackRetryQueue <- payment:
		logger.Infof("Pagamento %s enfileirado para retry no processador fallback (tentativa %d)",
			payment.CorrelationID, payment.RetryCount)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		logger.Warnf("AVISO: Fila de retry fallback cheia! Pagamento %s descartado", payment.CorrelationID)
		return nil
	}
}

// EnqueueForPermanentRetry enqueues a payment for permanent retry
func (rq *RetryQueueImpl) EnqueueForPermanentRetry(ctx context.Context, payment *entities.Payment) error {
	select {
	case rq.permanentRetryQueue <- payment:
		logger.Infof("Pagamento %s enfileirado para retry permanente (tentativa %d)",
			payment.CorrelationID, payment.RetryCount)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		logger.Warnf("AVISO: Fila de retry permanente cheia! Pagamento %s descartado", payment.CorrelationID)
		return nil
	}
}

// StartRetryProcessing starts the retry processing workers
func (rq *RetryQueueImpl) StartRetryProcessing(processor services.PaymentProcessorService, repository services.PaymentRepository) error {
	logger.WithField("workers", rq.workerCount*3).Info("Iniciando workers para retry de pagamentos")

	// Start workers for default retry queue
	for i := 0; i < rq.workerCount; i++ {
		workerStop := make(chan struct{})
		rq.workers[i] = workerStop

		rq.wg.Add(1)
		go rq.defaultRetryWorker(i, processor, repository, workerStop)
	}

	// Start workers for fallback retry queue
	for i := 0; i < rq.workerCount; i++ {
		workerIndex := rq.workerCount + i
		workerStop := make(chan struct{})
		rq.workers[workerIndex] = workerStop

		rq.wg.Add(1)
		go rq.fallbackRetryWorker(i, processor, repository, workerStop)
	}

	// Start workers for permanent retry queue
	for i := 0; i < rq.workerCount; i++ {
		workerIndex := rq.workerCount*2 + i
		workerStop := make(chan struct{})
		rq.workers[workerIndex] = workerStop

		rq.wg.Add(1)
		go rq.permanentRetryWorker(i, processor, repository, workerStop)
	}

	return nil
}

// defaultRetryWorker processes payments from the default retry queue
func (rq *RetryQueueImpl) defaultRetryWorker(id int, processor services.PaymentProcessorService, repository services.PaymentRepository, stop chan struct{}) {
	defer rq.wg.Done()

	logger.Infof("Default Retry Worker %d iniciado", id)

	for {
		select {
		case payment := <-rq.defaultRetryQueue:
			rq.processDefaultRetry(payment, processor, repository)
		case <-stop:
			logger.Infof("Default Retry Worker %d parado", id)
			return
		case <-rq.ctx.Done():
			logger.Infof("Default Retry Worker %d parado por contexto", id)
			return
		}
	}
}

// fallbackRetryWorker processes payments from the fallback retry queue
func (rq *RetryQueueImpl) fallbackRetryWorker(id int, processor services.PaymentProcessorService, repository services.PaymentRepository, stop chan struct{}) {
	defer rq.wg.Done()

	logger.Infof("Fallback Retry Worker %d iniciado", id)

	for {
		select {
		case payment := <-rq.fallbackRetryQueue:
			rq.processFallbackRetry(payment, processor, repository)
		case <-stop:
			logger.Infof("Fallback Retry Worker %d parado", id)
			return
		case <-rq.ctx.Done():
			logger.Infof("Fallback Retry Worker %d parado por contexto", id)
			return
		}
	}
}

// permanentRetryWorker processes payments from the permanent retry queue
func (rq *RetryQueueImpl) permanentRetryWorker(id int, processor services.PaymentProcessorService, repository services.PaymentRepository, stop chan struct{}) {
	defer rq.wg.Done()

	logger.Infof("Permanent Retry Worker %d iniciado", id)

	for {
		select {
		case payment := <-rq.permanentRetryQueue:
			rq.processPermanentRetry(payment, processor, repository)
		case <-stop:
			logger.Infof("Permanent Retry Worker %d parado", id)
			return
		case <-rq.ctx.Done():
			logger.Infof("Permanent Retry Worker %d parado por contexto", id)
			return
		}
	}
}

// processDefaultRetry processes payments from the default retry queue
func (rq *RetryQueueImpl) processDefaultRetry(payment *entities.Payment, processor services.PaymentProcessorService, repository services.PaymentRepository) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Check if we should bypass circuit breaker for critical retries
	shouldBypass := payment.RetryCount >= 2 && rq.shouldBypassCircuitBreaker()

	var err error
	var processorType entities.ProcessorType

	if shouldBypass {
		logger.Warnf("Tentando bypass do circuit breaker para pagamento crítico %s", payment.CorrelationID)
		err = rq.tryDirectProcessing(ctx, payment, processor, true) // force default
	} else {
		// MODIFICAÇÃO: Usar ProcessPayment ao invés de ProcessWithDefault para ter modo emergência
		processorType, err = processor.ProcessPayment(ctx, payment)
	}

	if err != nil {
		logger.Errorf("Erro ao reprocessar pagamento no default: %v", err)

		// Try fallback if default fails
		if rq.shouldTryFallback(payment) {
			if retryErr := rq.EnqueueForFallbackRetry(ctx, payment); retryErr != nil {
				logger.Errorf("Erro ao enfileirar para fallback retry: %v", retryErr)
				rq.handleFailedPayment(ctx, payment, repository)
			}
		} else {
			rq.handleFailedPayment(ctx, payment, repository)
		}
		return
	}

	// Success - mark as processed using the correct processor type
	var processorTypeToMark entities.ProcessorType
	if shouldBypass {
		processorTypeToMark = entities.ProcessorTypeDefault
	} else {
		processorTypeToMark = processorType
	}
	payment.MarkAsProcessed(processorTypeToMark)
	if err := repository.Update(ctx, payment); err != nil {
		logger.Errorf("Erro ao atualizar pagamento %s: %v", payment.CorrelationID, err)
	}
	logger.Infof("Pagamento %s reprocessado com sucesso no %s", payment.CorrelationID, string(processorTypeToMark))
}

// processFallbackRetry processes payments from the fallback retry queue
func (rq *RetryQueueImpl) processFallbackRetry(payment *entities.Payment, processor services.PaymentProcessorService, repository services.PaymentRepository) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Check if we should bypass circuit breaker for critical retries
	shouldBypass := payment.RetryCount >= 2 && rq.shouldBypassCircuitBreaker()

	var err error
	var processorType entities.ProcessorType

	if shouldBypass {
		logger.Warnf("Tentando bypass do circuit breaker fallback para pagamento crítico %s", payment.CorrelationID)
		err = rq.tryDirectProcessing(ctx, payment, processor, false) // force fallback
		processorType = entities.ProcessorTypeFallback
	} else {
		// MODIFICAÇÃO: Usar ProcessPayment ao invés de ProcessWithFallback para ter modo emergência
		processorType, err = processor.ProcessPayment(ctx, payment)
	}

	if err != nil {
		logger.Errorf("Erro ao reprocessar pagamento no fallback: %v", err)

		// If fallback fails and we haven't exceeded max retries, try permanent queue
		if payment.RetryCount < rq.maxFallbackRetries {
			if retryErr := rq.EnqueueForPermanentRetry(ctx, payment); retryErr != nil {
				logger.Errorf("Erro ao enfileirar para permanent retry: %v", retryErr)
				rq.handleFailedPayment(ctx, payment, repository)
			}
		} else {
			rq.handleFailedPayment(ctx, payment, repository)
		}
		return
	}

	// Success - mark as processed with the actual processor used
	payment.MarkAsProcessed(processorType)
	if err := repository.Update(ctx, payment); err != nil {
		logger.Errorf("Erro ao atualizar pagamento %s: %v", payment.CorrelationID, err)
	}
	logger.Infof("Pagamento %s reprocessado com sucesso no %s", payment.CorrelationID, string(processorType))
}

// processPermanentRetry processes payments from the permanent retry queue
func (rq *RetryQueueImpl) processPermanentRetry(payment *entities.Payment, processor services.PaymentProcessorService, repository services.PaymentRepository) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// For permanent retry, try both processors with longer backoff
	var err error

	// Try default first
	err = processor.ProcessWithDefault(ctx, payment)
	if err == nil {
		// Success with default
		payment.MarkAsProcessed(entities.ProcessorTypeDefault)
		if updateErr := repository.Update(ctx, payment); updateErr != nil {
			logger.Errorf("Erro ao atualizar pagamento %s: %v", payment.CorrelationID, updateErr)
		}
		logger.Infof("Pagamento %s reprocessado com sucesso no default (permanent retry)", payment.CorrelationID)
		return
	}

	// If default fails, try fallback
	err = processor.ProcessWithFallback(ctx, payment)
	if err == nil {
		// Success with fallback
		payment.MarkAsProcessed(entities.ProcessorTypeFallback)
		if updateErr := repository.Update(ctx, payment); updateErr != nil {
			logger.Errorf("Erro ao atualizar pagamento %s: %v", payment.CorrelationID, updateErr)
		}
		logger.Infof("Pagamento %s reprocessado com sucesso no fallback (permanent retry)", payment.CorrelationID)
		return
	}

	// Both processors failed
	logger.Errorf("Erro ao reprocessar pagamento no permanent retry: %v", err)

	// Check if we should continue retrying or mark as failed
	if payment.RetryCount < rq.maxPermanentRetries {
		// Increment retry count and re-enqueue with exponential backoff
		payment.RetryCount++

		// Wait before re-enqueuing (exponential backoff)
		backoffDuration := time.Duration(payment.RetryCount) * rq.permanentTickerInterval
		time.Sleep(backoffDuration)

		if retryErr := rq.EnqueueForPermanentRetry(ctx, payment); retryErr != nil {
			logger.Errorf("Erro ao re-enfileirar para permanent retry: %v", retryErr)
			rq.handleFailedPayment(ctx, payment, repository)
		} else {
			logger.Infof("Pagamento %s re-enfileirado para permanent retry (tentativa %d)", payment.CorrelationID, payment.RetryCount)
		}
	} else {
		// Exceeded max permanent retries, mark as failed
		rq.handleFailedPayment(ctx, payment, repository)
	}
}

// shouldBypassCircuitBreaker determines if we should bypass circuit breaker for critical payments
func (rq *RetryQueueImpl) shouldBypassCircuitBreaker() bool {
	// Bypass circuit breaker if both queues are getting full (emergency mode)
	defaultQueueSize := len(rq.defaultRetryQueue)
	fallbackQueueSize := len(rq.fallbackRetryQueue)
	permanentQueueSize := len(rq.permanentRetryQueue)

	totalRetryLoad := defaultQueueSize + fallbackQueueSize + permanentQueueSize
	queueCapacity := cap(rq.defaultRetryQueue)

	// If retry queues are > 80% full, enable bypass mode
	loadPercentage := float64(totalRetryLoad) / float64(queueCapacity*3) // 3 queues

	if loadPercentage > 0.8 {
		logger.Warnf("Sistema em modo emergência - bypass de circuit breaker habilitado (carga: %.1f%%)", loadPercentage*100)
		return true
	}

	return false
}

// shouldTryFallback determines if we should try fallback processor
func (rq *RetryQueueImpl) shouldTryFallback(payment *entities.Payment) bool {
	return payment.RetryCount < rq.maxDefaultRetries
}

// tryDirectProcessing bypasses circuit breaker and tries direct processing
func (rq *RetryQueueImpl) tryDirectProcessing(ctx context.Context, payment *entities.Payment, processor services.PaymentProcessorService, useDefault bool) error {
	// This is a direct call bypassing circuit breaker - use with caution
	logger.Warnf("BYPASS: Tentativa direta de processamento para %s", payment.CorrelationID)

	// Try direct processing with shorter timeout to fail fast
	shortCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if useDefault {
		_, err := processor.ProcessPayment(shortCtx, payment)
		return err
	} else {
		return processor.ProcessWithFallback(shortCtx, payment)
	}
}

// handleFailedPayment handles a payment that failed all retry attempts
func (rq *RetryQueueImpl) handleFailedPayment(ctx context.Context, payment *entities.Payment, repository services.PaymentRepository) {
	payment.MarkAsFailed()
	if err := repository.Update(ctx, payment); err != nil {
		logger.Errorf("Erro ao marcar pagamento %s como failed: %v", payment.CorrelationID, err)
	}
	logger.Errorf("Pagamento %s marcado como FAILED após esgotar todas as tentativas", payment.CorrelationID)
}

// handleRetry handles the retry logic for a payment
func (q *RetryQueueImpl) handleRetry(payment *entities.Payment, processorType entities.ProcessorType) {
	maxRetries := q.maxDefaultRetries
	if processorType == entities.ProcessorTypeFallback {
		maxRetries = q.maxFallbackRetries
	}

	if payment.RetryCount >= maxRetries {
		payment.Status = entities.PaymentStatusDiscarded
		logger.Warnf("Pagamento %s descartado após %d tentativas", payment.CorrelationID, payment.RetryCount)
		return
	}

	payment.RetryCount++
}

// Stop stops all retry workers
func (rq *RetryQueueImpl) Stop() error {
	logger.Info("Parando serviço de retry...")

	rq.mutex.Lock()
	defer rq.mutex.Unlock()

	// Signal all workers to stop
	for _, workerStop := range rq.workers {
		if workerStop != nil {
			close(workerStop)
		}
	}

	// Cancel context
	rq.cancel()

	// Wait for all workers to finish
	done := make(chan struct{})
	go func() {
		rq.wg.Wait()
		close(done)
	}()

	// Wait with timeout
	select {
	case <-done:
		logger.Info("Todos os workers de retry pararam com sucesso")
	case <-time.After(10 * time.Second):
		logger.Warn("AVISO: Timeout ao parar workers de retry")
	}

	// Close channels
	close(rq.defaultRetryQueue)
	close(rq.fallbackRetryQueue)
	close(rq.permanentRetryQueue)

	return nil
}

// GetQueueStats returns statistics about the retry queues
func (rq *RetryQueueImpl) GetQueueStats() map[string]interface{} {
	rq.mutex.RLock()
	defer rq.mutex.RUnlock()

	return map[string]interface{}{
		"default_queue_size":        len(rq.defaultRetryQueue),
		"fallback_queue_size":       len(rq.fallbackRetryQueue),
		"permanent_queue_size":      len(rq.permanentRetryQueue), // Nova métrica
		"worker_count":              rq.workerCount,
		"max_default_retries":       rq.maxDefaultRetries,
		"max_fallback_retries":      rq.maxFallbackRetries,
		"max_permanent_retries":     rq.maxPermanentRetries, // Nova métrica
		"ticker_interval":           rq.tickerInterval.String(),
		"permanent_ticker_interval": rq.permanentTickerInterval.String(), // Nova métrica
	}
}
