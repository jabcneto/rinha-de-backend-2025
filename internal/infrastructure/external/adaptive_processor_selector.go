package external

import (
	"context"
	"fmt"
	"sync"
	"time"

	"rinha-backend-clean/internal/domain/entities"
	"rinha-backend-clean/internal/domain/services"
	"rinha-backend-clean/internal/infrastructure/logger"
)

// ProcessorScore represents the health score of a processor
type ProcessorScore struct {
	ProcessorType    entities.ProcessorType
	HealthScore      float64 // 0.0 (worst) to 1.0 (best)
	ResponseTime     int64
	IsHealthy        bool
	IsFailing        bool
	LastUpdated      time.Time
	ConsecutiveFails int
	Weight           float64 // Dynamic weight based on recent performance
}

// AdaptiveProcessorSelector intelligently selects the best processor based on health metrics
type AdaptiveProcessorSelector struct {
	processors map[entities.ProcessorType]*ProcessorScore
	client     *PaymentProcessorClient
	mutex      sync.RWMutex

	// Adaptive configuration
	healthCheckInterval    time.Duration
	adaptiveThreshold      float64 // Switch threshold
	failureWeight          float64 // Weight for failure penalty
	responseTimeWeight     float64 // Weight for response time
	consecutiveFailPenalty float64 // Penalty multiplier for consecutive fails
}

// NewAdaptiveProcessorSelector creates a new adaptive processor selector
func NewAdaptiveProcessorSelector(client *PaymentProcessorClient) *AdaptiveProcessorSelector {
	return &AdaptiveProcessorSelector{
		processors: map[entities.ProcessorType]*ProcessorScore{
			entities.ProcessorTypeDefault: {
				ProcessorType: entities.ProcessorTypeDefault,
				HealthScore:   1.0,
				Weight:        1.0,
				IsHealthy:     true,
				LastUpdated:   time.Now(),
			},
			entities.ProcessorTypeFallback: {
				ProcessorType: entities.ProcessorTypeFallback,
				HealthScore:   1.0,
				Weight:        0.8, // Slightly lower initial weight for fallback
				IsHealthy:     true,
				LastUpdated:   time.Now(),
			},
		},
		client:                 client,
		healthCheckInterval:    3 * time.Second, // Frequent updates for adaptive decisions
		adaptiveThreshold:      0.2,             // Switch when score difference > 20%
		failureWeight:          0.4,             // 40% weight for failure status
		responseTimeWeight:     0.3,             // 30% weight for response time
		consecutiveFailPenalty: 0.1,             // 10% penalty per consecutive fail
	}
}

// SelectOptimalProcessor returns the best processor based on current health metrics
func (aps *AdaptiveProcessorSelector) SelectOptimalProcessor(_ context.Context) entities.ProcessorType {
	aps.mutex.RLock()
	defer aps.mutex.RUnlock()

	defaultScore := aps.processors[entities.ProcessorTypeDefault]
	fallbackScore := aps.processors[entities.ProcessorTypeFallback]

	// If both are unhealthy, prefer default (original behavior)
	if !defaultScore.IsHealthy && !fallbackScore.IsHealthy {
		logger.Warnf("Ambos processadores não saudáveis - usando default por padrão")
		return entities.ProcessorTypeDefault
	}

	// If only one is healthy, use it
	if defaultScore.IsHealthy && !fallbackScore.IsHealthy {
		logger.Debugf("Selecionando default (fallback não saudável)")
		return entities.ProcessorTypeDefault
	}
	if !defaultScore.IsHealthy && fallbackScore.IsHealthy {
		logger.Infof("Selecionando fallback (default não saudável)")
		return entities.ProcessorTypeFallback
	}

	// Both are healthy - use adaptive selection based on weighted scores
	defaultWeightedScore := aps.calculateWeightedScore(defaultScore)
	fallbackWeightedScore := aps.calculateWeightedScore(fallbackScore)

	scoreDifference := defaultWeightedScore - fallbackWeightedScore

	// If fallback is significantly better, use it
	if scoreDifference < -aps.adaptiveThreshold {
		logger.Infof("Seleção adaptativa: fallback (score: %.3f vs %.3f)",
			fallbackWeightedScore, defaultWeightedScore)
		return entities.ProcessorTypeFallback
	}

	// Default wins or scores are close - prefer default
	logger.Debugf("Seleção adaptativa: default (score: %.3f vs %.3f)",
		defaultWeightedScore, fallbackWeightedScore)
	return entities.ProcessorTypeDefault
}

