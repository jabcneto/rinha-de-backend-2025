package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"rinha-backend-clean/internal/domain/entities"
	"rinha-backend-clean/internal/domain/repositories"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// PaymentRepositoryImpl implements the PaymentRepository interface
type PaymentRepositoryImpl struct {
	db    *sql.DB
	cache *SummaryCache
	mutex sync.RWMutex

	// Batch update system
	batchBuffer   map[entities.ProcessorType]*BatchUpdate
	batchMutex    sync.RWMutex
	batchTicker   *time.Ticker
	batchSize     int
	batchInterval time.Duration
}

// BatchUpdate holds pending updates for a processor type
type BatchUpdate struct {
	TotalRequests int64
	TotalAmount   float64
	LastUpdate    time.Time
}

// SummaryCache provides in-memory caching for payment summary
type SummaryCache struct {
	data       *entities.PaymentSummary
	lastUpdate time.Time
	mutex      sync.RWMutex
	ttl        time.Duration
}

// NewPaymentRepository creates a new payment repository
func NewPaymentRepository(db *sql.DB) repositories.PaymentRepository {
	cache := &SummaryCache{
		data: entities.NewPaymentSummary(),
		ttl:  5 * time.Second, // Cache TTL of 5 seconds
	}

	repo := &PaymentRepositoryImpl{
		db:    db,
		cache: cache,
	}

	// Start background cache refresh
	go repo.startCacheRefresh()

	// Initialize batch update system
	repo.batchBuffer = make(map[entities.ProcessorType]*BatchUpdate)
	repo.batchSize = 1000
	repo.batchInterval = 5 * time.Second
	repo.batchTicker = time.NewTicker(repo.batchInterval)

	// Start background batch updater
	go repo.startBatchUpdate()

	return repo
}

// Save saves a payment to the repository (uses UPSERT)
func (r *PaymentRepositoryImpl) Save(ctx context.Context, payment *entities.Payment) error {
	query := `
		INSERT INTO payments (id, correlation_id, amount, status, processor_type, requested_at, processed_at, created_at, updated_at, retry_count, next_retry_at, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			processor_type = EXCLUDED.processor_type,
			processed_at = EXCLUDED.processed_at,
			updated_at = EXCLUDED.updated_at,
			retry_count = EXCLUDED.retry_count,
			next_retry_at = EXCLUDED.next_retry_at,
			last_error = EXCLUDED.last_error
	`

	_, err := r.db.ExecContext(ctx, query,
		payment.ID,
		payment.CorrelationID,
		payment.Amount,
		string(payment.Status),
		string(payment.ProcessorType),
		payment.RequestedAt,
		payment.ProcessedAt,
		payment.CreatedAt,
		payment.UpdatedAt,
		payment.RetryCount,
		payment.NextRetryAt,
		payment.LastError,
	)

	return err
}

// FindByCorrelationID finds a payment by its correlation ID
func (r *PaymentRepositoryImpl) FindByCorrelationID(ctx context.Context, correlationID uuid.UUID) (*entities.Payment, error) {
	query := `
		SELECT id, correlation_id, amount, status, processor_type, requested_at, processed_at, created_at, updated_at, retry_count, next_retry_at, last_error
		FROM payments
		WHERE correlation_id = $1
	`

	row := r.db.QueryRowContext(ctx, query, correlationID)

	payment := &entities.Payment{}
	var status, processorType string
	var processedAt sql.NullTime
	var nextRetryAt sql.NullTime
	var lastError sql.NullString

	err := row.Scan(
		&payment.ID,
		&payment.CorrelationID,
		&payment.Amount,
		&status,
		&processorType,
		&payment.RequestedAt,
		&processedAt,
		&payment.CreatedAt,
		&payment.UpdatedAt,
		&payment.RetryCount,
		&nextRetryAt,
		&lastError,
	)

	if err != nil {
		return nil, err
	}

	payment.Status = entities.PaymentStatus(status)
	payment.ProcessorType = entities.ProcessorType(processorType)
	if processedAt.Valid {
		payment.ProcessedAt = &processedAt.Time
	}
	if nextRetryAt.Valid {
		payment.NextRetryAt = &nextRetryAt.Time
	}
	if lastError.Valid {
		payment.LastError = lastError.String
	}

	return payment, nil
}

