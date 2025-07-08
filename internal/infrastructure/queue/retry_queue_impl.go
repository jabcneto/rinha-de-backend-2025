package queue

import (
	"context"
	"fmt"
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

// processDefaultRetry processes a payment retry with default processor
func (rq *RetryQueueImpl) processDefaultRetry(payment *entities.Payment, processor services.PaymentProcessorService, repository services.PaymentRepository) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Check if payment can still be retried
	if payment.RetryCount >= rq.maxDefaultRetries {
		// Re-enqueue for fallback retry
		if err := rq.EnqueueForFallbackRetry(ctx, payment); err != nil {
			logger.WithField("correlation_id", payment.CorrelationID).
				WithError(err).
				Error("Erro ao reenfileirar pagamento para retry fallback")
		}
		return
	}

	// Wait for retry interval if needed
	if payment.NextRetryAt != nil && payment.NextRetryAt.After(time.Now()) {
		time.Sleep(payment.NextRetryAt.Sub(time.Now()))
	}

	logger.WithField("correlation_id", payment.CorrelationID).
		WithField("retry_count", payment.RetryCount).
		Info("Tentando reprocessar pagamento no processador default")

	// Try to process with default processor only
	processorType, err := rq.processWithSpecificProcessor(ctx, processor, payment, entities.ProcessorTypeDefault)
	if err != nil {
		logger.WithField("correlation_id", payment.CorrelationID).
			WithError(err).
			Error("Erro ao reprocessar pagamento no default")

		// Mark for retry with exponential backoff
		if payment.RetryCount < rq.maxDefaultRetries {
			payment.RetryCount++
			nextRetry := time.Now().Add(time.Duration(payment.RetryCount) * rq.tickerInterval)
			payment.NextRetryAt = &nextRetry
			// Still has retries left, re-enqueue
			if enqErr := rq.EnqueueForDefaultRetry(ctx, payment); enqErr != nil {
				logger.WithField("correlation_id", payment.CorrelationID).
					WithError(enqErr).
					Error("Erro ao reenfileirar pagamento para retry default")
			}
		} else {
			// Exceeded max retries for default, try fallback
			logger.WithField("correlation_id", payment.CorrelationID).
				Info("Máximo de tentativas no default excedido, movendo para fallback")
			payment.RetryCount = 0 // Reset retry count for fallback
			if enqErr := rq.EnqueueForFallbackRetry(ctx, payment); enqErr != nil {
				logger.WithField("correlation_id", payment.CorrelationID).
					WithError(enqErr).
					Error("Erro ao reenfileirar pagamento para retry fallback")
			}
		}

		// Update payment in database
		if updateErr := repository.Update(ctx, payment); updateErr != nil {
			logger.WithField("correlation_id", payment.CorrelationID).
				WithError(updateErr).
				Error("Erro ao atualizar pagamento")
		}
		return
	}

	// Payment processed successfully
	logger.WithField("correlation_id", payment.CorrelationID).
		WithField("processor_type", processorType).
		Info("Pagamento reprocessado com sucesso no processador default")
	payment.Status = entities.PaymentStatusProcessed
	payment.ProcessorType = processorType
	now := time.Now()
	payment.ProcessedAt = &now
	payment.UpdatedAt = now

	// Update payment in database
	if err := repository.Update(ctx, payment); err != nil {
		logger.WithField("correlation_id", payment.CorrelationID).
			WithError(err).
			Error("Erro ao atualizar pagamento processado")
	}

	// Update summary
	if err := repository.UpdateSummary(ctx, processorType, payment.Amount); err != nil {
		logger.WithField("processor_type", processorType).
			WithError(err).
			Error("Erro ao atualizar resumo")
	}
}

