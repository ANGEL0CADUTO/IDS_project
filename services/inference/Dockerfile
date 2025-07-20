FROM python:3.9-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Installa wget, scarica il probe per l'health check, lo rende eseguibile,
# e poi pulisce rimuovendo wget e la cache di apt per mantenere l'immagine leggera.
RUN apt-get update && \
    apt-get install -y --no-install-recommends wget && \
    wget -q -O /grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.23/grpc_health_probe-linux-amd64 && \
    chmod +x /grpc_health_probe && \
    apt-get purge -y --auto-remove wget && \
    rm -rf /var/lib/apt/lists/*

# Copia tutto il resto (app.py, modello, e i file _pb2.py generati)
COPY . .

# Esponi la porta per la comunicazione gRPC
EXPOSE 5000

CMD ["python", "app.py"]