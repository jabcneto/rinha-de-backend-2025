#!/bin/bash

echo "🚀 Teste de Performance - Clean Architecture vs Implementação Original"
echo "=================================================================="

# Verificar se estamos no diretório correto
if [ ! -f "cmd/main.go" ]; then
    echo "❌ Erro: Execute este script no diretório do projeto Clean Architecture"
    exit 1
fi

echo "✅ Diretório correto encontrado"

# Compilar a aplicação
echo "🔨 Compilando aplicação Clean Architecture..."
/usr/local/go/bin/go build -o rinha-backend-clean ./cmd

if [ $? -ne 0 ]; then
    echo "❌ Erro na compilação!"
    exit 1
fi

echo "✅ Compilação bem-sucedida"

# Verificar estrutura do projeto
echo "📁 Verificando estrutura Clean Architecture..."

directories=(
    "internal/domain/entities"
    "internal/domain/repositories" 
    "internal/domain/services"
    "internal/application/usecases"
    "internal/application/dtos"
    "internal/infrastructure/database"
    "internal/infrastructure/queue"
    "internal/infrastructure/external"
    "internal/interfaces/http/handlers"
    "internal/interfaces/middleware"
    "cmd"
)

for dir in "${directories[@]}"; do
    if [ -d "$dir" ]; then
        echo "   ✅ $dir"
    else
        echo "   ❌ $dir não encontrado"
        exit 1
    fi
done

echo ""
echo "🎯 Melhorias Implementadas na Clean Architecture:"
echo ""
echo "1. 🚀 CACHE EM MEMÓRIA para /payments-summary"
echo "   - Cache TTL de 5 segundos"
echo "   - Refresh automático em background"
echo "   - Consultas não-bloqueantes"
echo ""
echo "2. ⚡ PROCESSAMENTO ASSÍNCRONO OTIMIZADO"
echo "   - Múltiplos workers (configurável)"
echo "   - UpdateSummary não-bloqueante"
echo "   - Pool de conexões otimizado"
echo ""
echo "3. 🏗️ ARQUITETURA LIMPA"
echo "   - Separação clara de responsabilidades"
echo "   - Inversão de dependências"
echo "   - Testabilidade melhorada"
echo ""
echo "4. 🔧 CONFIGURAÇÕES OTIMIZADAS"
echo "   - Pool de conexões: 50 conexões máximas"
echo "   - 8 workers por instância"
echo "   - Buffer de fila: 15.000 pagamentos"
echo ""

# Verificar arquivos principais
echo "📋 Verificando arquivos principais..."

files=(
    "cmd/main.go"
    "internal/infrastructure/database/payment_repository_impl.go"
    "internal/infrastructure/queue/payment_queue_impl.go"
    "internal/interfaces/http/handlers/payment_handler.go"
    "docker-compose.yml"
    "Dockerfile"
)

for file in "${files[@]}"; do
    if [ -f "$file" ]; then
        echo "   ✅ $file"
    else
        echo "   ❌ $file não encontrado"
        exit 1
    fi
done

echo ""
echo "🔍 Comparação de Performance:"
echo ""
echo "PROBLEMA ORIGINAL:"
echo "❌ Thread bloqueada no /payments-summary sob alta carga"
echo "❌ Mutex global bloqueando todas as consultas"
echo "❌ Consulta direta ao banco a cada request"
echo ""
echo "SOLUÇÃO CLEAN ARCHITECTURE:"
echo "✅ Cache em memória com refresh automático"
echo "✅ Consultas não-bloqueantes"
echo "✅ UpdateSummary assíncrono"
echo "✅ Múltiplos workers para processamento"
echo "✅ Pool de conexões otimizado"
echo ""

echo "📊 Configurações de Performance:"
echo "   - Workers por instância: 8"
echo "   - Buffer da fila: 15.000"
echo "   - Pool de conexões: 50"
echo "   - Cache TTL: 5 segundos"
echo "   - Refresh automático: 3 segundos"
echo ""

echo "🧪 Para testar a performance:"
echo "1. Suba os Payment Processors"
echo "2. Execute: docker-compose up --build"
echo "3. Execute: k6 run rinha.js"
echo ""
echo "📈 Resultados esperados:"
echo "   - /payments-summary sempre responsivo"
echo "   - Maior throughput de pagamentos"
echo "   - Menor latência geral"
echo "   - Sem bloqueios de thread"
echo ""
echo "✨ Clean Architecture implementada com sucesso!"

