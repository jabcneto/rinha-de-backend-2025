package main

import (
	"context"
	"os"
	"os/signal"
	"rinha-backend-clean/internal/domain/repositories"
	"strconv"
	"syscall"
	"time"

	"github.com/valyala/fasthttp"
	"rinha-backend-clean/internal/application/usecases"
	"rinha-backend-clean/internal/infrastructure/database"
	"rinha-backend-clean/internal/infrastructure/external"
	"rinha-backend-clean/internal/infrastructure/logger"
	"rinha-backend-clean/internal/infrastructure/queue"
	httpInterface "rinha-backend-clean/internal/interfaces/http"
	"rinha-backend-clean/internal/interfaces/http/handlers"
)

func main() {
	logger.Info("Iniciando Rinha Backend 2025 - Clean Architecture")

	// Load configuration from environment
	config := loadConfig()

	// Initialize database connection
	db, err := database.NewConnection(config.Database)
	if err != nil {
		logger.Fatalf("Erro ao conectar ao banco de dados: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			logger.Errorf("Erro ao fechar conexão com banco: %v", closeErr)
		}
	}()

	// Initialize repositories
	paymentRepo := repositories.NewPaymentRepository(db)

	// Initialize services
	paymentProcessor := external.NewPaymentProcessorClient()
	queueService := queue.NewPaymentQueue(config.Queue.BufferSize, config.Queue.WorkerCount)
	retryQueueService := queue.NewRetryQueue(config.Queue.BufferSize, config.Queue.WorkerCount)

	// Connect retry queue service to main queue service
	queueService.SetRetryQueueService(retryQueueService)

	// Initialize auto-scaler
	autoScaler := queue.NewAutoScaler(queueService, retryQueueService, paymentProcessor, paymentRepo)

	// Initialize use cases
	processPaymentUC := usecases.NewProcessPaymentUseCase(paymentRepo, queueService)
	getPaymentSummaryUC := usecases.NewGetPaymentSummaryUseCase(paymentRepo)
	purgePaymentsUC := usecases.NewPurgePaymentsUseCase(paymentRepo)

	// Initialize handlers
	paymentHandler := handlers.NewPaymentHandler(processPaymentUC, getPaymentSummaryUC, purgePaymentsUC)
	healthHandler := handlers.NewHealthHandler(queueService, retryQueueService, autoScaler, paymentProcessor)

	// Initialize router
	router := httpInterface.NewRouter(paymentHandler, healthHandler)

	// Start background services
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start payment processor health checks
	paymentProcessor.StartHealthChecks(ctx)

	// Start queue processing
	if err := queueService.StartProcessing(ctx, paymentProcessor, paymentRepo); err != nil {
		logger.Fatalf("Erro ao iniciar processamento da fila: %v", err)
	}

	// Start retry queue processing
	if err := retryQueueService.StartRetryProcessing(paymentProcessor, paymentRepo); err != nil {
		logger.Fatalf("Erro ao iniciar processamento da fila de retry: %v", err)
	}

	// Start auto-scaler
	autoScaler.Start()

	// Setup fasthttp server with optimized settings
	serverAddr := ":" + strconv.Itoa(config.Server.Port)
	logger.Infof("Servidor iniciado na porta %d", config.Server.Port)

	// Configure fasthttp server for high performance
	server := &fasthttp.Server{
		Handler:                       router.Handler,
		Name:                          "rinha-backend-2025",
		ReadTimeout:                   10 * time.Second, // Timeout para ler request
		WriteTimeout:                  10 * time.Second, // Timeout para escrever response
		IdleTimeout:                   60 * time.Second, // Timeout para conexões idle
		MaxConnsPerIP:                 1000,             // Máximo de conexões por IP
		MaxRequestsPerConn:            10000,            // Máximo de requests por conexão
		MaxRequestBodySize:            1024 * 1024,      // 1MB max body size
		ReduceMemoryUsage:             true,             // Reduzir uso de memória
		DisableKeepalive:              false,            // Manter keep-alive habilitado
		TCPKeepalive:                  true,             // TCP keep-alive
		Concurrency:                   50000,            // Máximo de conexões concorrentes
		DisableHeaderNamesNormalizing: true,             // Performance boost
	}

	go func() {
		if err := server.ListenAndServe(serverAddr); err != nil {
			logger.Fatalf("Erro ao iniciar servidor: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Iniciando shutdown graceful...")

	// Cancel background services
	cancel()

	logger.Info("Servidor encerrado com sucesso")
}

// Config holds application configuration
type Config struct {
	Server   ServerConfig
	Database *database.DatabaseConfig
	Queue    QueueConfig
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port int
}

// QueueConfig holds queue configuration
type QueueConfig struct {
	BufferSize  int
	WorkerCount int
}

// loadConfig loads configuration from environment variables
func loadConfig() *Config {
	// Parse server port
	port := 9999
	if portStr := os.Getenv("PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}
	if portStr := os.Getenv("SERVER_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	// Parse queue configuration
	bufferSize := 10000 // Aumentado de 1000 para 10000
	workerCount := 20   // Aumentado de 5 para 20
	if bufferStr := os.Getenv("QUEUE_BUFFER_SIZE"); bufferStr != "" {
		if b, err := strconv.Atoi(bufferStr); err == nil {
			bufferSize = b
		}
	}

	if w, err := strconv.Atoi(os.Getenv("WORKER_COUNT")); err == nil && w > 0 {
		if w > 50 { // Limite máximo
			w = 50
		}
		workerCount = w
	}

	// Parse database configuration from environment variables
	dbHost := getEnvOrDefault("DB_HOST", "localhost")
	dbPort := 5432
	if portStr := os.Getenv("DB_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			dbPort = p
		}
	}
	dbUser := getEnvOrDefault("DB_USER", "postgres")
	dbPassword := getEnvOrDefault("DB_PASSWORD", "postgres")
	dbName := getEnvOrDefault("DB_NAME", "payments")
	sslMode := getEnvOrDefault("SSL_MODE", "disable")

	return &Config{
		Server: ServerConfig{
			Port: port,
		},
		Database: &database.DatabaseConfig{
			Host:     dbHost,
			Port:     dbPort,
			User:     dbUser,
			Password: dbPassword,
			DBName:   dbName,
			SSLMode:  sslMode,
		},
		Queue: QueueConfig{
			BufferSize:  bufferSize,
			WorkerCount: workerCount,
		},
	}
}

// getEnvOrDefault returns environment variable value or default if not set
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
