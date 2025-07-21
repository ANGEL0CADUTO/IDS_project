# ==============================================================================
# Makefile per il Progetto IDS
#
# Offre comandi semplici per gestire l'intero ciclo di vita dell'applicazione.
# ==============================================================================

# Evita conflitti con file che potrebbero avere lo stesso nome dei target.
.PHONY: all help build up down logs test test-client test-analysis test-system test-client-benign test-client-malicious clean clean-all clean-influx

# Target di default: eseguito quando si lancia 'make' senza argomenti.
all: help

help:
	@echo ""
	@echo "--- Comandi di Gestione Applicazione ---"
	@echo "  make build                  -> Costruisce (o ricostruisce) le immagini Docker di tutti i servizi."
	@echo "  make up                     -> Avvia tutti i servizi in background."
	@echo "  make down                   -> Ferma e rimuove i container dei servizi."
	@echo "  make logs                   -> Mostra i log di tutti i servizi in tempo reale (Ctrl+C per uscire)."
	@echo ""
	@echo "--- Comandi di Generazione Traffico ---"
	@echo "  make test-client-benign     -> Esegue il client in modalità BENIGNA (solo traffico normale)."
	@echo "  make test-client-malicious  -> Esegue il client in modalità MALEVOLA (solo traffico di attacco)."
	@echo ""
	@echo "--- Comandi di Testing ---"
	@echo "  make test                   -> Esegue TUTTI i test (unitari, integrazione, sistema)."
	@echo "  make test-client            -> Esegue solo i test per il data generator (cmd/test-client)."
	@echo "  make test-analysis          -> Esegue solo i test per l'analysis service."
	@echo "  make test-system            -> Esegue solo i test di sistema end-to-end."
	@echo ""
	@echo "--- Comandi di Pulizia ---"
	@echo "  make clean                  -> Ferma e rimuove i container e le reti."
	@echo "  make clean-influx           -> Ferma tutto e rimuove SOLO i dati di InfluxDB."
	@echo "  make clean-all              -> ATTENZIONE: Ferma tutto e RIMUOVE TUTTI i volumi (InfluxDB, Grafana)."
	@echo ""


# ==============================================================================
# Implementazione dei Comandi
# ==============================================================================

build:
	@echo "-> Costruzione delle immagini Docker..."
	docker compose build --no-cache

up: build
	@echo "-> Avvio di tutti i servizi in background..."
	docker compose up -d

down:
	@echo "-> Fermo e rimozione dei container..."
	docker compose down

clean: down

logs:
	@echo "-> Visualizzazione dei log in tempo reale (premere Ctrl+C per uscire)..."
	docker compose logs -f

test-client-benign:
	@echo "-> Esecuzione del client in modalità BENIGNA..."
	go run ./cmd/test-client/main.go -mode=benign

test-client-malicious:
	@echo "-> Esecuzione del client in modalità MALEVOLA..."
	go run ./cmd/test-client/main.go -mode=malicious

# --- NUOVA SEZIONE: TESTING ---

# Esegue tutti i test del progetto in modalità verbosa.
# -count=1 disabilita la cache dei test per assicurare una nuova esecuzione.
# ./... è il wildcard di Go per dire "in questa cartella e in tutte le sottocartelle".
test:
	@echo "-> Esecuzione di tutti i test (unitari, integrazione, sistema)..."
	go test -v -count=1 ./...

# Esegue i test specifici per ogni componente.
test-client:
	@echo "-> Esecuzione dei test per il package cmd/test-client..."
	go test -v -count=1 ./cmd/test-client

test-analysis:
	@echo "-> Esecuzione dei test per il package services/analysis..."
	go test -v -count=1 ./services/analysis

test-system:
	@echo "-> Esecuzione dei test di sistema (end-to-end)..."
	go test -v -count=1 ./tests

# --- SEZIONE PULIZIA ---

clean-all:
	@echo "-> ATTENZIONE: Fermo dei container e RIMOZIONE di tutti i volumi di dati..."
	docker compose down -v

clean-influx:
	@echo "-> Fermo dei container e rimozione del volume di InfluxDB..."
	docker compose down
	docker volume rm ids-project_influxdb-data || true