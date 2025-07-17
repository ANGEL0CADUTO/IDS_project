# --- Stage 1: Build ---
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY ../../go.mod ../../go.sum ./
RUN go mod download
COPY ../../ .
# Compiliamo l'eseguibile 'storage'
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/storage ./services/storage

# --- Stage 2: Final Image ---
FROM alpine:latest
WORKDIR /app
# Copiamo l'eseguibile compilato
COPY --from=builder /app/storage .
# Esponiamo la porta per questo servizio. Usiamo una porta diversa.
EXPOSE 50052
# Eseguiamo il binario 'storage'
CMD ["./storage"]