// calculateWeightedScore calculates a weighted health score
func (aps *AdaptiveProcessorSelector) calculateWeightedScore(score *ProcessorScore) float64 {
	if score == nil {
		return 0.0
	}

	weightedScore := score.Weight

	// Factor in failure status
	if score.IsFailing {
		weightedScore *= (1.0 - aps.failureWeight)
	}

	// Factor in response time (normalize to 0-1 scale, assuming 1000ms = 0 score)
	responseTimeFactor := 1.0
	if score.ResponseTime > 0 {
		// Better response time = higher score
		responseTimeFactor = 1.0 - float64(score.ResponseTime)/1000.0
		if responseTimeFactor < 0 {
			responseTimeFactor = 0
		}
	}
	weightedScore *= (1.0 - aps.responseTimeWeight) + (aps.responseTimeWeight * responseTimeFactor)

	// Apply consecutive failure penalty
	if score.ConsecutiveFails > 0 {
		penalty := float64(score.ConsecutiveFails) * aps.consecutiveFailPenalty
		weightedScore *= (1.0 - penalty)
	}

	// Ensure score is between 0 and 1
	if weightedScore < 0 {
		weightedScore = 0
	}
	if weightedScore > 1 {
		weightedScore = 1
	}

	return weightedScore
}

// UpdateHealthMetrics updates processor health metrics from health check data
func (aps *AdaptiveProcessorSelector) UpdateHealthMetrics(ctx context.Context) {
	aps.mutex.Lock()
	defer aps.mutex.Unlock()

	// Get health status from the payment processor client
	if aps.client != nil {
		healthStatus, err := aps.client.GetHealthStatus(ctx)
		if err != nil {
			logger.Warnf("Erro ao obter health status: %v", err)
			return
		}

		// Update default processor metrics
		if defaultScore, exists := aps.processors[entities.ProcessorTypeDefault]; exists {
			aps.updateProcessorScore(defaultScore, healthStatus.Default)
		}

		// Update fallback processor metrics
		if fallbackScore, exists := aps.processors[entities.ProcessorTypeFallback]; exists {
			aps.updateProcessorScore(fallbackScore, healthStatus.Fallback)
		}
	}

	// Log adaptive insights
	aps.logAdaptiveInsights()
}

// updateProcessorScore updates a single processor's score
func (aps *AdaptiveProcessorSelector) updateProcessorScore(score *ProcessorScore, health services.ProcessorHealth) {
	wasHealthy := score.IsHealthy
	score.IsHealthy = health.IsHealthy
	score.IsFailing = !health.IsHealthy
	score.ResponseTime = health.MinResponseTime
	score.LastUpdated = time.Now()

	// Update consecutive failure count
	if !health.IsHealthy {
		if !wasHealthy {
			score.ConsecutiveFails++
		} else {
			score.ConsecutiveFails = 1 // First failure
		}
	} else {
		score.ConsecutiveFails = 0 // Reset on success
	}

	// Adaptively adjust weight based on recent performance
	aps.adaptWeight(score)

	// Calculate new health score
	score.HealthScore = aps.calculateHealthScore(score)
}

