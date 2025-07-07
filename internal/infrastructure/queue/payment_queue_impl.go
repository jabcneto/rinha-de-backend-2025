package queue

import (
	"context"
	"log"
	"sync"
	"time"

	"rinha-backend-clean/internal/domain/entities"
	"rinha-backend-clean/internal/domain/services"
)

// PaymentQueueImpl implements the QueueService interface
type PaymentQueueImpl struct {
	queue       chan *entities.Payment
	workerCount int
	workers     []chan struct{}
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	mutex       sync.RWMutex
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

// Enqueue adds a payment to the processing queue
func (q *PaymentQueueImpl) Enqueue(ctx context.Context, payment *entities.Payment) error {
	select {
	case q.queue <- payment:
		log.Printf("Pagamento %s enfileirado com sucesso", payment.CorrelationID)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		log.Printf("AVISO: Fila de pagamentos cheia! Pagamento %s descartado", payment.CorrelationID)
		return nil // Don't block, just drop the payment
	}
}

// StartProcessing starts the background payment processing with multiple workers
func (q *PaymentQueueImpl) StartProcessing(ctx context.Context, processor services.PaymentProcessorService, repository services.PaymentRepository) error {
	log.Printf("Iniciando %d workers para processamento de pagamentos...", q.workerCount)

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

	log.Printf("Worker %d iniciado", id)

	for {
		select {
		case payment := <-q.queue:
			q.processPayment(payment, processor, repository)
		case <-stop:
			log.Printf("Worker %d parado", id)
			return
		case <-q.ctx.Done():
			log.Printf("Worker %d parado por contexto", id)
			return
		}
	}
}

// processPayment processes a single payment
func (q *PaymentQueueImpl) processPayment(payment *entities.Payment, processor services.PaymentProcessorService, repository services.PaymentRepository) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Try to process payment first
	processorType, err := processor.ProcessPayment(ctx, payment)
	if err != nil {
		log.Printf("Erro ao processar pagamento %s: %v", payment.CorrelationID, err)

		// Mark payment as failed and try to update
		payment.MarkAsFailed()
		if updateErr := repository.Update(ctx, payment); updateErr != nil {
			log.Printf("Erro ao atualizar status do pagamento %s: %v", payment.CorrelationID, updateErr)
		}
		return
	}

	// Mark payment as processed and update
	payment.MarkAsProcessed(processorType)
	if err := repository.Update(ctx, payment); err != nil {
		log.Printf("Erro ao atualizar pagamento %s: %v", payment.CorrelationID, err)
	}

	// Update summary (non-blocking)
	if err := repository.UpdateSummary(ctx, processorType, payment.Amount); err != nil {
		log.Printf("Erro ao atualizar resumo do pagamento %s: %v", payment.CorrelationID, err)
	} else {
		log.Printf("Pagamento %s processado com sucesso via %s", payment.CorrelationID, processorType)
	}
}

// GetQueueSize returns the current queue size
func (q *PaymentQueueImpl) GetQueueSize() int {
	return len(q.queue)
}

// Stop stops the queue processing
func (q *PaymentQueueImpl) Stop(ctx context.Context) error {
	log.Println("Parando processamento da fila...")

	// Stop all workers
	for i, worker := range q.workers {
		if worker != nil {
			close(worker)
			log.Printf("Worker %d parado", i)
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
		log.Println("Todos os workers pararam com sucesso")
		return nil
	case <-ctx.Done():
		log.Println("Timeout ao parar workers")
		return ctx.Err()
	}
}