// Update updates an existing payment (now uses UPSERT)
func (r *PaymentRepositoryImpl) Update(ctx context.Context, payment *entities.Payment) error {
	query := `
		INSERT INTO payments (id, correlation_id, amount, status, processor_type, requested_at, processed_at, created_at, updated_at, retry_count, next_retry_at, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			processor_type = EXCLUDED.processor_type,
			processed_at = EXCLUDED.processed_at,
			updated_at = EXCLUDED.updated_at,
			retry_count = EXCLUDED.retry_count,
			next_retry_at = EXCLUDED.next_retry_at,
			last_error = EXCLUDED.last_error
	`

	_, err := r.db.ExecContext(ctx, query,
		payment.ID,
		payment.CorrelationID,
		payment.Amount,
		string(payment.Status),
		string(payment.ProcessorType),
		payment.RequestedAt,
		payment.ProcessedAt,
		payment.CreatedAt,
		payment.UpdatedAt,
		payment.RetryCount,
		payment.NextRetryAt,
		payment.LastError,
	)

	return err
}

// GetSummary returns payment summary with optional time filters (may block)
func (r *PaymentRepositoryImpl) GetSummary(ctx context.Context, filter *entities.PaymentSummaryFilter) (*entities.PaymentSummary, error) {
	summary := entities.NewPaymentSummary()

	// If no filters, return aggregated data from payment_summary table
	if filter == nil || (filter.From == nil && filter.To == nil) {
		// Query default processor summary
		row := r.db.QueryRowContext(ctx, "SELECT total_requests, total_amount FROM payment_summary WHERE processor_type = $1", "default")
		err := row.Scan(&summary.Default.TotalRequests, &summary.Default.TotalAmount)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("erro ao obter resumo default: %w", err)
		}

		// Query fallback processor summary
		row = r.db.QueryRowContext(ctx, "SELECT total_requests, total_amount FROM payment_summary WHERE processor_type = $1", "fallback")
		err = row.Scan(&summary.Fallback.TotalRequests, &summary.Fallback.TotalAmount)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("erro ao obter resumo fallback: %w", err)
		}

		return summary, nil
	}

	// If there are filters, query the payments table directly
	whereClause := "WHERE 1=1"
	args := []interface{}{}
	argIndex := 1

	if filter.From != nil {
		whereClause += fmt.Sprintf(" AND created_at >= $%d", argIndex)
		args = append(args, filter.From)
		argIndex++
	}

	if filter.To != nil {
		whereClause += fmt.Sprintf(" AND created_at <= $%d", argIndex)
		args = append(args, filter.To)
		argIndex++
	}

	// Query default processor summary with filters
	query := fmt.Sprintf(`
		SELECT 
			COALESCE(COUNT(*), 0) as total_requests,
			COALESCE(SUM(amount), 0) as total_amount
		FROM payments 
		%s AND processor_type = $%d
	`, whereClause, argIndex)

	argsDefault := append(args, "default")
	row := r.db.QueryRowContext(ctx, query, argsDefault...)
	err := row.Scan(&summary.Default.TotalRequests, &summary.Default.TotalAmount)
	if err != nil {
		return nil, fmt.Errorf("erro ao obter resumo default com filtros: %w", err)
	}

	// Query fallback processor summary with filters
	argsFallback := append(args, "fallback")
	row = r.db.QueryRowContext(ctx, query, argsFallback...)
	err = row.Scan(&summary.Fallback.TotalRequests, &summary.Fallback.TotalAmount)
	if err != nil {
		return nil, fmt.Errorf("erro ao obter resumo fallback com filtros: %w", err)
	}

	return summary, nil
}

