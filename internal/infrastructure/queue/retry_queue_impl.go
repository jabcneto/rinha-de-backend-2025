package queue

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"rinha-backend-clean/internal/domain/entities"
	"rinha-backend-clean/internal/domain/services"
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
		log.Printf("Pagamento %s enfileirado para retry no processador default (tentativa %d)",
			payment.CorrelationID, payment.RetryCount)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		log.Printf("AVISO: Fila de retry default cheia! Pagamento %s descartado", payment.CorrelationID)
		return fmt.Errorf("fila de retry default cheia")
	}
}

// EnqueueForFallbackRetry enqueues a payment for retry with fallback processor
func (rq *RetryQueueImpl) EnqueueForFallbackRetry(ctx context.Context, payment *entities.Payment) error {
	select {
	case rq.fallbackRetryQueue <- payment:
		log.Printf("Pagamento %s enfileirado para retry no processador fallback (tentativa %d)",
			payment.CorrelationID, payment.RetryCount)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		log.Printf("AVISO: Fila de retry fallback cheia! Pagamento %s descartado", payment.CorrelationID)
		return fmt.Errorf("fila de retry fallback cheia")
	}
}

// EnqueueForPermanentRetry enqueues a payment for permanent retry processing
func (rq *RetryQueueImpl) EnqueueForPermanentRetry(ctx context.Context, payment *entities.Payment) error {
	select {
	case rq.permanentRetryQueue <- payment:
		log.Printf("Pagamento %s enfileirado para retry permanente (tentativa %d)",
			payment.CorrelationID, payment.RetryCount)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		log.Printf("AVISO: Fila de retry permanente cheia! Pagamento %s descartado", payment.CorrelationID)
		return fmt.Errorf("fila de retry permanente cheia")
	}
}

// StartRetryProcessing starts the retry processing workers
func (rq *RetryQueueImpl) StartRetryProcessing(processor services.PaymentProcessorService, repository services.PaymentRepository) error {
	log.Printf("Iniciando %d workers para retry de pagamentos...", rq.workerCount*3)

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

	log.Printf("Default Retry Worker %d iniciado", id)
	ticker := time.NewTicker(rq.tickerInterval)
	defer ticker.Stop()

	for {
		select {
		case payment := <-rq.defaultRetryQueue:
			rq.processDefaultRetry(payment, processor, repository)
		case <-ticker.C:
			// Periodically check for payments ready for retry
			continue
		case <-stop:
			log.Printf("Default Retry Worker %d parado", id)
			return
		case <-rq.ctx.Done():
			log.Printf("Default Retry Worker %d parado por contexto", id)
			return
		}
	}
}

// fallbackRetryWorker processes payments from the fallback retry queue
func (rq *RetryQueueImpl) fallbackRetryWorker(id int, processor services.PaymentProcessorService, repository services.PaymentRepository, stop chan struct{}) {
	defer rq.wg.Done()

	log.Printf("Fallback Retry Worker %d iniciado", id)
	ticker := time.NewTicker(rq.tickerInterval)
	defer ticker.Stop()

	for {
		select {
		case payment := <-rq.fallbackRetryQueue:
			rq.processFallbackRetry(payment, processor, repository)
		case <-ticker.C:
			// Periodically check for payments ready for retry
			continue
		case <-stop:
			log.Printf("Fallback Retry Worker %d parado", id)
			return
		case <-rq.ctx.Done():
			log.Printf("Fallback Retry Worker %d parado por contexto", id)
			return
		}
	}
}

// permanentRetryWorker processes payments from the permanent retry queue
func (rq *RetryQueueImpl) permanentRetryWorker(id int, processor services.PaymentProcessorService, repository services.PaymentRepository, stop chan struct{}) {
	defer rq.wg.Done()

	log.Printf("Permanent Retry Worker %d iniciado", id)
	ticker := time.NewTicker(rq.permanentTickerInterval)
	defer ticker.Stop()

	for {
		select {
		case payment := <-rq.permanentRetryQueue:
			rq.processPermanentRetry(payment, processor, repository)
		case <-ticker.C:
			// Periodically check for payments ready for retry
			continue
		case <-stop:
			log.Printf("Permanent Retry Worker %d parado", id)
			return
		case <-rq.ctx.Done():
			log.Printf("Permanent Retry Worker %d parado por contexto", id)
			return
		}
	}
}

