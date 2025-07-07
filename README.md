# Rinha Backend 2025 - Clean Architecture

## 🎯 Problema Resolvido

**PROBLEMA ORIGINAL**: Thread bloqueada no endpoint `/payments-summary` sob alta carga, impedindo consultas durante processamento intenso de pagamentos.

**SOLUÇÃO**: Refatoração completa para Clean Architecture com cache em memória, processamento assíncrono otimizado e separação clara de responsabilidades.

## 🏗️ Arquitetura

### Estrutura de Diretórios

```
rinha-backend-clean/
├── cmd/                              # Ponto de entrada da aplicação
│   └── main.go
├── internal/
│   ├── domain/                       # Camada de Domínio (regras de negócio)
│   │   ├── entities/                 # Entidades do domínio
│   │   │   ├── payment.go
│   │   │   └── payment_summary.go
│   │   ├── repositories/             # Interfaces dos repositórios
│   │   │   └── payment_repository.go
│   │   └── services/                 # Interfaces dos serviços
│   │       ├── payment_processor_service.go
│   │       └── queue_service.go
│   ├── application/                  # Camada de Aplicação (casos de uso)
│   │   ├── usecases/                 # Casos de uso
│   │   │   ├── process_payment.go
│   │   │   ├── get_payment_summary.go
│   │   │   └── purge_payments.go
│   │   └── dtos/                     # Data Transfer Objects
│   │       └── payment_request.go
│   ├── infrastructure/               # Camada de Infraestrutura
│   │   ├── database/                 # Implementação do banco de dados
│   │   │   ├── connection.go
│   │   │   └── payment_repository_impl.go
│   │   ├── queue/                    # Implementação da fila
│   │   │   └── payment_queue_impl.go
│   │   └── external/                 # Serviços externos
│   │       ├── payment_processor_client.go
│   │       └── circuit_breaker.go
│   └── interfaces/                   # Camada de Interface
│       ├── http/                     # Handlers HTTP
│       │   ├── handlers/
│       │   │   ├── payment_handler.go
│       │   │   └── health_handler.go
│       │   └── router.go
│       └── middleware/               # Middlewares
│           ├── logging.go
│           ├── recovery.go
│           └── cors.go
├── docker-compose.yml
├── Dockerfile
└── README.md
```

### Princípios da Clean Architecture

1. **Inversão de Dependências**: Camadas internas não dependem de camadas externas
2. **Separação de Responsabilidades**: Cada camada tem uma responsabilidade específica
3. **Testabilidade**: Interfaces permitem fácil criação de mocks
4. **Independência de Frameworks**: Lógica de negócio independente de tecnologias

## 🚀 Otimizações de Performance

### 1. Cache em Memória para `/payments-summary`

```go
type SummaryCache struct {
    data      *entities.PaymentSummary
    lastUpdate time.Time
    mutex     sync.RWMutex
    ttl       time.Duration // 5 segundos
}
```

**Benefícios:**
- ✅ Consultas não-bloqueantes
- ✅ Refresh automático em background (3s)
- ✅ Fallback para banco em caso de cache miss
- ✅ TTL configurável

### 2. Processamento Assíncrono Otimizado

```go
type PaymentQueueImpl struct {
    queue       chan *entities.Payment
    workerCount int                    // 8 workers por instância
    workers     []chan struct{}
}
```

**Benefícios:**
- ✅ Múltiplos workers paralelos
- ✅ UpdateSummary não-bloqueante
- ✅ Buffer de fila aumentado (15.000)
- ✅ Graceful shutdown

### 3. Pool de Conexões Otimizado

```go
db.SetMaxOpenConns(50)    // Aumentado de 25
db.SetMaxIdleConns(25)    // Mantém mais conexões idle
db.SetConnMaxLifetime(10 * time.Minute) // Maior lifetime
```

### 4. Middleware de Performance

- **Logging**: Rastreamento de latência por request
- **Recovery**: Recuperação de panics sem derrubar o servidor
- **CORS**: Headers otimizados

## 📊 Comparação: Antes vs Depois

| Aspecto | Implementação Original | Clean Architecture |
|---------|----------------------|-------------------|
| **Thread Blocking** | ❌ Mutex global bloqueia consultas | ✅ Cache não-bloqueante |
| **Consulta Summary** | ❌ Sempre vai ao banco | ✅ Cache + fallback |
| **Workers** | ❌ 1 worker por instância | ✅ 8 workers configuráveis |
| **Pool Conexões** | ❌ 25 conexões máx | ✅ 50 conexões máx |
| **Update Summary** | ❌ Bloqueante | ✅ Assíncrono |
| **Testabilidade** | ❌ Código acoplado | ✅ Interfaces + DI |
| **Manutenibilidade** | ❌ Tudo em main.go | ✅ Separação clara |