// adaptWeight dynamically adjusts processor weight based on performance
func (aps *AdaptiveProcessorSelector) adaptWeight(score *ProcessorScore) {
	// Increase weight for consistently healthy processors
	if score.IsHealthy && score.ConsecutiveFails == 0 {
		score.Weight = score.Weight*0.95 + 0.05*1.0 // Gradually move towards 1.0
	} else if score.IsFailing {
		// Decrease weight for failing processors
		penalty := 0.1 * float64(score.ConsecutiveFails)
		if penalty > 0.5 {
			penalty = 0.5 // Max 50% penalty
		}
		score.Weight = score.Weight*0.9 + 0.1*(1.0-penalty)
	}

	// Ensure weight bounds
	if score.Weight < 0.1 {
		score.Weight = 0.1
	}
	if score.Weight > 1.0 {
		score.Weight = 1.0
	}
}

// calculateHealthScore calculates overall health score
func (aps *AdaptiveProcessorSelector) calculateHealthScore(score *ProcessorScore) float64 {
	healthScore := 1.0

	if score.IsFailing {
		healthScore -= 0.5 // Major penalty for failing
	}

	// Response time factor (0-500ms = full score, >1000ms = zero score)
	if score.ResponseTime > 0 {
		rtPenalty := float64(score.ResponseTime-500) / 500.0
		if rtPenalty > 0 {
			healthScore -= rtPenalty * 0.3 // Max 30% penalty for slow response
		}
	}

	// Consecutive failure penalty
	if score.ConsecutiveFails > 0 {
		healthScore -= float64(score.ConsecutiveFails) * 0.1
	}

	if healthScore < 0 {
		healthScore = 0
	}

	return healthScore
}

// logAdaptiveInsights logs current adaptive selection insights
func (aps *AdaptiveProcessorSelector) logAdaptiveInsights() {
	defaultScore := aps.processors[entities.ProcessorTypeDefault]
	fallbackScore := aps.processors[entities.ProcessorTypeFallback]

	defaultWeighted := aps.calculateWeightedScore(defaultScore)
	fallbackWeighted := aps.calculateWeightedScore(fallbackScore)

	logger.WithFields(map[string]interface{}{
		"default_score":   defaultWeighted,
		"fallback_score":  fallbackWeighted,
		"default_rt":      defaultScore.ResponseTime,
		"fallback_rt":     fallbackScore.ResponseTime,
		"default_fails":   defaultScore.ConsecutiveFails,
		"fallback_fails":  fallbackScore.ConsecutiveFails,
		"default_weight":  defaultScore.Weight,
		"fallback_weight": fallbackScore.Weight,
	}).Debug("Adaptive processor metrics updated")
}

// GetAllProcessorStats returns current processor statistics for all processors
func (aps *AdaptiveProcessorSelector) GetAllProcessorStats() map[string]interface{} {
	aps.mutex.RLock()
	defer aps.mutex.RUnlock()

	stats := make(map[string]interface{})

	for processorType, score := range aps.processors {
		stats[string(processorType)] = map[string]interface{}{
			"health_score":      score.HealthScore,
			"weighted_score":    aps.calculateWeightedScore(score),
			"response_time":     score.ResponseTime,
			"is_healthy":        score.IsHealthy,
			"is_failing":        score.IsFailing,
			"consecutive_fails": score.ConsecutiveFails,
			"weight":            score.Weight,
			"last_updated":      score.LastUpdated.Unix(),
		}
	}

	return map[string]interface{}{
		"processors":            stats,
		"adaptive_threshold":    aps.adaptiveThreshold,
		"failure_weight":        aps.failureWeight,
		"response_time_weight":  aps.responseTimeWeight,
		"recommended_processor": string(aps.SelectOptimalProcessor(context.Background())),
	}
}

// StartAdaptiveMonitoring starts background monitoring for adaptive selection
func (aps *AdaptiveProcessorSelector) StartAdaptiveMonitoring(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(aps.healthCheckInterval)
		defer ticker.Stop()

		logger.Info("Adaptive processor monitoring iniciado")

		for {
			select {
			case <-ticker.C:
				aps.UpdateHealthMetrics(ctx)
			case <-ctx.Done():
				logger.Info("Adaptive processor monitoring parado")
				return
			}
		}
	}()
}

