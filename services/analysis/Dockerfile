# Fase 1: Build dell'eseguibile
FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
COPY go.work go.work.sum ./
COPY pkg ./pkg
COPY proto ./proto
COPY services/analysis ./services/analysis

RUN go work sync
RUN go mod download

# Compilazione standard, senza CGO
RUN CGO_ENABLED=0 GOOS=linux go build -o /analysis-service ./services/analysis

# Scarichiamo il probe per l'health check
RUN wget -q -O /grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.23/grpc_health_probe-linux-amd64
RUN chmod +x /grpc_health_probe

# Fase 2: Immagine Finale
FROM alpine:latest

# Copiamo solo il nostro servizio e l'health probe
COPY --from=builder /analysis-service /analysis-service
COPY --from=builder /grpc_health_probe /grpc_health_probe

WORKDIR /

EXPOSE 50053

CMD ["/analysis-service"]