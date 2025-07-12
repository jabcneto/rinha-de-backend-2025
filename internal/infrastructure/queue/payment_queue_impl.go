package queue

import (
	"context"
	"rinha-backend-clean/internal/domain/entities"
	"rinha-backend-clean/internal/domain/services"
	"rinha-backend-clean/internal/infrastructure/logger"
	"sync"
	"time"
)

// PaymentQueueImpl implements the QueueService interface
type PaymentQueueImpl struct {
	queue             chan *entities.Payment
	workerCount       int
	workers           []chan struct{}
	wg                sync.WaitGroup
	ctx               context.Context
	cancel            context.CancelFunc
	mutex             sync.RWMutex
	retryQueueService services.RetryQueueService
}

// NewPaymentQueue creates a new payment queue service
func NewPaymentQueue(bufferSize, workerCount int) services.QueueService {
	ctx, cancel := context.WithCancel(context.Background())

	return &PaymentQueueImpl{
		queue:       make(chan *entities.Payment, bufferSize),
		workerCount: workerCount,
		workers:     make([]chan struct{}, workerCount),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// SetRetryQueueService sets the retry queue service for handling failed payments
func (q *PaymentQueueImpl) SetRetryQueueService(retryQueueService services.RetryQueueService) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.retryQueueService = retryQueueService
}

// Enqueue adds a payment to the processing queue
func (q *PaymentQueueImpl) Enqueue(ctx context.Context, payment *entities.Payment) error {
	select {
	case q.queue <- payment:
		logger.Infof("Pagamento %s enfileirado com sucesso", payment.CorrelationID)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		logger.Warnf("AVISO: Fila de pagamentos cheia! Pagamento %s descartado", payment.CorrelationID)
		return nil // Don't block, just drop the payment
	}
}

// StartProcessing starts the background payment processing with multiple workers
func (q *PaymentQueueImpl) StartProcessing(ctx context.Context, processor services.PaymentProcessorService, repository services.PaymentRepository) error {
	logger.Infof("Iniciando %d workers para processamento de pagamentos...", q.workerCount)

	for i := 0; i < q.workerCount; i++ {
		workerStop := make(chan struct{})
		q.workers[i] = workerStop

		q.wg.Add(1)
		go q.worker(i, processor, repository, workerStop)
	}

	return nil
}

// worker processes payments from the queue
func (q *PaymentQueueImpl) worker(id int, processor services.PaymentProcessorService, repository services.PaymentRepository, stop chan struct{}) {
	defer q.wg.Done()

	logger.Infof("Worker %d iniciado", id)

	for {
		select {
		case payment := <-q.queue:
			q.processPayment(payment, processor, repository)
		case <-stop:
			logger.Infof("Worker %d parado", id)
			return
		case <-q.ctx.Done():
			logger.Infof("Worker %d parado por contexto", id)
			return
		}
	}
}

// processPayment processes a single payment
func (q *PaymentQueueImpl) processPayment(payment *entities.Payment, processor services.PaymentProcessorService, repository services.PaymentRepository) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// IGNORA pagamentos descartados
	if payment.Status == entities.PaymentStatusDiscarded {
		logger.Warnf("Pagamento %s ignorado: status descartado", payment.CorrelationID)
		return
	}

	// Try to process payment first
	processorType, err := processor.ProcessPayment(ctx, payment)
	if err != nil {
		logger.Errorf("Erro ao processar pagamento %s: %v", payment.CorrelationID, err)

		// Cancelar pagamento após 3 tentativas, sem enviar para retentativa
		if payment.RetryCount >= 2 { // 1 tentativa inicial + 2 retentativas = 3
			payment.MarkAsDiscarded()
			logger.Warnf("Pagamento %s cancelado após 3 tentativas", payment.CorrelationID)
			if updateErr := repository.Update(ctx, payment); updateErr != nil {
				logger.Errorf("Erro ao atualizar status do pagamento %s: %v", payment.CorrelationID, updateErr)
			}
			return
		}

		// Caso contrário, marcar para retry normalmente
		q.mutex.RLock()
		retryService := q.retryQueueService
		q.mutex.RUnlock()

		if retryService != nil {
			if payment.MarkForRetry(err, 2) {
				if retryErr := retryService.EnqueueForDefaultRetry(ctx, payment); retryErr != nil {
					logger.Errorf("Erro ao enfileirar pagamento %s para retry: %v", payment.CorrelationID, retryErr)
					payment.MarkAsDiscarded()
				} else {
					logger.Infof("Pagamento %s enfileirado para retry (tentativa %d)", payment.CorrelationID, payment.RetryCount)
				}
			} else {
				payment.MarkAsDiscarded()
				logger.Warnf("Pagamento %s cancelado após exceder tentativas", payment.CorrelationID)
			}
		} else {
			payment.MarkAsDiscarded()
			logger.Warnf("Pagamento %s cancelado (sem retry service)", payment.CorrelationID)
		}

		if updateErr := repository.Update(ctx, payment); updateErr != nil {
			logger.Errorf("Erro ao atualizar status do pagamento %s: %v", payment.CorrelationID, updateErr)
		}
		return
	}

	// Mark payment as processed and update
	payment.MarkAsProcessed(processorType)
	if err := repository.Update(ctx, payment); err != nil {
		logger.Errorf("Erro ao atualizar pagamento %s: %v", payment.CorrelationID, err)
	}

	// Update summary (non-blocking)
	if err := repository.UpdateSummary(ctx, processorType, payment.Amount); err != nil {
		logger.Errorf("Erro ao atualizar resumo do pagamento %s: %v", payment.CorrelationID, err)
	} else {
		logger.Infof("Pagamento %s processado com sucesso via %s", payment.CorrelationID, processorType)
	}

	// Atualiza o resumo apenas se o pagamento estiver com status 'processed'
	if payment.Status == entities.PaymentStatus("processed") {
		for {
			err := repository.UpdateSummary(ctx, processorType, payment.Amount)
			if err != nil {
				logger.Errorf("Erro ao atualizar resumo do pagamento %s: %v", payment.CorrelationID, err)
				continue // retenta imediatamente
			} else {
				logger.Infof("Pagamento %s processado com sucesso via %s", payment.CorrelationID, processorType)
				break
			}
		}
	}
}

// GetQueueSize returns the current queue size
func (q *PaymentQueueImpl) GetQueueSize() int {
	return len(q.queue)
}

// Stop stops the queue processing
func (q *PaymentQueueImpl) Stop(ctx context.Context) error {
	logger.Info("Parando processamento da fila...")

	// Stop all workers
	for i, worker := range q.workers {
		if worker != nil {
			close(worker)
			logger.Infof("Worker %d parado", i)
		}
	}

	// Cancel context
	q.cancel()

	// Wait for all workers to finish
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("Todos os workers pararam com sucesso")
		return nil
	case <-ctx.Done():
		logger.Info("Timeout ao parar workers")
		return ctx.Err()
	}
}