// OnCircuitBreakerFailure is called when a circuit breaker records a failure
func (aps *AdaptiveProcessorSelector) OnCircuitBreakerFailure(processorType entities.ProcessorType, _ map[string]interface{}) {
	aps.mutex.Lock()
	defer aps.mutex.Unlock()

	if score, exists := aps.processors[processorType]; exists {
		// INTEGRAÇÃO: Circuit breaker informa sobre falhas
		score.ConsecutiveFails++
		score.IsFailing = true
		score.LastUpdated = time.Now()

		// Penalizar weight mais agressivamente quando circuit breaker falha
		penalty := 0.15 * float64(score.ConsecutiveFails)
		if penalty > 0.6 {
			penalty = 0.6 // Max 60% penalty
		}
		score.Weight *= (1.0 - penalty)

		if score.Weight < 0.1 {
			score.Weight = 0.1
		}

		logger.Debugf("Adaptive: Circuit breaker failure para %s - weight ajustado para %.3f",
			processorType, score.Weight)
	}
}

// OnCircuitBreakerSuccess is called when a circuit breaker records a success
func (aps *AdaptiveProcessorSelector) OnCircuitBreakerSuccess(processorType entities.ProcessorType, _ map[string]interface{}) {
	aps.mutex.Lock()
	defer aps.mutex.Unlock()

	if score, exists := aps.processors[processorType]; exists {
		// INTEGRAÇÃO: Circuit breaker informa sobre sucessos
		score.ConsecutiveFails = 0
		score.IsFailing = false
		score.LastUpdated = time.Now()

		// Aumentar weight quando circuit breaker tem sucesso
		if score.Weight < 1.0 {
			score.Weight = score.Weight*0.9 + 0.1*1.0 // Move gradualmente para 1.0
		}

		logger.Debugf("Adaptive: Circuit breaker success para %s - weight ajustado para %.3f",
			processorType, score.Weight)
	}
}

// OnCircuitBreakerStateChange is called when a circuit breaker changes state
func (aps *AdaptiveProcessorSelector) OnCircuitBreakerStateChange(processorType entities.ProcessorType, newState string, _ map[string]interface{}) {
	aps.mutex.Lock()
	defer aps.mutex.Unlock()

	if score, exists := aps.processors[processorType]; exists {
		switch newState {
		case "open":
			// Circuit breaker aberto = processador muito problemático
			score.IsFailing = true
			score.Weight *= 0.3 // Reduz drasticamente o peso
			logger.Warnf("🔴 Adaptive: Circuit breaker %s ABERTO - peso reduzido para %.3f",
				processorType, score.Weight)

		case "half-open":
			// Circuit breaker half-open = tentando recuperar
			score.Weight = 0.5 // Peso neutro durante recuperação
			logger.Infof("🟡 Adaptive: Circuit breaker %s HALF-OPEN - peso ajustado para %.3f",
				processorType, score.Weight)

		case "closed":
			// Circuit breaker fechado = processador recuperado
			score.IsFailing = false
			score.ConsecutiveFails = 0
			score.Weight = 1.0 // Restaura peso total
			logger.Infof("🟢 Adaptive: Circuit breaker %s FECHADO - peso restaurado para %.3f",
				processorType, score.Weight)
		}

		score.LastUpdated = time.Now()
	}
}