## 🔧 Configurações

### Variáveis de Ambiente

```bash
# Banco de dados
DATABASE_URL="host=db user=postgres password=postgres dbname=rinha_db sslmode=disable"

# Payment Processors
PAYMENT_PROCESSOR_URL_DEFAULT="http://payment-processor-default:8080"
PAYMENT_PROCESSOR_URL_FALLBACK="http://payment-processor-fallback:8080"

# Performance
QUEUE_BUFFER_SIZE="15000"      # Buffer da fila
QUEUE_WORKER_COUNT="8"         # Workers por instância
PORT="9999"                    # Porta do servidor
```

### Recursos Utilizados

```yaml
# Total: 1.5 CPU, 650MB RAM
api1:     0.4 CPU, 150MB RAM
api2:     0.4 CPU, 150MB RAM  
nginx:    0.2 CPU, 50MB RAM
db:       0.5 CPU, 300MB RAM
```

## 🚀 Como Executar

### 1. Pré-requisitos

```bash
# Subir Payment Processors primeiro
git clone https://github.com/zanfranceschi/rinha-de-backend-2025-payment-processor.git
cd rinha-de-backend-2025-payment-processor/payment-processor
docker-compose up -d
```

### 2. Executar Clean Architecture

```bash
cd rinha-backend-clean
docker-compose up --build
```

### 3. Testar Performance

```bash
# Executar script de teste
./test_performance.sh

# Executar k6
cd /caminho/para/rinha-test
k6 run rinha.js
```

## 🧪 Endpoints

### POST /payments
```json
{
    "correlationId": "uuid-v4",
    "amount": 123.45
}
```

### GET /payments-summary
```json
{
    "default": {
        "totalRequests": 1000,
        "totalAmount": 50000.00
    },
    "fallback": {
        "totalRequests": 50,
        "totalAmount": 2500.00
    }
}
```

### POST /purge-payments
```json
{
    "message": "All payments purged."
}
```

### GET /health
```json
{
    "status": "healthy",
    "timestamp": "2025-07-07T12:00:00Z",
    "queue_size": 42
}
```

## 📈 Resultados Esperados

### Performance
- **Latência /payments-summary**: < 5ms (cache hit)
- **Throughput**: > 500 req/s por instância
- **Zero bloqueios**: Consultas sempre responsivas
- **Graceful degradation**: Fallback automático

### Resiliência
- **Circuit Breaker**: Proteção contra falhas
- **Health Checks**: Monitoramento contínuo
- **Retry Logic**: Retentativos automáticos
- **Graceful Shutdown**: Parada segura

### Manutenibilidade
- **Testabilidade**: 100% das interfaces mockáveis
- **Extensibilidade**: Fácil adição de novos casos de uso
- **Debugging**: Logs estruturados por camada
- **Monitoramento**: Métricas por componente

## 🔍 Monitoramento

### Logs Estruturados
```
2025-07-07 12:00:00 [INFO] Pagamento recebido e enfileirado: uuid - R$ 100.50
2025-07-07 12:00:01 [INFO] Cache hit para payments-summary
2025-07-07 12:00:02 [INFO] Worker 3 processou pagamento via default
```

### Métricas Disponíveis
- Queue size via `/health`
- Latência por request (logs)
- Status dos Circuit Breakers
- Health status dos processors

## 🎯 Benefícios da Clean Architecture

1. **Performance**: Cache + processamento assíncrono
2. **Escalabilidade**: Workers configuráveis
3. **Manutenibilidade**: Código organizado e testável
4. **Resiliência**: Circuit breakers e fallbacks
5. **Flexibilidade**: Fácil troca de implementações
6. **Testabilidade**: Interfaces permitem mocks
7. **Monitoramento**: Logs e métricas estruturados

## 🏆 Conclusão

A refatoração para Clean Architecture não apenas resolveu o problema de thread bloqueada, mas também:

- **Melhorou a performance** com cache e processamento otimizado
- **Aumentou a manutenibilidade** com separação clara de responsabilidades  
- **Facilitou testes** com inversão de dependências
- **Preparou para escala** com arquitetura flexível

O resultado é um sistema robusto, performático e preparado para crescimento futuro.