// processFallbackRetry processes a single payment retry with fallback processor
func (rq *RetryQueueImpl) processFallbackRetry(payment *entities.Payment, processor services.PaymentProcessorService, repository services.PaymentRepository) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Check if payment is ready for retry (respecting backoff)
	if payment.NextRetryAt != nil && payment.NextRetryAt.After(time.Now()) {
		// Schedule for later processing using a timer instead of blocking sleep
		delay := time.Until(*payment.NextRetryAt)

		// Use a timer that can be cancelled and doesn't block the worker
		timer := time.NewTimer(delay)
		go func() {
			defer timer.Stop()
			select {
			case <-timer.C:
				// Time has elapsed, re-enqueue for processing
				if err := rq.EnqueueForFallbackRetry(context.Background(), payment); err != nil {
					logger.WithField("correlation_id", payment.CorrelationID).
						WithError(err).
						Error("Erro ao reenfileirar pagamento para retry fallback após delay")
				}
			case <-rq.ctx.Done():
				// Service is shutting down, don't re-enqueue
				logger.WithField("correlation_id", payment.CorrelationID).
					Debug("Retry cancelado devido ao shutdown do serviço")
				return
			}
		}()
		return
	}

	logger.WithField("correlation_id", payment.CorrelationID).
		WithField("retry_count", payment.RetryCount).
		Info("Tentando reprocessar pagamento no processador fallback")

	// Try to process with fallback processor only
	processorType, err := rq.processWithSpecificProcessor(ctx, processor, payment, entities.ProcessorTypeFallback)
	if err != nil {
		logger.WithField("correlation_id", payment.CorrelationID).
			WithError(err).
			Error("Erro ao reprocessar pagamento no fallback")

		// Mark for retry with exponential backoff
		if payment.RetryCount < rq.maxFallbackRetries {
			payment.RetryCount++
			nextRetry := time.Now().Add(time.Duration(payment.RetryCount) * rq.tickerInterval)
			payment.NextRetryAt = &nextRetry
			// Still has retries left, re-enqueue
			if enqErr := rq.EnqueueForFallbackRetry(ctx, payment); enqErr != nil {
				logger.WithField("correlation_id", payment.CorrelationID).
					WithError(enqErr).
					Error("Erro ao reenfileirar pagamento para retry fallback")
			}
		} else {
			// Exceeded max retries for fallback, move to permanent retry
			logger.WithField("correlation_id", payment.CorrelationID).
				Info("Máximo de tentativas no fallback excedido, movendo para retry permanente")
			payment.RetryCount = 0 // Reset retry count for permanent retry
			if enqErr := rq.EnqueueForPermanentRetry(ctx, payment); enqErr != nil {
				logger.WithField("correlation_id", payment.CorrelationID).
					WithError(enqErr).
					Error("Erro ao reenfileirar pagamento para retry permanente")
			}
		}

		// Update payment in database
		if updateErr := repository.Update(ctx, payment); updateErr != nil {
			logger.WithField("correlation_id", payment.CorrelationID).
				WithError(updateErr).
				Error("Erro ao atualizar pagamento")
		}
		return
	}

	// Payment processed successfully
	logger.WithField("correlation_id", payment.CorrelationID).
		WithField("processor_type", processorType).
		Info("Pagamento reprocessado com sucesso no processador fallback")
	payment.Status = entities.PaymentStatusProcessed
	payment.ProcessorType = processorType
	now := time.Now()
	payment.ProcessedAt = &now
	payment.UpdatedAt = now

	// Update payment in database
	if err := repository.Update(ctx, payment); err != nil {
		logger.WithField("correlation_id", payment.CorrelationID).
			WithError(err).
			Error("Erro ao atualizar pagamento processado")
	}

	// Update summary
	if err := repository.UpdateSummary(ctx, processorType, payment.Amount); err != nil {
		logger.WithField("processor_type", processorType).
			WithError(err).
			Error("Erro ao atualizar resumo")
	}
}