// UpdateSummary updates the payment summary using batch processing (optimized for high throughput)
func (r *PaymentRepositoryImpl) UpdateSummary(ctx context.Context, processorType entities.ProcessorType, amount float64) error {
	// Add to batch buffer instead of updating directly
	r.batchMutex.Lock()
	defer r.batchMutex.Unlock()

	if r.batchBuffer[processorType] == nil {
		r.batchBuffer[processorType] = &BatchUpdate{
			TotalRequests: 0,
			TotalAmount:   0,
			LastUpdate:    time.Now(),
		}
	}

	// Accumulate in batch buffer
	r.batchBuffer[processorType].TotalRequests++
	r.batchBuffer[processorType].TotalAmount += amount
	r.batchBuffer[processorType].LastUpdate = time.Now()

	// Update cache immediately for fast reads
	r.cache.mutex.Lock()
	r.cache.data.AddPayment(processorType, amount)
	r.cache.lastUpdate = time.Now()
	r.cache.mutex.Unlock()

	// Trigger batch update if buffer is full
	if r.batchBuffer[processorType].TotalRequests >= int64(r.batchSize) {
		go r.flushBatch(processorType)
	}

	return nil
}

// flushBatch flushes pending updates for a specific processor type
func (r *PaymentRepositoryImpl) flushBatch(processorType entities.ProcessorType) {
	r.batchMutex.Lock()

	batch := r.batchBuffer[processorType]
	if batch == nil || batch.TotalRequests == 0 {
		r.batchMutex.Unlock()
		return
	}

	// Take ownership of the batch and clear it
	requests := batch.TotalRequests
	amount := batch.TotalAmount
	delete(r.batchBuffer, processorType)

	r.batchMutex.Unlock()

	// Perform the database update outside of the lock
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	query := `
		UPDATE payment_summary 
		SET total_requests = total_requests + $1, total_amount = total_amount + $2 
		WHERE processor_type = $3
	`

	_, err := r.db.ExecContext(ctx, query, requests, amount, string(processorType))
	if err != nil {
		log.Printf("Erro ao atualizar resumo do pagamento em batch para %s: %v", processorType, err)

		// Re-add to batch buffer if update failed
		r.batchMutex.Lock()
		if r.batchBuffer[processorType] == nil {
			r.batchBuffer[processorType] = &BatchUpdate{
				TotalRequests: 0,
				TotalAmount:   0,
				LastUpdate:    time.Now(),
			}
		}
		r.batchBuffer[processorType].TotalRequests += requests
		r.batchBuffer[processorType].TotalAmount += amount
		r.batchMutex.Unlock()
	}
}

// GetSummaryFromCache returns cached summary data (non-blocking)
func (r *PaymentRepositoryImpl) GetSummaryFromCache(ctx context.Context) (*entities.PaymentSummary, error) {
	r.cache.mutex.RLock()
	defer r.cache.mutex.RUnlock()

	// Check if cache is still valid
	if time.Since(r.cache.lastUpdate) > r.cache.ttl {
		return nil, fmt.Errorf("cache expired")
	}

	// Return a copy of cached data
	summary := &entities.PaymentSummary{
		Default: entities.ProcessorSummary{
			TotalRequests: r.cache.data.Default.TotalRequests,
			TotalAmount:   r.cache.data.Default.TotalAmount,
		},
		Fallback: entities.ProcessorSummary{
			TotalRequests: r.cache.data.Fallback.TotalRequests,
			TotalAmount:   r.cache.data.Fallback.TotalAmount,
		},
	}

	return summary, nil
}

// startCacheRefresh starts a background goroutine to refresh cache periodically
func (r *PaymentRepositoryImpl) startCacheRefresh() {
	ticker := time.NewTicker(3 * time.Second) // Refresh every 3 seconds
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

		summary, err := r.GetSummary(ctx, nil)
		if err != nil {
			log.Printf("Erro ao atualizar cache: %v", err)
			cancel()
			continue
		}

		r.cache.mutex.Lock()
		r.cache.data = summary
		r.cache.lastUpdate = time.Now()
		r.cache.mutex.Unlock()

		cancel()
	}
}

// startBatchUpdate starts a background goroutine to process batch updates
func (r *PaymentRepositoryImpl) startBatchUpdate() {
	for range r.batchTicker.C {
		r.batchMutex.Lock()

		// Process each processor type in the batch buffer
		for processorType, batch := range r.batchBuffer {
			if time.Since(batch.LastUpdate) < r.batchInterval {
				continue // Skip if not enough time has passed
			}

			query := `
				UPDATE payment_summary 
				SET total_requests = total_requests + $1, total_amount = total_amount + $2 
				WHERE processor_type = $3
			`

			_, err := r.db.ExecContext(context.Background(), query, batch.TotalRequests, batch.TotalAmount, string(processorType))
			if err != nil {
				log.Printf("Erro ao atualizar resumo do pagamento em batch para %s: %v", processorType, err)
				continue
			}

			// Reset batch buffer for this processor type
			delete(r.batchBuffer, processorType)
		}

		r.batchMutex.Unlock()
	}
}

