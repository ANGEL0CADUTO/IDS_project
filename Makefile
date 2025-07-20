# ==============================================================================
# Makefile per il Progetto IDS
#
# Offre comandi semplici per gestire l'intero ciclo di vita dell'applicazione.
# ==============================================================================

# Evita conflitti con file che potrebbero avere lo stesso nome dei target.
.PHONY: all help build up down logs test-client clean clean-grafana

# Target di default: eseguito quando si lancia 'make' senza argomenti.
# Mostra un messaggio di aiuto.
all: help

help:
	@echo ""
	@echo "--- Comandi Disponibili per il Progetto IDS ---"
	@echo ""
	@echo "  make build          -> Costruisce (o ricostruisce) le immagini Docker di tutti i servizi."
	@echo "  make up             -> Avvia tutti i servizi in background."
	@echo "  make down           -> Ferma e rimuove i container dei servizi."
	@echo "  make logs           -> Mostra i log di tutti i servizi in tempo reale (Ctrl+C per uscire)."
	@echo "  make test-client    -> Esegue il client di test per generare traffico di rete."
	@echo "  make clean          -> ATTENZIONE: Ferma tutto e RIMUOVE i volumi dei dati (InfluxDB, Grafana)."
	@echo "  make clean-influx   -> Ferma tutto e rimuove SOLO i dati di InfluxDB, preservando Grafana."
	@echo ""

# Costruisce le immagini Docker. --no-cache assicura che vengano usati i file più recenti.
build:
	@echo "-> Costruzione delle immagini Docker..."
	docker compose build --no-cache

# Avvia l'intera infrastruttura in modalità detached (background).
# Dipende da 'build', quindi costruirà le immagini se non esistono.
up: build
	@echo "-> Avvio di tutti i servizi in background..."
	docker compose up -d

# Ferma e rimuove i container. Non tocca i volumi.
down:
	@echo "-> Fermo e rimozione dei container..."
	docker compose down

# Mostra i log di tutti i servizi in modalità "follow".
logs:
	@echo "-> Visualizzazione dei log in tempo reale (premere Ctrl+C per uscire)..."
	docker compose logs -f

# Esegue lo script del client di test.
test-client:
	@echo "-> Esecuzione del client di test per generare traffico..."
	go run ./cmd/test-client/main.go

# Pulisce completamente l'ambiente, inclusi i dati persistenti.
# Utile per un reset completo.
clean:
	@echo "-> ATTENZIONE: Fermo dei container e RIMOZIONE di tutti i volumi di dati..."
	docker compose down -v

# Pulisce solo i dati di InfluxDB, utile per rieseguire i test senza perdere le dashboard.
clean-influx:
	@echo "-> Fermo dei container e rimozione del volume di InfluxDB..."
	docker compose down
	docker volume rm ids-project_influxdb-data || true