// GetCircuitBreakerRecommendation provides recommendations for circuit breaker settings
func (aps *AdaptiveProcessorSelector) GetCircuitBreakerRecommendation(processorType entities.ProcessorType) map[string]interface{} {
	aps.mutex.RLock()
	defer aps.mutex.RUnlock()

	score, exists := aps.processors[processorType]
	if !exists {
		return map[string]interface{}{"error": "processor not found"}
	}

	recommendations := make(map[string]interface{})

	// Recomendar timeout baseado na performance
	if score.ResponseTime < 100 {
		recommendations["suggested_timeout_ms"] = 200
		recommendations["reasoning"] = "Processador muito rápido - timeout agressivo"
	} else if score.ResponseTime < 500 {
		recommendations["suggested_timeout_ms"] = 500
		recommendations["reasoning"] = "Processador moderado - timeout balanceado"
	} else {
		recommendations["suggested_timeout_ms"] = 1000
		recommendations["reasoning"] = "Processador lento - timeout conservador"
	}

	// Recomendar max failures baseado na estabilidade
	if score.ConsecutiveFails > 5 {
		recommendations["suggested_max_failures"] = 1
		recommendations["reasoning"] = "Processador instável - falha rápido"
	} else if score.ConsecutiveFails > 2 {
		recommendations["suggested_max_failures"] = 2
		recommendations["reasoning"] = "Processador moderadamente estável"
	} else {
		recommendations["suggested_max_failures"] = 3
		recommendations["reasoning"] = "Processador estável - permite mais tentativas"
	}

	return recommendations
}

// NOVA: recalculateHealthScore recalcula o health score baseado nas métricas atuais
func (aps *AdaptiveProcessorSelector) recalculateHealthScore(score *ProcessorScore) {
	if score == nil {
		return
	}

	// Calcular health score baseado em múltiplos fatores
	healthScore := 1.0

	// Fator 1: Status de falha
	if score.IsFailing {
		healthScore *= 0.3 // Penalidade severa por estar falhando
	}

	// Fator 2: Falhas consecutivas
	if score.ConsecutiveFails > 0 {
		penalty := float64(score.ConsecutiveFails) * 0.15
		healthScore *= (1.0 - penalty)
		if healthScore < 0.1 {
			healthScore = 0.1
		}
	}

	// Fator 3: Tempo de resposta (normalizado)
	if score.ResponseTime > 0 {
		// Tempos menores que 200ms são excelentes
		// Tempos entre 200-500ms são bons
		// Tempos maiores que 500ms são ruins
		if score.ResponseTime <= 200 {
			healthScore *= 1.0 // Sem penalidade
		} else if score.ResponseTime <= 500 {
			penalty := float64(score.ResponseTime-200) / 300.0 * 0.3
			healthScore *= (1.0 - penalty)
		} else {
			penalty := 0.5 + (float64(score.ResponseTime-500) / 1000.0 * 0.3)
			if penalty > 0.8 {
				penalty = 0.8
			}
			healthScore *= (1.0 - penalty)
		}
	}

	// Fator 4: Tempo desde última atualização
	timeSinceUpdate := time.Since(score.LastUpdated)
	if timeSinceUpdate > 30*time.Second {
		agesPenalty := timeSinceUpdate.Seconds() / 30.0 * 0.2
		if agesPenalty > 0.5 {
			agesPenalty = 0.5
		}
		healthScore *= (1.0 - agesPenalty)
	}

	// Garantir que o score está no intervalo [0, 1]
	if healthScore < 0 {
		healthScore = 0
	}
	if healthScore > 1 {
		healthScore = 1
	}

	score.HealthScore = healthScore
	score.IsHealthy = healthScore > 0.5
}

// NOVA: GetProcessorStats retorna estatísticas detalhadas de um processador
func (aps *AdaptiveProcessorSelector) GetProcessorStats(processorType entities.ProcessorType) map[string]interface{} {
	aps.mutex.RLock()
	defer aps.mutex.RUnlock()

	score, exists := aps.processors[processorType]
	if !exists {
		return map[string]interface{}{
			"error": "processor not found",
		}
	}

	return map[string]interface{}{
		"processor_type":    string(score.ProcessorType),
		"health_score":      score.HealthScore,
		"response_time_ms":  score.ResponseTime,
		"is_healthy":        score.IsHealthy,
		"is_failing":        score.IsFailing,
		"consecutive_fails": score.ConsecutiveFails,
		"weight":            score.Weight,
		"last_updated":      score.LastUpdated.Format(time.RFC3339),
		"weighted_score":    aps.calculateWeightedScore(score),
	}
}

