package logger

import (
	"context"
	"github.com/sirupsen/logrus"
	"os"
	"strings"
)

var log *logrus.Logger

func init() {
	log = logrus.New()
	log.SetOutput(os.Stdout)

	// Configurar formatter baseado na variável de ambiente
	if strings.ToLower(os.Getenv("LOG_FORMAT")) == "text" {
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02 15:04:05",
			ForceColors:     true,
			DisableQuote:    true,
		})
	} else {
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "timestamp",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "message",
				logrus.FieldKeyFunc:  "caller",
			},
		})
	}

	// Configurar nível de log baseado na variável de ambiente
	level := strings.ToLower(os.Getenv("LOG_LEVEL"))
	switch level {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "warn", "warning":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	case "fatal":
		log.SetLevel(logrus.FatalLevel)
	case "panic":
		log.SetLevel(logrus.PanicLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}
}

// GetLogger retorna a instância do logger
func GetLogger() *logrus.Logger {
	return log
}

// WithFields cria um entry com campos estruturados
func WithFields(fields logrus.Fields) *logrus.Entry {
	return log.WithFields(fields)
}

// WithField cria um entry com um campo estruturado
func WithField(key string, value interface{}) *logrus.Entry {
	return log.WithField(key, value)
}

// WithContext cria um entry com contexto
func WithContext(ctx context.Context) *logrus.Entry {
	return log.WithContext(ctx)
}

// WithError cria um entry com erro
func WithError(err error) *logrus.Entry {
	return log.WithError(err)
}

// Info logs an info message
func Info(args ...interface{}) {
	log.Info(args...)
}

// Infof logs an info message with formatting
func Infof(format string, args ...interface{}) {
	log.Infof(format, args...)
}

// Error logs an error message
func Error(args ...interface{}) {
	log.Error(args...)
}

// Errorf logs an error message with formatting
func Errorf(format string, args ...interface{}) {
	log.Errorf(format, args...)
}

// Warn logs a warning message
func Warn(args ...interface{}) {
	log.Warn(args...)
}

// Warnf logs a warning message with formatting
func Warnf(format string, args ...interface{}) {
	log.Warnf(format, args...)
}

// Debug logs a debug message
func Debug(args ...interface{}) {
	log.Debug(args...)
}

// Debugf logs a debug message with formatting
func Debugf(format string, args ...interface{}) {
	log.Debugf(format, args...)
}

// Fatal logs a fatal message and exits
func Fatal(args ...interface{}) {
	log.Fatal(args...)
}

// Fatalf logs a fatal message with formatting and exits
func Fatalf(format string, args ...interface{}) {
	log.Fatalf(format, args...)
}

// Panic logs a panic message and panics
func Panic(args ...interface{}) {
	log.Panic(args...)
}

// Panicf logs a panic message with formatting and panics
func Panicf(format string, args ...interface{}) {
	log.Panicf(format, args...)
}

// LogPaymentProcessing logs payment processing with structured fields
func LogPaymentProcessing(paymentID, processorType string, amount float64) {
	WithFields(logrus.Fields{
		"payment_id":     paymentID,
		"processor_type": processorType,
		"amount":         amount,
		"operation":      "payment_processing",
	}).Info("Processando pagamento")
}

// LogPaymentSuccess logs successful payment processing
func LogPaymentSuccess(paymentID, processorType string, amount float64, duration string) {
	WithFields(logrus.Fields{
		"payment_id":     paymentID,
		"processor_type": processorType,
		"amount":         amount,
		"duration":       duration,
		"operation":      "payment_success",
		"status":         "success",
	}).Info("Pagamento processado com sucesso")
}

// LogPaymentError logs payment processing errors
func LogPaymentError(paymentID, processorType string, amount float64, err error) {
	WithFields(logrus.Fields{
		"payment_id":     paymentID,
		"processor_type": processorType,
		"amount":         amount,
		"operation":      "payment_error",
		"status":         "error",
		"error":          err.Error(),
	}).Error("Erro ao processar pagamento")
}

// LogRetryAttempt logs retry attempts
func LogRetryAttempt(paymentID string, attempt int, maxAttempts int, err error) {
	WithFields(logrus.Fields{
		"payment_id":   paymentID,
		"attempt":      attempt,
		"max_attempts": maxAttempts,
		"operation":    "retry_attempt",
		"error":        err.Error(),
	}).Warn("Tentativa de retry do pagamento")
}

// LogQueueStats logs queue statistics
func LogQueueStats(queueSize int, activeWorkers int, pendingItems int) {
	WithFields(logrus.Fields{
		"queue_size":     queueSize,
		"active_workers": activeWorkers,
		"pending_items":  pendingItems,
		"operation":      "queue_stats",
	}).Debug("Estatísticas da fila")
}

// LogCircuitBreakerState logs circuit breaker state changes
func LogCircuitBreakerState(processorType, state string) {
	WithFields(logrus.Fields{
		"processor_type": processorType,
		"state":          state,
		"operation":      "circuit_breaker",
	}).Warn("Mudança de estado do circuit breaker")
}

// LogHTTPRequest logs HTTP requests
func LogHTTPRequest(method, path string, statusCode int, duration string, clientIP string) {
	WithFields(logrus.Fields{
		"method":      method,
		"path":        path,
		"status_code": statusCode,
		"duration":    duration,
		"client_ip":   clientIP,
		"operation":   "http_request",
	}).Info("Requisição HTTP processada")
}