// processPermanentRetry processes a single payment retry with permanent processor
func (rq *RetryQueueImpl) processPermanentRetry(payment *entities.Payment, processor services.PaymentProcessorService, repository services.PaymentRepository) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Check if payment is ready for retry (respecting backoff)
	if payment.NextRetryAt != nil && payment.NextRetryAt.After(time.Now()) {
		// Schedule for later processing using a timer instead of blocking sleep
		delay := time.Until(*payment.NextRetryAt)

		// Use a timer that can be cancelled and doesn't block the worker
		timer := time.NewTimer(delay)
		go func() {
			defer timer.Stop()
			select {
			case <-timer.C:
				// Time has elapsed, re-enqueue for processing
				if err := rq.EnqueueForPermanentRetry(context.Background(), payment); err != nil {
					logger.WithField("correlation_id", payment.CorrelationID).
						WithError(err).
						Error("Erro ao reenfileirar pagamento para retry permanente após delay")
				}
			case <-rq.ctx.Done():
				// Service is shutting down, don't re-enqueue
				logger.WithField("correlation_id", payment.CorrelationID).
					Debug("Retry cancelado devido ao shutdown do serviço")
				return
			}
		}()
		return
	}

	logger.WithField("correlation_id", payment.CorrelationID).
		WithField("retry_count", payment.RetryCount).
		Info("Tentando reprocessar pagamento na fila permanente")

	// Try to process with both processors (using ProcessWithPermanent logic)
	var processorType entities.ProcessorType
	var err error

	// First try default processor
	err = processor.ProcessWithDefault(ctx, payment)
	if err == nil {
		processorType = entities.ProcessorTypeDefault
		logger.WithField("correlation_id", payment.CorrelationID).
			WithField("processor_type", processorType).
			Info("Pagamento processado com sucesso no processador default (fila permanente)")
	} else {
		logger.WithField("correlation_id", payment.CorrelationID).
			WithError(err).
			Info("Falha no default para pagamento (fila permanente), tentando fallback")

		// If default fails, try fallback processor
		err = processor.ProcessWithFallback(ctx, payment)
		if err == nil {
			processorType = entities.ProcessorTypeFallback
			logger.WithField("correlation_id", payment.CorrelationID).
				WithField("processor_type", processorType).
				Info("Pagamento processado com sucesso no processador fallback (fila permanente)")
		}
	}

	if err != nil {
		logger.WithField("correlation_id", payment.CorrelationID).
			WithError(err).
			Error("Erro ao reprocessar pagamento na fila permanente")

		// Mark for retry with exponential backoff
		if payment.RetryCount < rq.maxPermanentRetries {
			payment.RetryCount++
			nextRetry := time.Now().Add(time.Duration(payment.RetryCount) * rq.permanentTickerInterval)
			payment.NextRetryAt = &nextRetry
			// Still has retries left, re-enqueue
			if enqErr := rq.EnqueueForPermanentRetry(ctx, payment); enqErr != nil {
				logger.WithField("correlation_id", payment.CorrelationID).
					WithError(enqErr).
					Error("Erro ao reenfileirar pagamento para retry permanente")
			}
		} else {
			// Exceeded max retries, mark as permanently failed
			logger.WithField("correlation_id", payment.CorrelationID).
				Info("Máximo de tentativas excedido, marcando como falhou permanentemente")
			payment.Status = entities.PaymentStatusFailed
			now := time.Now()
			payment.UpdatedAt = now
		}

		// Update payment in database
		if updateErr := repository.Update(ctx, payment); updateErr != nil {
			logger.WithField("correlation_id", payment.CorrelationID).
				WithError(updateErr).
				Error("Erro ao atualizar pagamento")
		}
		return
	}

	// Payment processed successfully
	logger.WithField("correlation_id", payment.CorrelationID).
		WithField("processor_type", processorType).
		Info("Pagamento reprocessado com sucesso no processador")
	payment.Status = entities.PaymentStatusProcessed
	payment.ProcessorType = processorType
	now := time.Now()
	payment.ProcessedAt = &now
	payment.UpdatedAt = now

	// Reset retry information since payment was successful
	payment.RetryCount = 0
	payment.NextRetryAt = nil

	// Update payment in database
	if err := repository.Update(ctx, payment); err != nil {
		logger.WithField("correlation_id", payment.CorrelationID).
			WithError(err).
			Error("Erro ao atualizar pagamento processado")
	}

	// Update summary
	if err := repository.UpdateSummary(ctx, processorType, payment.Amount); err != nil {
		logger.WithField("processor_type", processorType).
			WithError(err).
			Error("Erro ao atualizar resumo")
	}
}

// processWithSpecificProcessor tries to process a payment with a specific processor type
func (rq *RetryQueueImpl) processWithSpecificProcessor(ctx context.Context, processor services.PaymentProcessorService, payment *entities.Payment, processorType entities.ProcessorType) (entities.ProcessorType, error) {
	var err error

	switch processorType {
	case entities.ProcessorTypeDefault:
		err = processor.ProcessWithDefault(ctx, payment)
		if err != nil {
			return "", err
		}
		return entities.ProcessorTypeDefault, nil
	case entities.ProcessorTypeFallback:
		err = processor.ProcessWithFallback(ctx, payment)
		if err != nil {
			return "", err
		}
		return entities.ProcessorTypeFallback, nil
	default:
		return "", fmt.Errorf("tipo de processador inválido: %s", processorType)
	}
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
