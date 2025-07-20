# Fase 1: Build dell'eseguibile
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copia l'intero contesto del progetto
COPY . .

# Esegui i comandi Go
RUN go work sync
RUN go mod download

# Build del binario di analysis
RUN CGO_ENABLED=0 GOOS=linux go build -o /analysis-service ./services/analysis

# Scarica grpc_health_probe
RUN wget -q -O /grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.23/grpc_health_probe-linux-amd64
RUN chmod +x /grpc_health_probe

# Fase 2: Immagine Finale
FROM alpine:latest

COPY --from=builder /analysis-service /analysis-service
COPY --from=builder /grpc_health_probe /grpc_health_probe

WORKDIR /
EXPOSE 50053
CMD ["/analysis-service"]