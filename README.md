# Rinha de Backend 2025 - Payment Processor

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17-336791?style=flat&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker&logoColor=white)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

> 🚀 Implementação em Go para a [Rinha de Backend 2025](https://github.com/zanfranceschi/rinha-de-backend-2025) com foco em performance, escalabilidade e resiliência.

## 📋 Índice

- [Sobre o Projeto](#-sobre-o-projeto)
- [Arquitetura](#-arquitetura)
- [Tecnologias](#-tecnologias)
- [Início Rápido](#-início-rápido)
- [API Endpoints](#-api-endpoints)
- [Performance](#-performance)
- [Monitoramento](#-monitoramento)
- [Desenvolvimento](#-desenvolvimento)
- [Contribuição](#-contribuição)

## 🎯 Sobre o Projeto

Esta implementação foi desenvolvida especificamente para participar da **Rinha de Backend 2025**, um desafio que testa a capacidade de criar aplicações backend de alta performance e resiliência.

### Características Principais

- ✅ **Alta Performance**: Otimizado para processar milhares de transações por segundo
- ✅ **Arquitetura Limpa**: Separação clara de responsabilidades e fácil manutenção
- ✅ **Resiliência**: Circuit breakers, retry logic e graceful degradation
- ✅ **Escalabilidade**: Auto-scaling de workers e otimizações de banco
- ✅ **Observabilidade**: Logs estruturados e métricas de performance

## 🏗️ Arquitetura

O projeto segue os princípios da **Clean Architecture**, garantindo:

```
cmd/                    # Ponto de entrada da aplicação
├── main.go            # Bootstrap e configuração inicial

internal/              # Código interno da aplicação
├── application/       # Casos de uso e DTOs
│   ├── dtos/         # Data Transfer Objects
│   └── usecases/     # Regras de negócio
├── domain/           # Entidades e regras de domínio
│   ├── entities/     # Modelos de domínio
│   ├── repositories/ # Contratos de persistência
│   └── services/     # Serviços de domínio
├── infrastructure/   # Implementações concretas
│   ├── database/     # Repositórios PostgreSQL
│   ├── external/     # Integrações externas
│   └── queue/        # Sistema de filas
└── interfaces/       # Camada de entrada
    ├── http/         # Handlers REST
    └── middleware/   # Middlewares HTTP
```

### Principais Componentes

- **Payment Processor**: Processa pagamentos de forma assíncrona
- **Queue System**: Sistema de filas com auto-scaling
- **Circuit Breaker**: Proteção contra falhas em cascata
- **Retry Logic**: Reprocessamento inteligente de falhas
- **Database Optimization**: PostgreSQL otimizado para alta concorrência

## 🛠️ Tecnologias

| Categoria | Tecnologia | Versão | Propósito |
|-----------|------------|---------|-----------|
| **Backend** | Go | 1.21+ | Linguagem principal |
| **Database** | PostgreSQL | 17-alpine | Persistência ACID |
| **HTTP Router** | Gorilla Mux | 1.8.1 | Roteamento HTTP |
| **UUID** | Google UUID | 1.6.0 | Geração de IDs únicos |
| **Database Driver** | lib/pq | 1.10.9 | Driver PostgreSQL |
| **Containerization** | Docker | Latest | Containerização |
| **Reverse Proxy** | Nginx | Latest | Load balancing |

## 🚀 Início Rápido

### Pré-requisitos

- [Docker](https://www.docker.com/) 20.10+
- [Docker Compose](https://docs.docker.com/compose/) 2.0+
- (Opcional) [Go](https://golang.org/) 1.21+ para desenvolvimento

### Execução com Docker

```bash
# Clone o repositório
git clone https://github.com/jabcneto/rinha-de-backend-2025
cd rinha-backend

# Inicie todos os serviços
docker-compose up -d

# Verifique os logs
docker-compose logs -f app

# Teste a aplicação
curl http://localhost:9999/health
```

### Execução Local (Desenvolvimento)

```bash
# Configure as variáveis de ambiente
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/payments?sslmode=disable"
export PORT=8080

# Instale as dependências
go mod download

# Execute a aplicação
go run cmd/main.go
```

## 📡 API Endpoints

### Health Check
```http
GET /health
```

**Response:**
```json
{
  "status": "ok",
  "timestamp": "2025-01-08T15:30:00Z"
}
```

### Processar Pagamento
```http
POST /payments
Content-Type: application/json

{
  "correlationId": "550e8400-e29b-41d4-a716-446655440000",
  "amount": 100.50
}
```

**Response:**
```json
{
  "message": "Payment request received",
  "correlationId": "550e8400-e29b-41d4-a716-446655440000"
}
```

### Resumo de Pagamentos
```http
GET /payments-summary
```

**Response:**
```json
{
  "default": {
    "totalRequests": 800,
    "totalAmount": 20000.50
  },
  "fallback": {
    "totalRequests": 200,
    "totalAmount": 5000.25
  }
}
```

## ⚡ Performance

### Otimizações Implementadas

1. **Database Tuning**:
   - Shared buffers otimizado para containers
   - WAL compression habilitado
   - Checkpoints ajustados para performance
   - Índices estratégicos para consultas

2. **Application Level**:
   - Connection pooling configurado
   - Processamento assíncrono com workers
   - Circuit breakers para resiliência
   - Cache de resultados agregados

3. **Infrastructure**:
   - Nginx como reverse proxy
   - Auto-scaling de workers baseado em carga
   - Graceful shutdown para zero downtime

### Métricas Esperadas

- **Throughput**: 10k+ req/s em hardware modesto
- **Latência**: P95 < 100ms para operações de escrita
- **Disponibilidade**: 99.9%+ com circuit breakers
- **Recovery Time**: < 5s após falhas temporárias

## 🔧 Desenvolvimento

### Estrutura de Configuração

```go
type Config struct {
    Database struct {
        URL             string
        MaxConnections  int
        MaxIdleTime     time.Duration
    }
    HTTP struct {
        Port         string
        ReadTimeout  time.Duration
        WriteTimeout time.Duration
    }
    Queue struct {
        WorkerCount    int
        BufferSize     int
        RetryAttempts  int
    }
}
```

### Variáveis de Ambiente

| Variável | Descrição | Padrão |
|----------|-----------|---------|
| `DATABASE_URL` | URL de conexão PostgreSQL | `postgres://postgres:postgres@localhost:5432/payments` |
| `PORT` | Porta HTTP da aplicação | `8080` |
| `WORKER_COUNT` | Número de workers para processamento | `10` |
| `MAX_DB_CONNECTIONS` | Máximo de conexões com DB | `25` |
| `CIRCUIT_BREAKER_THRESHOLD` | Limite do circuit breaker | `5` |

## 📈 Estratégias de Escalabilidade

1. **Horizontal Scaling**: Múltiplas instâncias da aplicação
2. **Database Optimization**: Índices, particionamento, read replicas
3. **Queue Management**: Auto-scaling baseado em backlog
4. **Circuit Breakers**: Proteção contra cascata de falhas
5. **Graceful Degradation**: Funcionalidade reduzida em sobrecarga

### Padrões de Código

- Siga as convenções do Go (gofmt, golint)
- Escreva testes para novas funcionalidades
- Mantenha a documentação atualizada
- Use commits semânticos

## 📝 Licença

Este projeto está sob a licença MIT. Veja o arquivo [LICENSE](LICENSE) para mais detalhes.

## 🎯 Rinha de Backend 2025

Este projeto foi desenvolvido especificamente para o desafio **Rinha de Backend 2025**. Para mais informações sobre o desafio, visite o [repositório oficial](https://github.com/zanfranceschi/rinha-de-backend-2025).

### Objetivos Atendidos

- ✅ API REST completa conforme especificação
- ✅ Persistência em PostgreSQL
- ✅ Containerização com Docker
- ✅ Performance otimizada para alta concorrência
- ✅ Resiliência e recuperação de falhas
- ✅ Observabilidade e monitoramento

---

<div align="center">
  <p><strong>Desenvolvido com ❤️ para a Rinha de Backend 2025</strong></p>
  <p>🚀 <em>Go fast, scale hard, fail gracefully</em> 🚀</p>
</div>