// GetPaymentsForRetry retrieves payments that are eligible for retry
func (r *PaymentRepositoryImpl) GetPaymentsForRetry(ctx context.Context, limit int) ([]*entities.Payment, error) {
	query := `
		SELECT id, correlation_id, amount, status, processor_type, requested_at, processed_at, created_at, updated_at, retry_count, next_retry_at, last_error
		FROM payments
		WHERE status = 'failed' 
		AND retry_count < 3 
		AND (next_retry_at IS NULL OR next_retry_at <= NOW())
		ORDER BY created_at ASC
		LIMIT $1
	`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar pagamentos para retry: %w", err)
	}
	defer rows.Close()

	var payments []*entities.Payment
	for rows.Next() {
		payment := &entities.Payment{}
		var status, processorType string
		var processedAt sql.NullTime
		var nextRetryAt sql.NullTime
		var lastError sql.NullString

		err := rows.Scan(
			&payment.ID,
			&payment.CorrelationID,
			&payment.Amount,
			&status,
			&processorType,
			&payment.RequestedAt,
			&processedAt,
			&payment.CreatedAt,
			&payment.UpdatedAt,
			&payment.RetryCount,
			&nextRetryAt,
			&lastError,
		)

		if err != nil {
			return nil, fmt.Errorf("erro ao fazer scan do pagamento: %w", err)
		}

		payment.Status = entities.PaymentStatus(status)
		payment.ProcessorType = entities.ProcessorType(processorType)
		if processedAt.Valid {
			payment.ProcessedAt = &processedAt.Time
		}
		if nextRetryAt.Valid {
			payment.NextRetryAt = &nextRetryAt.Time
		}
		if lastError.Valid {
			payment.LastError = lastError.String
		}

		payments = append(payments, payment)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("erro ao iterar pagamentos: %w", err)
	}

	return payments, nil
}

// UpdateRetryInfo updates retry-related information for a payment
func (r *PaymentRepositoryImpl) UpdateRetryInfo(ctx context.Context, paymentID uuid.UUID, retryCount int, nextRetryAt *time.Time, lastError string) error {
	query := `
		UPDATE payments 
		SET retry_count = $1, next_retry_at = $2, last_error = $3, updated_at = NOW()
		WHERE id = $4
	`

	_, err := r.db.ExecContext(ctx, query, retryCount, nextRetryAt, lastError, paymentID)
	if err != nil {
		return fmt.Errorf("erro ao atualizar informações de retry: %w", err)
	}

	return nil
}

// MarkPaymentAsFailed marks a payment as permanently failed
func (r *PaymentRepositoryImpl) MarkPaymentAsFailed(ctx context.Context, paymentID uuid.UUID, lastError string) error {
	query := `
		UPDATE payments 
		SET status = 'failed', last_error = $1, updated_at = NOW()
		WHERE id = $2
	`

	_, err := r.db.ExecContext(ctx, query, lastError, paymentID)
	if err != nil {
		return fmt.Errorf("erro ao marcar pagamento como falhou: %w", err)
	}

	return nil
}

// PurgeSummary resets all summary data
func (r *PaymentRepositoryImpl) PurgeSummary(ctx context.Context) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// Clear batch buffer first
	r.batchMutex.Lock()
	r.batchBuffer = make(map[entities.ProcessorType]*BatchUpdate)
	r.batchMutex.Unlock()

	_, err := r.db.ExecContext(ctx, "UPDATE payment_summary SET total_requests = 0, total_amount = 0.00")
	if err != nil {
		return fmt.Errorf("erro ao limpar resumo de pagamentos: %w", err)
	}

	// Clear cache
	r.cache.mutex.Lock()
	r.cache.data.Reset()
	r.cache.lastUpdate = time.Now()
	r.cache.mutex.Unlock()

	return nil
}
