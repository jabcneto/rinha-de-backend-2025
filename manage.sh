#!/bin/bash

# Script para gerenciar o load balancer da aplicação Rinha Backend

case "$1" in
    "start")
        echo "🚀 Iniciando aplicação com load balancer..."
        docker-compose up --build -d
        echo "✅ Aplicação iniciada!"
        echo "📊 Load balancer disponível em: http://localhost:9999"
        echo "🔍 Para monitorar logs: ./manage.sh logs"
        ;;

    "stop")
        echo "🛑 Parando aplicação..."
        docker-compose down
        echo "✅ Aplicação parada!"
        ;;

    "restart")
        echo "🔄 Reiniciando aplicação..."
        docker-compose down
        docker-compose up --build -d
        echo "✅ Aplicação reiniciada!"
        ;;

    "logs")
        echo "📋 Logs da aplicação (Ctrl+C para sair):"
        docker-compose logs -f
        ;;

    "status")
        echo "📊 Status dos serviços:"
        docker-compose ps
        ;;

    "test")
        echo "🧪 Testando load balancer..."
        echo "Testando health check:"
        curl -s http://localhost:9999/health
        echo -e "\n"
        echo "Testando payments summary:"
        curl -s http://localhost:9999/payments-summary
        echo -e "\n"
        ;;

    "scale")
        if [ -z "$2" ]; then
            echo "❌ Especifique o número de instâncias: ./manage.sh scale 3"
            exit 1
        fi
        echo "📈 Escalando para $2 instâncias..."
        docker-compose up --scale api1=$2 --scale api2=$2 -d
        echo "✅ Aplicação escalada para $2 instâncias!"
        ;;

    *)
        echo "🔧 Gerenciador Load Balancer - Rinha Backend"
        echo ""
        echo "Comandos disponíveis:"
        echo "  start    - Inicia a aplicação com load balancer"
        echo "  stop     - Para a aplicação"
        echo "  restart  - Reinicia a aplicação"
        echo "  logs     - Mostra logs em tempo real"
        echo "  status   - Mostra status dos serviços"
        echo "  test     - Testa o load balancer"
        echo "  scale N  - Escala para N instâncias"
        echo ""
        echo "Exemplo: ./manage.sh start"
        ;;
esac
