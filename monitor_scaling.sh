#!/bin/bash

# Script de monitoramento em tempo real do auto-scaling
echo "🔍 Monitor de Auto-Scaling - Rinha Backend 2025"
echo "================================================"
echo ""

# Cores para output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# URL base da API
API_URL="http://localhost:9999"

# Função para mostrar métricas em tempo real
monitor_scaling() {
    echo -e "${BLUE}Iniciando monitoramento em tempo real...${NC}"
    echo "Pressione Ctrl+C para sair"
    echo ""

    while true; do
        # Clear screen
        clear

        echo -e "${BLUE}🚀 AUTO-SCALING MONITOR - $(date)${NC}"
        echo "================================================"

        # Get scaling metrics
        response=$(curl -s "$API_URL/scaling-metrics" 2>/dev/null)

        if [ $? -eq 0 ] && [ ! -z "$response" ]; then
            # Parse JSON response (simplified - usando jq seria melhor)
            echo -e "${GREEN}📊 MÉTRICAS DE AUTO-SCALING:${NC}"
            echo "$response" | python3 -m json.tool 2>/dev/null || echo "$response"
            echo ""

            # Get health status
            health=$(curl -s "$API_URL/health" 2>/dev/null)
            if [ $? -eq 0 ] && [ ! -z "$health" ]; then
                echo -e "${YELLOW}💚 STATUS DE SAÚDE:${NC}"
                echo "$health" | python3 -m json.tool 2>/dev/null || echo "$health"
            fi
        else
            echo -e "${RED}❌ Erro ao conectar com a API em $API_URL${NC}"
            echo "Verifique se o servidor está rodando..."
        fi

        echo ""
        echo -e "${BLUE}Próxima atualização em 5 segundos...${NC}"
        sleep 5
    done
}

# Função para teste de carga
load_test() {
    echo -e "${YELLOW}🧪 Iniciando teste de carga para demonstrar auto-scaling...${NC}"

    # Criar múltiplos pagamentos simultaneamente
    for i in {1..100}; do
        correlation_id=$(uuidgen 2>/dev/null || echo "test-$i-$(date +%s)")
        amount=$(( ( RANDOM % 1000 ) + 1 ))

        curl -s -X POST "$API_URL/payments" \
            -H "Content-Type: application/json" \
            -d "{
                \"correlationId\": \"$correlation_id\",
                \"amount\": $amount
            }" > /dev/null &

        if [ $(( $i % 10 )) -eq 0 ]; then
            echo "Enviados $i pagamentos..."
        fi
    done

    wait
    echo -e "${GREEN}✅ 100 pagamentos enviados! Observe as métricas de scaling.${NC}"
}

# Função para mostrar resumo das configurações
show_config() {
    echo -e "${BLUE}⚙️  CONFIGURAÇÕES DE AUTO-SCALING:${NC}"
    echo "• Workers mínimos: 10"
    echo "• Workers máximos: 50"
    echo "• Workers iniciais: 20"
    echo "• Threshold scale-up: 80% utilização"
    echo "• Threshold scale-down: 30% utilização"
    echo "• Intervalo de verificação: 5 segundos"
    echo "• Buffer da fila: 15.000 slots"
    echo ""
    echo -e "${YELLOW}📈 LOAD BALANCING NGINX:${NC}"
    echo "• Algoritmo: least_conn (menos conexões)"
    echo "• Health checks automáticos"
    echo "• Failover automático"
    echo "• Rate limiting: 100 req/s"
    echo "• Pool de conexões: 32 keep-alive"
    echo ""
}

# Menu principal
case "${1:-menu}" in
    "monitor")
        monitor_scaling
        ;;
    "load-test")
        load_test
        ;;
    "config")
        show_config
        ;;
    *)
        echo -e "${GREEN}🔧 Monitor de Auto-Scaling - Opções:${NC}"
        echo ""
        echo "1. ./monitor_scaling.sh monitor     - Monitor em tempo real"
        echo "2. ./monitor_scaling.sh load-test   - Teste de carga"
        echo "3. ./monitor_scaling.sh config      - Mostrar configurações"
        echo ""
        echo -e "${BLUE}Exemplos de uso:${NC}"
        echo "• Para monitorar: ./monitor_scaling.sh monitor"
        echo "• Para testar: ./monitor_scaling.sh load-test && ./monitor_scaling.sh monitor"
        echo ""
        show_config
        ;;
esac
