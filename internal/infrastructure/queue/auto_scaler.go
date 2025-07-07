package queue

import (
	"context"
	"log"
	"sync"
	"time"

	"rinha-backend-clean/internal/domain/services"
)

// AutoScaler handles dynamic worker scaling based on queue load
type AutoScaler struct {
	queueService      services.QueueService
	retryQueueService services.RetryQueueService
	processor         services.PaymentProcessorService
	repository        services.PaymentRepository

	// Scaling configuration
	minWorkers         int
	maxWorkers         int
	currentWorkers     int
	scaleUpThreshold   float64 // Queue utilization % to scale up
	scaleDownThreshold float64 // Queue utilization % to scale down
	checkInterval      time.Duration

	// Worker management
	activeWorkers []context.CancelFunc
	workerMutex   sync.RWMutex

	// Metrics
	metrics *ScalingMetrics
	ctx     context.Context
	cancel  context.CancelFunc
}

// ScalingMetrics tracks scaling decisions and performance
type ScalingMetrics struct {
	mutex               sync.RWMutex
	TotalScaleUps       int
	TotalScaleDowns     int
	CurrentQueueSize    int
	QueueUtilization    float64
	ProcessingRate      float64
	LastScalingDecision time.Time
	WorkerEfficiency    float64
}

// NewAutoScaler creates a new auto-scaler instance
func NewAutoScaler(
	queueService services.QueueService,
	retryQueueService services.RetryQueueService,
	processor services.PaymentProcessorService,
	repository services.PaymentRepository,
) *AutoScaler {
	ctx, cancel := context.WithCancel(context.Background())

	return &AutoScaler{
		queueService:       queueService,
		retryQueueService:  retryQueueService,
		processor:          processor,
		repository:         repository,
		minWorkers:         10,              // Mínimo de workers
		maxWorkers:         50,              // Máximo de workers
		currentWorkers:     20,              // Workers iniciais
		scaleUpThreshold:   0.8,             // Scale up quando 80% da fila estiver cheia
		scaleDownThreshold: 0.3,             // Scale down quando 30% da fila estiver cheia
		checkInterval:      5 * time.Second, // Verifica a cada 5 segundos
		activeWorkers:      make([]context.CancelFunc, 0),
		metrics:            &ScalingMetrics{},
		ctx:                ctx,
		cancel:             cancel,
	}
}

// Start begins the auto-scaling monitoring
func (as *AutoScaler) Start() {
	log.Printf("🚀 Auto-scaler iniciado: %d-%d workers, verificação a cada %v",
		as.minWorkers, as.maxWorkers, as.checkInterval)

	// Start initial workers
	as.scaleToWorkers(as.currentWorkers)

	// Start monitoring goroutine
	go as.monitorAndScale()
}

// Stop stops the auto-scaler and all workers
func (as *AutoScaler) Stop() {
	log.Println("🛑 Parando auto-scaler...")
	as.cancel()

	as.workerMutex.Lock()
	defer as.workerMutex.Unlock()

	// Stop all active workers
	for _, cancelFunc := range as.activeWorkers {
		cancelFunc()
	}
	as.activeWorkers = nil
	as.currentWorkers = 0

	log.Println("✅ Auto-scaler parado com sucesso")
}

// monitorAndScale continuously monitors queue metrics and adjusts workers
func (as *AutoScaler) monitorAndScale() {
	ticker := time.NewTicker(as.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			as.evaluateAndScale()
		case <-as.ctx.Done():
			return
		}
	}
}

// evaluateAndScale analyzes current metrics and makes scaling decisions
func (as *AutoScaler) evaluateAndScale() {
	// Get queue statistics
	queueStats := as.getQueueStats()
	retryStats := as.retryQueueService.GetQueueStats()

	// Calculate metrics
	totalQueueSize := queueStats["queue_size"].(int) +
		retryStats["default_queue_size"].(int) +
		retryStats["fallback_queue_size"].(int) +
		retryStats["permanent_queue_size"].(int)

	maxQueueCapacity := 15000 * 4 // Buffer size * number of queues
	utilization := float64(totalQueueSize) / float64(maxQueueCapacity)

	// Update metrics
	as.updateMetrics(totalQueueSize, utilization)

	// Make scaling decision
	targetWorkers := as.calculateTargetWorkers(utilization, totalQueueSize)

	if targetWorkers != as.currentWorkers {
		log.Printf("📊 Queue utilization: %.2f%%, Queue size: %d, Current workers: %d → Target: %d",
			utilization*100, totalQueueSize, as.currentWorkers, targetWorkers)

		as.scaleToWorkers(targetWorkers)
	}
}

// calculateTargetWorkers determines optimal number of workers based on metrics
func (as *AutoScaler) calculateTargetWorkers(utilization float64, queueSize int) int {
	currentWorkers := as.currentWorkers

	// Scale up conditions
	if utilization > as.scaleUpThreshold || queueSize > 5000 {
		// Aggressive scaling for high load
		if utilization > 0.95 || queueSize > 10000 {
			return min(as.maxWorkers, currentWorkers+10) // Add 10 workers
		}
		return min(as.maxWorkers, currentWorkers+5) // Add 5 workers
	}

	// Scale down conditions
	if utilization < as.scaleDownThreshold && queueSize < 1000 {
		// Conservative scaling down
		if utilization < 0.1 && queueSize < 100 {
			return max(as.minWorkers, currentWorkers-5) // Remove 5 workers
		}
		return max(as.minWorkers, currentWorkers-2) // Remove 2 workers
	}

	// No scaling needed
	return currentWorkers
}

