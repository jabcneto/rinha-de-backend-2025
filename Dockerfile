# Multi-stage build para otimizar o tamanho da imagem
FROM golang:1.23-alpine AS builder

# Instalar dependências necessárias
RUN apk add --no-cache git ca-certificates tzdata

# Configurar diretório de trabalho
WORKDIR /app

# Copiar arquivos de dependências
COPY go.mod go.sum ./

# Baixar dependências
RUN go mod download

# Copiar código fonte e init.sql
COPY . .

# Build da aplicação com otimizações
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o main ./cmd/main.go

# Imagem final mínima
FROM alpine:3.19

# Instalar certificados SSL e curl para health checks
RUN apk --no-cache add ca-certificates tzdata curl

# Criar usuário não-root
RUN addgroup -g 1001 -S app && \
    adduser -S app -u 1001 -G app

# Configurar diretório de trabalho
WORKDIR /app

# Copiar o binário da aplicação e o init.sql
COPY --from=builder /app/main .
COPY --from=builder /app/init.sql .

# Definir propriedades do arquivo
RUN chmod +x main

# Mudar para usuário não-root
USER app

# Expor porta
EXPOSE 9999

# Comando de execução
CMD ["./main"]
