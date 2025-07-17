# --- Stage 1: Build ---
# Usiamo un'immagine Go ufficiale per compilare l'applicazione.
FROM golang:1.22-alpine AS builder

# Impostiamo la directory di lavoro all'interno del container.
WORKDIR /app

# Copiamo i file di gestione delle dipendenze per primi.
# Questo sfrutta la cache di Docker: se i file non cambiano, questo layer non viene rieseguito.
COPY ../../go.mod ../../go.sum ./
RUN go mod download

# Copiamo tutto il codice sorgente del progetto.
COPY ../../ .

# Compiliamo l'applicazione Collector.
# CGO_ENABLED=0 crea un binario statico, GOOS=linux lo rende compatibile con l'ambiente Alpine.
# L'output è un singolo file eseguibile chiamato 'collector' in /app.
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/collector ./services/collector

# --- Stage 2: Final Image ---
# Partiamo da un'immagine Alpine pulita, molto leggera.
FROM alpine:latest

WORKDIR /app

# Copiamo solo il file eseguibile compilato dallo stage 'builder'.
COPY --from=builder /app/collector .

# Esponiamo la porta su cui il nostro server gRPC è in ascolto.
EXPOSE 50051

# Il comando che verrà eseguito all'avvio del container.
CMD ["./collector"]