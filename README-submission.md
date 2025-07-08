# Rinha de Backend 2025 - Payment Processor

**Participante:** João Antonio Barcelos Coutinho Neto 
**Repositório do código fonte:** https://github.com/jabcneto/rinha-de-backend-2025

## 🚀 Tecnologias Utilizadas

### Linguagens
- **Go 1.21** - Linguagem principal, escolhida pela performance e simplicidade

### Banco de Dados
- **PostgreSQL 17** - Banco principal com otimizações específicas para a rinha

### Load Balancer
- **Nginx** - Balanceamento de carga entre as instâncias da API

### Outras Tecnologias
- **Docker & Docker Compose** - Containerização e orquestração
- **Gorilla Mux** - Roteamento HTTP
- **UUID** - Geração de identificadores únicos
- **Circuit Breaker** - Padrão de resiliência
- **Auto Scaling** - Escalabilidade automática de workers

## 🏗️ Arquitetura

### Clean Architecture
- **Domain Layer**: Entidades e regras de negócio
- **Application Layer**: Casos de uso e DTOs
- **Infrastructure Layer**: Implementações concretas (banco, HTTP, etc.)
- **Interface Layer**: Handlers HTTP e middlewares

### Componentes Principais
- **2 Instâncias da API** - Load balanceadas via Nginx
- **Queue System** - Processamento assíncrono com workers
- **Circuit Breaker** - Resiliência contra falhas
- **Auto Scaling** - Ajuste automático de workers baseado na carga
- **Fallback System** - Processador de backup quando o principal falha

## 📊 Otimizações de Performance

### PostgreSQL
- Configurações específicas para máxima performance
- Índices estratégicos
- Tabela de summary agregado para consultas rápidas
- `fsync=off` e `synchronous_commit=off` para velocidade máxima

### Aplicação
- Workers assíncronos para processamento
- Pool de conexões otimizado
- Cache em memória para summaries
- Retry logic com backoff exponencial

## 🔧 Recursos Utilizados

- **Total CPU**: 1.5 cores
- **Total Memory**: 370MB
- **PostgreSQL**: 0.65 CPU, 155MB RAM
- **API Instances**: 0.4 CPU cada, 90MB RAM cada
- **Nginx**: 0.15 CPU, 35MB RAM

## 📈 Endpoints Implementados

- `POST /payments` - Processar pagamentos
- `GET /payments-summary` - Resumo agregado de pagamentos
- `POST /purge-payments` - Limpeza de dados
- `GET /health` - Health check
- `GET /scaling-metrics` - Métricas de escalabilidade

## 🎯 Destaques da Implementação

1. **Resiliência**: Circuit breakers e retry logic
2. **Performance**: Processamento assíncrono e otimizações de banco
3. **Escalabilidade**: Auto-scaling baseado na demanda
4. **Observabilidade**: Logs estruturados e métricas
5. **Clean Code**: Arquitetura limpa e bem estruturada
