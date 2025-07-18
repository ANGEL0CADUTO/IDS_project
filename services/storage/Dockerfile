# Fase 1: Build dell'eseguibile
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copia i file di gestione delle dipendenze, INCLUSO go.work
COPY go.mod go.sum ./
COPY go.work go.work.sum ./

COPY pkg ./pkg 

# Sincronizza le dipendenze di go.work
RUN go work sync
RUN go mod download

# Copia il codice sorgente necessario
COPY services/storage ./services/storage
COPY proto ./proto


# Build del binario dello storage
RUN CGO_ENABLED=0 GOOS=linux go build -o /storage-service ./services/storage
# Per risolvere race ocndition
RUN wget -q -O /grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.23/grpc_health_probe-linux-amd64
RUN chmod +x /grpc_health_probe


# Fase 2: Creazione dell'immagine finale minimale
FROM alpine:latest

# Copia solo il binario compilato
COPY --from=builder /storage-service /storage-service
COPY --from=builder /grpc_health_probe /grpc_health_probe

# Esponi la porta
EXPOSE 50052

# Comando per avviare il servizio
CMD ["/storage-service"]