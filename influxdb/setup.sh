#!/bin/sh
# setup.sh

# Questo script viene eseguito da InfluxDB al primo avvio.
# Userà le variabili d'ambiente fornite da docker-compose per configurare i bucket.

set -e

# Crea il bucket per le metriche
influx bucket create \
  --name "${DOCKER_INFLUXDB_INIT_BUCKET}" \
  --org "${DOCKER_INFLUXDB_INIT_ORG}" \
  --retention 0

echo "Bucket '${DOCKER_INFLUXDB_INIT_BUCKET}' creato."

# Crea il bucket per gli allarmi
influx bucket create \
  --name "${INFLUXDB_ALARMS_BUCKET_NAME}" \
  --org "${DOCKER_INFLUXDB_INIT_ORG}" \
  --retention 0

echo "Bucket '${INFLUXDB_ALARMS_BUCKET_NAME}' creato."

echo "Setup di InfluxDB completato."