// processDefaultRetry processes a single payment retry with default processor
func (rq *RetryQueueImpl) processDefaultRetry(payment *entities.Payment, processor services.PaymentProcessorService, repository services.PaymentRepository) {
	// Check if payment is ready for retry (respecting backoff)
	if !payment.IsReadyForRetry() {
		// Re-enqueue for later processing
		go func() {
			time.Sleep(time.Until(*payment.NextRetryAt))
			if err := rq.EnqueueForDefaultRetry(context.Background(), payment); err != nil {
				log.Printf("Erro ao reenfileirar pagamento %s para retry default: %v", payment.CorrelationID, err)
			}
		}()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Printf("Tentando reprocessar pagamento %s no processador default (tentativa %d)",
		payment.CorrelationID, payment.RetryCount)

	// Try to process with default processor only
	processorType, err := rq.processWithSpecificProcessor(ctx, processor, payment, entities.ProcessorTypeDefault)
	if err != nil {
		log.Printf("Erro ao reprocessar pagamento %s no default: %v", payment.CorrelationID, err)

		// Mark for retry with exponential backoff
		if payment.MarkForRetry(err, rq.maxDefaultRetries) {
			// Still has retries left, re-enqueue
			if enqErr := rq.EnqueueForDefaultRetry(ctx, payment); enqErr != nil {
				log.Printf("Erro ao reenfileirar pagamento %s para retry default: %v", payment.CorrelationID, enqErr)
			}
		} else {
			// Exceeded max retries for default, try fallback
			log.Printf("Máximo de tentativas no default excedido para %s, movendo para fallback", payment.CorrelationID)
			payment.RetryCount = 0 // Reset retry count for fallback
			if enqErr := rq.EnqueueForFallbackRetry(ctx, payment); enqErr != nil {
				log.Printf("Erro ao reenfileirar pagamento %s para retry fallback: %v", payment.CorrelationID, enqErr)
			}
		}

		// Update payment in database
		if updateErr := repository.Update(ctx, payment); updateErr != nil {
			log.Printf("Erro ao atualizar pagamento %s: %v", payment.CorrelationID, updateErr)
		}
		return
	}

	// Payment processed successfully
	log.Printf("Pagamento %s reprocessado com sucesso no processador %s", payment.CorrelationID, processorType)
	payment.Status = entities.PaymentStatusCompleted
	payment.ProcessorType = processorType
	now := time.Now()
	payment.ProcessedAt = &now
	payment.UpdatedAt = now

	// Update payment in database
	if err := repository.Update(ctx, payment); err != nil {
		log.Printf("Erro ao atualizar pagamento processado %s: %v", payment.CorrelationID, err)
	}

	// Update summary
	if err := repository.UpdateSummary(ctx, processorType, payment.Amount); err != nil {
		log.Printf("Erro ao atualizar resumo para %s: %v", processorType, err)
	}
}

// processFallbackRetry processes a single payment retry with fallback processor
func (rq *RetryQueueImpl) processFallbackRetry(payment *entities.Payment, processor services.PaymentProcessorService, repository services.PaymentRepository) {
	// Check if payment is ready for retry (respecting backoff)
	if !payment.IsReadyForRetry() {
		// Re-enqueue for later processing
		go func() {
			time.Sleep(time.Until(*payment.NextRetryAt))
			if err := rq.EnqueueForFallbackRetry(context.Background(), payment); err != nil {
				log.Printf("Erro ao reenfileirar pagamento %s para retry fallback: %v", payment.CorrelationID, err)
			}
		}()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Printf("Tentando reprocessar pagamento %s no processador fallback (tentativa %d)",
		payment.CorrelationID, payment.RetryCount)

	// Try to process with fallback processor only
	processorType, err := rq.processWithSpecificProcessor(ctx, processor, payment, entities.ProcessorTypeFallback)
	if err != nil {
		log.Printf("Erro ao reprocessar pagamento %s no fallback: %v", payment.CorrelationID, err)

		// Mark for retry with exponential backoff
		if payment.MarkForRetry(err, rq.maxFallbackRetries) {
			// Still has retries left, re-enqueue
			if enqErr := rq.EnqueueForFallbackRetry(ctx, payment); enqErr != nil {
				log.Printf("Erro ao reenfileirar pagamento %s para retry fallback: %v", payment.CorrelationID, enqErr)
			}
		} else {
			// Exceeded max retries for fallback, move to permanent retry
			log.Printf("Máximo de tentativas no fallback excedido para %s, movendo para retry permanente", payment.CorrelationID)
			payment.RetryCount = 0 // Reset retry count for permanent retry
			if enqErr := rq.EnqueueForPermanentRetry(ctx, payment); enqErr != nil {
				log.Printf("Erro ao reenfileirar pagamento %s para retry permanente: %v", payment.CorrelationID, enqErr)
			}
		}

		// Update payment in database
		if updateErr := repository.Update(ctx, payment); updateErr != nil {
			log.Printf("Erro ao atualizar pagamento %s: %v", payment.CorrelationID, updateErr)
		}
		return
	}

	// Payment processed successfully
	log.Printf("Pagamento %s reprocessado com sucesso no processador %s", payment.CorrelationID, processorType)
	payment.Status = entities.PaymentStatusCompleted
	payment.ProcessorType = processorType
	now := time.Now()
	payment.ProcessedAt = &now
	payment.UpdatedAt = now

	// Update payment in database
	if err := repository.Update(ctx, payment); err != nil {
		log.Printf("Erro ao atualizar pagamento processado %s: %v", payment.CorrelationID, err)
	}

	// Update summary
	if err := repository.UpdateSummary(ctx, processorType, payment.Amount); err != nil {
		log.Printf("Erro ao atualizar resumo para %s: %v", processorType, err)
	}
}

// processPermanentRetry processes a single payment retry with permanent processor
func (rq *RetryQueueImpl) processPermanentRetry(payment *entities.Payment, processor services.PaymentProcessorService, repository services.PaymentRepository) {
	// Check if payment is ready for retry (respecting backoff)
	if !payment.IsReadyForRetry() {
		// Re-enqueue for later processing
		go func() {
			time.Sleep(time.Until(*payment.NextRetryAt))
			if err := rq.EnqueueForPermanentRetry(context.Background(), payment); err != nil {
				log.Printf("Erro ao reenfileirar pagamento %s para retry permanente: %v", payment.CorrelationID, err)
			}
		}()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Printf("Tentando reprocessar pagamento %s na fila permanente (tentativa %d)",
		payment.CorrelationID, payment.RetryCount)

	// Try to process with both processors (using ProcessWithPermanent logic)
	var processorType entities.ProcessorType
	var err error

	// First try default processor
	err = processor.ProcessWithDefault(ctx, payment)
	if err == nil {
		processorType = entities.ProcessorTypeDefault
		log.Printf("Pagamento %s processado com sucesso no processador default (fila permanente)", payment.CorrelationID)
	} else {
		log.Printf("Falha no default para pagamento %s (fila permanente): %v. Tentando fallback...", payment.CorrelationID, err)

		// If default fails, try fallback processor
		err = processor.ProcessWithFallback(ctx, payment)
		if err == nil {
			processorType = entities.ProcessorTypeFallback
			log.Printf("Pagamento %s processado com sucesso no processador fallback (fila permanente)", payment.CorrelationID)
		}
	}

	if err != nil {
		log.Printf("Erro ao reprocessar pagamento %s na fila permanente: %v", payment.CorrelationID, err)

		// Mark for retry with exponential backoff
		if payment.MarkForRetry(err, rq.maxPermanentRetries) {
			// Still has retries left, re-enqueue
			if enqErr := rq.EnqueueForPermanentRetry(ctx, payment); enqErr != nil {
				log.Printf("Erro ao reenfileirar pagamento %s para retry permanente: %v", payment.CorrelationID, enqErr)
			}
		} else {
			// Exceeded max retries, mark as permanently failed
			log.Printf("Máximo de tentativas excedido para %s, marcando como falhou permanentemente", payment.CorrelationID)
			payment.Status = entities.PaymentStatusFailed
			now := time.Now()
			payment.UpdatedAt = now
		}

		// Update payment in database
		if updateErr := repository.Update(ctx, payment); updateErr != nil {
			log.Printf("Erro ao atualizar pagamento %s: %v", payment.CorrelationID, updateErr)
		}
		return
	}

	// Payment processed successfully
	log.Printf("Pagamento %s reprocessado com sucesso no processador %s", payment.CorrelationID, processorType)
	payment.Status = entities.PaymentStatusCompleted
	payment.ProcessorType = processorType
	now := time.Now()
	payment.ProcessedAt = &now
	payment.UpdatedAt = now

	// Reset retry information since payment was successful
	payment.ResetRetryInfo()

	// Update payment in database
	if err := repository.Update(ctx, payment); err != nil {
		log.Printf("Erro ao atualizar pagamento processado %s: %v", payment.CorrelationID, err)
	}

	// Update summary
	if err := repository.UpdateSummary(ctx, processorType, payment.Amount); err != nil {
		log.Printf("Erro ao atualizar resumo para %s: %v", processorType, err)
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
	log.Println("Parando serviço de retry...")

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
		log.Println("Todos os workers de retry pararam com sucesso")
	case <-time.After(10 * time.Second):
		log.Println("AVISO: Timeout ao parar workers de retry")
	}

	// Close channels
	close(rq.defaultRetryQueue)
	close(rq.fallbackRetryQueue)
	close(rq.permanentRetryQueue) // Fecha a nova fila

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
