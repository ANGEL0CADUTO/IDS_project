# Fase 1: Build dell'eseguibile
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copia i file di gestione delle dipendenze, INCLUSO go.work
COPY go.mod go.sum ./
COPY go.work go.work.sum ./

COPY pkg ./pkg 

# Sincronizza le dipendenze di go.work. È un passo FONDAMENTALE in un monorepo.
RUN go work sync
# Scarica le dipendenze
RUN go mod download

# Copia il codice sorgente necessario
COPY services/collector ./services/collector
COPY proto ./proto


# Build del binario del collector. Il path ora è relativo alla WORKDIR /app
RUN CGO_ENABLED=0 GOOS=linux go build -o /collector-service ./services/collector
# Per risolvere race condition
RUN wget -q -O /grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.23/grpc_health_probe-linux-amd64
RUN chmod +x /grpc_health_probe

# Fase 2: Creazione dell'immagine finale minimale
FROM alpine:latest

# Copia solo il binario compilato dalla fase di build
COPY --from=builder /collector-service /collector-service
COPY --from=builder /grpc_health_probe /grpc_health_probe

# Esponi la porta su cui il servizio gRPC è in ascolto
EXPOSE 50051

# Comando per avviare il servizio
CMD ["/collector-service"]