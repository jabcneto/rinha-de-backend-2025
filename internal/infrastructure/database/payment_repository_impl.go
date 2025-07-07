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

	return repo
}

// Save saves a payment to the repository
func (r *PaymentRepositoryImpl) Save(ctx context.Context, payment *entities.Payment) error {
	query := `
		INSERT INTO payments (id, correlation_id, amount, status, processor_type, requested_at, processed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
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
	)

	return err
}

// FindByCorrelationID finds a payment by its correlation ID
func (r *PaymentRepositoryImpl) FindByCorrelationID(ctx context.Context, correlationID uuid.UUID) (*entities.Payment, error) {
	query := `
		SELECT id, correlation_id, amount, status, processor_type, requested_at, processed_at, created_at, updated_at
		FROM payments
		WHERE correlation_id = $1
	`

	row := r.db.QueryRowContext(ctx, query, correlationID)

	payment := &entities.Payment{}
	var status, processorType string
	var processedAt sql.NullTime

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
	)

	if err != nil {
		return nil, err
	}

	payment.Status = entities.PaymentStatus(status)
	payment.ProcessorType = entities.ProcessorType(processorType)
	if processedAt.Valid {
		payment.ProcessedAt = &processedAt.Time
	}

	return payment, nil
}

// Update updates an existing payment
func (r *PaymentRepositoryImpl) Update(ctx context.Context, payment *entities.Payment) error {
	query := `
		UPDATE payments 
		SET status = $1, processor_type = $2, processed_at = $3, updated_at = $4
		WHERE id = $5
	`

	_, err := r.db.ExecContext(ctx, query,
		string(payment.Status),
		string(payment.ProcessorType),
		payment.ProcessedAt,
		payment.UpdatedAt,
		payment.ID,
	)

	return err
}

// GetSummary returns payment summary with optional time filters (may block)
func (r *PaymentRepositoryImpl) GetSummary(ctx context.Context, filter *entities.PaymentSummaryFilter) (*entities.PaymentSummary, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	summary := entities.NewPaymentSummary()

	// Monta a cláusula WHERE dinamicamente
	where := ""
	args := []interface{}{}
	argIdx := 1
	if filter != nil {
		if filter.From != nil {
			where += fmt.Sprintf(" AND requested_at >= $%d", argIdx)
			args = append(args, *filter.From)
			argIdx++
		}
		if filter.To != nil {
			where += fmt.Sprintf(" AND requested_at <= $%d", argIdx)
			args = append(args, *filter.To)
			argIdx++
		}
	}

	// Query default processor summary
	queryDefault := "SELECT COUNT(*), COALESCE(SUM(amount),0) FROM payments WHERE processor_type = $1" + where
	argsDefault := append([]interface{}{string(entities.ProcessorTypeDefault)}, args...)
	row := r.db.QueryRowContext(ctx, queryDefault, argsDefault...)
	if err := row.Scan(&summary.Default.TotalRequests, &summary.Default.TotalAmount); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("erro ao obter resumo default: %w", err)
	}

	// Query fallback processor summary
	queryFallback := "SELECT COUNT(*), COALESCE(SUM(amount),0) FROM payments WHERE processor_type = $1" + where
	argsFallback := append([]interface{}{string(entities.ProcessorTypeFallback)}, args...)
	row = r.db.QueryRowContext(ctx, queryFallback, argsFallback...)
	if err := row.Scan(&summary.Fallback.TotalRequests, &summary.Fallback.TotalAmount); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("erro ao obter resumo fallback: %w", err)
	}

	return summary, nil
}

// UpdateSummary updates the payment summary (optimized for high throughput)
func (r *PaymentRepositoryImpl) UpdateSummary(ctx context.Context, processorType entities.ProcessorType, amount float64) error {
	// Use a separate goroutine to avoid blocking the main thread
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		query := `
			UPDATE payment_summary 
			SET total_requests = total_requests + 1, total_amount = total_amount + $1 
			WHERE processor_type = $2
		`

		_, err := r.db.ExecContext(ctx, query, amount, string(processorType))
		if err != nil {
			log.Printf("Erro ao atualizar resumo do pagamento para %s: %v", processorType, err)
		}

		// Update cache immediately
		r.cache.mutex.Lock()
		r.cache.data.AddPayment(processorType, amount)
		r.cache.lastUpdate = time.Now()
		r.cache.mutex.Unlock()
	}()

	return nil
}

// PurgeSummary resets all summary data
func (r *PaymentRepositoryImpl) PurgeSummary(ctx context.Context) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

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
