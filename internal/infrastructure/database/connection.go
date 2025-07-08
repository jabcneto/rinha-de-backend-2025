package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"rinha-backend-clean/internal/infrastructure/logger"
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

// NewConnection creates a new database connection with retry logic
func NewConnection(config *DatabaseConfig) (*sql.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host, config.Port, config.User, config.Password, config.DBName, config.SSLMode)

	var db *sql.DB
	var err error

	// Retry logic for database connection
	maxRetries := 30
	retryDelay := 2 * time.Second

	logger.Infof("Tentando conectar ao banco de dados em %s:%d...", config.Host, config.Port)

	for i := 0; i < maxRetries; i++ {
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			logger.Warnf("Tentativa %d/%d: erro ao abrir conexão: %v", i+1, maxRetries, err)
			time.Sleep(retryDelay)
			continue
		}

		// Test connection with timeout
		if err := db.Ping(); err != nil {
			logger.Warnf("Tentativa %d/%d: erro ao conectar: %v", i+1, maxRetries, err)
			db.Close()
			time.Sleep(retryDelay)
			continue
		}

		// Connection successful
		logger.Info("Conexão com o banco de dados estabelecida com sucesso!")
		break
	}

	if err != nil {
		return nil, fmt.Errorf("falha ao conectar ao banco após %d tentativas: %w", maxRetries, err)
	}

	// Configure connection pool for resource-constrained environment
	db.SetMaxOpenConns(50)                  // Reduced for memory constraints
	db.SetMaxIdleConns(25)                  // Keep fewer idle connections
	db.SetConnMaxLifetime(15 * time.Minute) // Shorter lifetime
	db.SetConnMaxIdleTime(3 * time.Minute)  // Close idle connections sooner

	return db, nil
}
