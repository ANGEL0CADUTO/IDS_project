# Fase 1: Build dell'eseguibile
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copia l'intero contesto del progetto
COPY . .

# Esegui i comandi Go
RUN go work sync
RUN go mod download

# Build del binario del collector
RUN CGO_ENABLED=0 GOOS=linux go build -o /collector-service ./services/collector

# Scarica grpc_health_probe
RUN wget -q -O /grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.23/grpc_health_probe-linux-amd64
RUN chmod +x /grpc_health_probe

# Fase 2: Creazione dell'immagine finale minimale
FROM alpine:latest

COPY --from=builder /collector-service /collector-service
COPY --from=builder /grpc_health_probe /grpc_health_probe

EXPOSE 50051
CMD ["/collector-service"]