// scaleToWorkers adjusts the number of active workers
func (as *AutoScaler) scaleToWorkers(targetWorkers int) {
	as.workerMutex.Lock()
	defer as.workerMutex.Unlock()

	currentWorkers := as.currentWorkers

	if targetWorkers > currentWorkers {
		// Scale up - add workers
		workersToAdd := targetWorkers - currentWorkers
		for i := 0; i < workersToAdd; i++ {
			workerCtx, workerCancel := context.WithCancel(as.ctx)
			as.activeWorkers = append(as.activeWorkers, workerCancel)

			// Start new worker
			go as.dynamicWorker(workerCtx, len(as.activeWorkers)-1)
		}

		as.metrics.mutex.Lock()
		as.metrics.TotalScaleUps++
		as.metrics.LastScalingDecision = time.Now()
		as.metrics.mutex.Unlock()

		log.Printf("⬆️ Scaled UP: %d → %d workers (+%d)", currentWorkers, targetWorkers, workersToAdd)

	} else if targetWorkers < currentWorkers {
		// Scale down - remove workers
		workersToRemove := currentWorkers - targetWorkers
		for i := 0; i < workersToRemove && len(as.activeWorkers) > 0; i++ {
			// Stop the last worker
			lastIndex := len(as.activeWorkers) - 1
			as.activeWorkers[lastIndex]()
			as.activeWorkers = as.activeWorkers[:lastIndex]
		}

		as.metrics.mutex.Lock()
		as.metrics.TotalScaleDowns++
		as.metrics.LastScalingDecision = time.Now()
		as.metrics.mutex.Unlock()

		log.Printf("⬇️ Scaled DOWN: %d → %d workers (-%d)", currentWorkers, targetWorkers, workersToRemove)
	}

	as.currentWorkers = targetWorkers
}

// dynamicWorker is a worker that can process any type of payment queue
func (as *AutoScaler) dynamicWorker(ctx context.Context, workerID int) {
	log.Printf("🔧 Dynamic Worker %d iniciado", workerID)
	defer log.Printf("🔧 Dynamic Worker %d parado", workerID)

	ticker := time.NewTicker(100 * time.Millisecond) // Check queues every 100ms
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Try to process from any available queue
			processed := as.tryProcessFromQueues(ctx)
			if !processed {
				// If no work found, sleep a bit longer
				time.Sleep(500 * time.Millisecond)
			}
		case <-ctx.Done():
			return
		}
	}
}

// tryProcessFromQueues attempts to process work from any available queue
func (as *AutoScaler) tryProcessFromQueues(ctx context.Context) bool {
	// Priority order: main queue, default retry, fallback retry, permanent retry
	// This would need integration with the actual queue implementations
	// For now, we'll simulate work processing

	// In a real implementation, you would:
	// 1. Check main payment queue first
	// 2. Check retry queues in priority order
	// 3. Process one item if available
	// 4. Return true if work was processed, false if queues were empty

	return false // Placeholder
}

// updateMetrics updates internal scaling metrics
func (as *AutoScaler) updateMetrics(queueSize int, utilization float64) {
	as.metrics.mutex.Lock()
	defer as.metrics.mutex.Unlock()

	as.metrics.CurrentQueueSize = queueSize
	as.metrics.QueueUtilization = utilization

	// Calculate processing rate (simplified)
	as.metrics.ProcessingRate = float64(as.currentWorkers) * 10.0 // Assume 10 payments/sec per worker

	// Calculate worker efficiency
	if as.currentWorkers > 0 {
		as.metrics.WorkerEfficiency = utilization / float64(as.currentWorkers)
	}
}

// getQueueStats gets statistics from the main queue service
func (as *AutoScaler) getQueueStats() map[string]interface{} {
	// This would call the actual queue service's GetStats method
	// For now, return placeholder data
	return map[string]interface{}{
		"queue_size": 0,
	}
}

// GetMetrics returns current scaling metrics
func (as *AutoScaler) GetMetrics() *ScalingMetrics {
	as.metrics.mutex.RLock()
	defer as.metrics.mutex.RUnlock()

	// Return a copy of metrics
	return &ScalingMetrics{
		TotalScaleUps:       as.metrics.TotalScaleUps,
		TotalScaleDowns:     as.metrics.TotalScaleDowns,
		CurrentQueueSize:    as.metrics.CurrentQueueSize,
		QueueUtilization:    as.metrics.QueueUtilization,
		ProcessingRate:      as.metrics.ProcessingRate,
		LastScalingDecision: as.metrics.LastScalingDecision,
		WorkerEfficiency:    as.metrics.WorkerEfficiency,
	}
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
