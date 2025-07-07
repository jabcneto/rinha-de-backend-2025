package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// NewConnection creates a new database connection
func NewConnection(config *DatabaseConfig) (*sql.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.User, config.Password, config.DBName, config.SSLMode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir conexão com o banco de dados: %w", err)
	}

	// Configure connection pool for high performance
	db.SetMaxOpenConns(50)                  // Increased from 25
	db.SetMaxIdleConns(25)                  // Keep more idle connections
	db.SetConnMaxLifetime(10 * time.Minute) // Increased lifetime

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("erro ao conectar ao banco de dados: %w", err)
	}

	log.Println("Conexão com o banco de dados estabelecida com sucesso!")

	// Create tables
	if err := createTables(db); err != nil {
		return nil, err
	}

	return db, nil
}

// createTables creates the necessary database tables
func createTables(db *sql.DB) error {
	queries := []string{
		// Payment summary table (optimized)
		`CREATE TABLE IF NOT EXISTS payment_summary (
			id SERIAL PRIMARY KEY,
			processor_type VARCHAR(10) UNIQUE NOT NULL,
			total_requests BIGINT NOT NULL DEFAULT 0,
			total_amount DECIMAL(18, 2) NOT NULL DEFAULT 0.00,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,

		// Individual payments table (for detailed tracking)
		`CREATE TABLE IF NOT EXISTS payments (
			id UUID PRIMARY KEY,
			correlation_id UUID UNIQUE NOT NULL,
			amount DECIMAL(18, 2) NOT NULL,
			status VARCHAR(20) NOT NULL,
			processor_type VARCHAR(10),
			requested_at TIMESTAMP NOT NULL,
			processed_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,

		// Indexes for performance
		`CREATE INDEX IF NOT EXISTS idx_payments_correlation_id ON payments(correlation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status)`,
		`CREATE INDEX IF NOT EXISTS idx_payments_processor_type ON payments(processor_type)`,
		`CREATE INDEX IF NOT EXISTS idx_payments_created_at ON payments(created_at)`,

		// Insert initial summary data
		`INSERT INTO payment_summary (processor_type, total_requests, total_amount) VALUES
			('default', 0, 0.00) ON CONFLICT (processor_type) DO NOTHING`,
		`INSERT INTO payment_summary (processor_type, total_requests, total_amount) VALUES
			('fallback', 0, 0.00) ON CONFLICT (processor_type) DO NOTHING`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("erro ao executar query: %s - %w", query, err)
		}
	}

	log.Println("Tabelas verificadas/criadas com sucesso.")
	return nil
}