// NOVA: GetRecommendationsForProcessor retorna recomendações específicas para um processador
func (aps *AdaptiveProcessorSelector) GetRecommendationsForProcessor(processorType entities.ProcessorType) map[string]interface{} {
	aps.mutex.RLock()
	defer aps.mutex.RUnlock()

	score, exists := aps.processors[processorType]
	if !exists {
		return map[string]interface{}{
			"error": "processor not found",
		}
	}

	recommendations := make(map[string]interface{})

	// Recomendar timeout baseado no tempo de resposta
	if score.ResponseTime < 200 {
		recommendations["suggested_timeout_ms"] = 300
		recommendations["reasoning"] = "Processador muito rápido - timeout agressivo"
	} else if score.ResponseTime < 500 {
		recommendations["suggested_timeout_ms"] = 500
		recommendations["reasoning"] = "Processador moderado - timeout balanceado"
	} else {
		recommendations["suggested_timeout_ms"] = 1000
		recommendations["reasoning"] = "Processador lento - timeout conservador"
	}

	// Recomendar max failures baseado na estabilidade
	if score.ConsecutiveFails > 5 {
		recommendations["suggested_max_failures"] = 1
		recommendations["reasoning"] = "Processador instável - falha rápido"
	} else if score.ConsecutiveFails > 2 {
		recommendations["suggested_max_failures"] = 2
		recommendations["reasoning"] = "Processador moderadamente estável"
	} else {
		recommendations["suggested_max_failures"] = 3
		recommendations["reasoning"] = "Processador estável - permite mais tentativas"
	}

	return recommendations
}

// NOVA: GetAllStats retorna estatísticas de todos os processadores
func (aps *AdaptiveProcessorSelector) GetAllStats() map[string]interface{} {
	aps.mutex.RLock()
	defer aps.mutex.RUnlock()

	stats := make(map[string]interface{})

	processors := make(map[string]interface{})
	for processorType, score := range aps.processors {
		processors[string(processorType)] = map[string]interface{}{
			"health_score":      score.HealthScore,
			"response_time_ms":  score.ResponseTime,
			"is_healthy":        score.IsHealthy,
			"is_failing":        score.IsFailing,
			"consecutive_fails": score.ConsecutiveFails,
			"weight":            score.Weight,
			"last_updated":      score.LastUpdated.Format(time.RFC3339),
			"weighted_score":    aps.calculateWeightedScore(score),
		}
	}

	stats["processors"] = processors
	stats["configuration"] = map[string]interface{}{
		"health_check_interval":    aps.healthCheckInterval.String(),
		"adaptive_threshold":       aps.adaptiveThreshold,
		"failure_weight":           aps.failureWeight,
		"response_time_weight":     aps.responseTimeWeight,
		"consecutive_fail_penalty": aps.consecutiveFailPenalty,
	}

	// Adicionar recomendação atual
	optimalProcessor := aps.SelectOptimalProcessor(context.Background())
	stats["current_recommendation"] = string(optimalProcessor)

	return stats
}

