package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"rinha-backend-clean/internal/application/usecases"
	"rinha-backend-clean/internal/infrastructure/database"
	"rinha-backend-clean/internal/infrastructure/external"
	"rinha-backend-clean/internal/infrastructure/queue"
	httpInterface "rinha-backend-clean/internal/interfaces/http"
	"rinha-backend-clean/internal/interfaces/http/handlers"
)

func main() {
	log.Println("Iniciando Rinha Backend 2025 - Clean Architecture")

	// Load configuration from environment
	config := loadConfig()

	// Initialize database connection
	db, err := database.NewConnection(config.Database)
	if err != nil {
		log.Fatalf("Erro ao conectar ao banco de dados: %v", err)
	}
	defer db.Close()

	// Initialize repositories
	paymentRepo := database.NewPaymentRepository(db)

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
	healthHandler := handlers.NewHealthHandler(queueService, retryQueueService, autoScaler)

	// Initialize router
	router := httpInterface.NewRouter(paymentHandler, healthHandler)
	httpRouter := router.SetupRoutes()

	// Start background services
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start payment processor health checks
	paymentProcessor.StartHealthChecks(ctx)

	// Start queue processing
	if err := queueService.StartProcessing(ctx, paymentProcessor, paymentRepo); err != nil {
		log.Fatalf("Erro ao iniciar processamento da fila: %v", err)
	}

	// Start retry queue processing
	if err := retryQueueService.StartRetryProcessing(paymentProcessor, paymentRepo); err != nil {
		log.Fatalf("Erro ao iniciar processamento da fila de retry: %v", err)
	}

	// Start auto-scaler
	autoScaler.Start()

	// Setup HTTP server
	server := &http.Server{
		Addr:         ":" + strconv.Itoa(config.Server.Port),
		Handler:      httpRouter,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Servidor iniciado na porta %d", config.Server.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Erro ao iniciar servidor: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Iniciando shutdown graceful...")

	// Create a deadline for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Shutdown HTTP server
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Erro durante shutdown do servidor: %v", err)
	}

	// Stop queue processing
	if err := queueService.Stop(shutdownCtx); err != nil {
		log.Printf("Erro ao parar processamento da fila: %v", err)
	}

	// Stop retry queue processing
	if err := retryQueueService.Stop(); err != nil {
		log.Printf("Erro ao parar processamento da fila de retry: %v", err)
	}

	// Cancel background services
	cancel()

	log.Println("Servidor encerrado com sucesso")
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