// NOVA: UpdateProcessorHealthDirect atualiza diretamente a saúde de um processador
func (aps *AdaptiveProcessorSelector) UpdateProcessorHealthDirect(
	processorType entities.ProcessorType,
	responseTime time.Duration,
	isSuccess bool,
) {
	aps.mutex.Lock()
	defer aps.mutex.Unlock()

	score, exists := aps.processors[processorType]
	if !exists {
		// Criar novo score se não existir
		score = &ProcessorScore{
			ProcessorType: processorType,
			HealthScore:   1.0,
			Weight:        1.0,
			IsHealthy:     true,
			LastUpdated:   time.Now(),
		}
		aps.processors[processorType] = score
	}

	// Atualizar métricas
	score.ResponseTime = responseTime.Milliseconds()
	score.LastUpdated = time.Now()

	if isSuccess {
		score.ConsecutiveFails = 0
		score.IsFailing = false
		score.IsHealthy = true

		// Melhorar weight gradualmente
		score.Weight *= 1.02
		if score.Weight > 1.0 {
			score.Weight = 1.0
		}
	} else {
		score.ConsecutiveFails++
		score.IsFailing = true
		score.IsHealthy = false

		// Penalizar weight
		score.Weight *= 0.95
		if score.Weight < 0.1 {
			score.Weight = 0.1
		}
	}

	// Recalcular health score
	aps.recalculateHealthScore(score)

	logger.Debugf("Adaptive Processor atualizado: %s (success: %v, response: %dms, health: %.3f, weight: %.3f)",
		processorType, isSuccess, responseTime.Milliseconds(), score.HealthScore, score.Weight)
}

// NOVA: StartHealthMonitoring inicia monitoramento automático de saúde
func (aps *AdaptiveProcessorSelector) StartHealthMonitoring(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(aps.healthCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Info("Parando monitoramento de saúde do Adaptive Processor")
				return
			case <-ticker.C:
				aps.performHealthCheck()
			}
		}
	}()

	logger.Infof("Monitoramento de saúde do Adaptive Processor iniciado (intervalo: %v)", aps.healthCheckInterval)
}

// NOVA: performHealthCheck executa verificação de saúde automática
func (aps *AdaptiveProcessorSelector) performHealthCheck() {
	aps.mutex.Lock()
	defer aps.mutex.Unlock()

	now := time.Now()

	for processorType, score := range aps.processors {
		// Verificar se está muito tempo sem atualizações
		timeSinceUpdate := now.Sub(score.LastUpdated)

		if timeSinceUpdate > 60*time.Second {
			// Marcar como potencialmente não saudável se sem atualizações por muito tempo
			score.Weight *= 0.98
			if score.Weight < 0.5 {
				score.IsHealthy = false
			}

			logger.Warnf("Processador %s sem atualizações há %v (weight reduzido para %.3f)",
				processorType, timeSinceUpdate, score.Weight)
		}

		// Recalcular health score
		aps.recalculateHealthScore(score)
	}
}

// NOVA: ShouldSwitchProcessor determina se deveria trocar de processador
func (aps *AdaptiveProcessorSelector) ShouldSwitchProcessor(
	currentProcessor entities.ProcessorType,
) (bool, entities.ProcessorType, string) {
	aps.mutex.RLock()
	defer aps.mutex.RUnlock()

	currentScore := aps.processors[currentProcessor]
	if currentScore == nil {
		return true, entities.ProcessorTypeDefault, "processador atual não encontrado"
	}

	// Se o processador atual não está saudável, definitivamente trocar
	if !currentScore.IsHealthy {
		optimalProcessor := aps.SelectOptimalProcessor(context.Background())
		if optimalProcessor != currentProcessor {
			return true, optimalProcessor, "processador atual não saudável"
		}
	}

	// Verificar se existe uma alternativa significativamente melhor
	optimalProcessor := aps.SelectOptimalProcessor(context.Background())
	if optimalProcessor != currentProcessor {
		optimalScore := aps.processors[optimalProcessor]
		if optimalScore != nil {
			currentWeightedScore := aps.calculateWeightedScore(currentScore)
			optimalWeightedScore := aps.calculateWeightedScore(optimalScore)

			scoreDifference := optimalWeightedScore - currentWeightedScore

			// Se a diferença for significativa, recomendar troca
			if scoreDifference > aps.adaptiveThreshold*2 { // Threshold mais alto para evitar troca frequente
				return true, optimalProcessor, fmt.Sprintf("alternativa significativamente melhor (diferença: %.3f)", scoreDifference)
			}
		}
	}

	return false, currentProcessor, "processador atual é optimal"
}
