# ==============================================================================
# Makefile per il Progetto IDS
#
# Offre comandi semplici per gestire l'intero ciclo di vita dell'applicazione,
# sia in locale che su un'istanza AWS EC2.
# ==============================================================================

# --- CONFIGURAZIONE AWS ---
# Modifica queste variabili con i dati della tua istanza EC2.
AWS_KEY  ?= C:/Users/aroma/.ssh/ids-project-key.pem
AWS_USER ?= ubuntu
AWS_HOST ?= 44.206.235.59
# --------------------

# Alias per il comando SSH
AWS_SSH := ssh -i $(AWS_KEY) -t $(AWS_USER)@$(AWS_HOST)

# Variabile per specificare un singolo servizio nei comandi up/down/stop
SERVICE ?=

.PHONY: all help build up down logs test test-unit test-system test-client-benign test-client-malicious clean clean-all clean-influx clean-grafana aws-help aws-setup aws-deploy aws-up aws-down aws-logs aws-clean-all aws-clean-influx

# ==============================================================================
# Sezione di Aiuto
# ==============================================================================

all: help

help:
	@echo ""
	@echo "--- Comandi di Gestione Locale ---"
	@echo "  make up                        -> Avvia tutti i servizi in locale."
	@echo "  make down                      -> Ferma e rimuove i container in locale."
	@echo "  make logs                      -> Mostra i log dei servizi locali."
	@echo "  make test-client-benign        -> Lancia il client benigno verso localhost."
	@echo ""
	@echo "--- Comandi di Testing Locale ---"
	@echo "  make test                      -> Esegue TUTTI i test."
	@echo ""
	@echo "--- Comandi di Pulizia Locale ---"
	@echo "  make clean                     -> Ferma e rimuove i container e le reti (non i volumi)."
	@echo "  make clean-influx              -> Pulisce SOLO i dati di InfluxDB in locale."
	@echo "  make clean-grafana             -> Pulisce SOLO i dati di Grafana in locale."
	@echo "  make clean-all                 -> Pulisce TUTTO in locale, inclusi TUTTI i volumi."
	@echo ""
	@echo "--- Comandi di Gestione Remota (AWS) ---"
	@echo "  make aws-help                  -> Mostra aiuto specifico per i comandi AWS."
	@echo ""

aws-help:
	@echo "--- Comandi di Gestione Remota (AWS EC2) ---"
	@echo "Assicurati di aver configurato le variabili AWS_* all'inizio del Makefile."
	@echo ""
	@echo "  make aws-setup                 -> Esegue il setup iniziale sull'istanza EC2 (una sola volta)."
	@echo "  make aws-deploy                -> Clona/aggiorna il repo e avvia tutti i servizi su EC2."
	@echo "  make aws-up SERVICE=<nome>     -> Avvia un servizio specifico su EC2 (es. 'inference')."
	@echo "  make aws-down SERVICE=<nome>   -> Ferma un servizio specifico su EC2."
	@echo "  make aws-logs                  -> Mostra i log in tempo reale da EC2."
	@echo "  make aws-clean-influx          -> Pulisce SOLO i dati di InfluxDB su EC2."
	@echo "  make aws-clean-all             -> Pulisce completamente l'ambiente su EC2."
	@echo "  make aws-test-client-benign    -> Lancia il client benigno verso l'IP di AWS."
	@echo ""


# ==============================================================================
# Comandi Locali
# ==============================================================================

build:
	@echo "-> (Locale) Costruzione delle immagini Docker..."
	docker compose build --no-cache

up: build
	@echo "-> (Locale) Avvio di tutti i servizi in background..."
	docker compose up -d

down:
	@echo "-> (Locale) Fermo e rimozione dei container..."
	docker compose down

clean: down

logs:
	@echo "-> (Locale) Visualizzazione dei log..."
	docker compose logs -f

test-client-benign:
	@echo "-> (Locale) Esecuzione del client in modalità BENIGNA..."
	go run ./cmd/test-client/main.go -mode=benign -addr=localhost:50051

test-client-malicious:
	@echo "-> (Locale) Esecuzione del client in modalità MALEVOLA..."
	go run ./cmd/test-client/main.go -mode=malicious -addr=localhost:50051

test:
	@echo "-> (Locale) Esecuzione di tutti i test..."
	go test -v -count=1 ./...

test-unit:
	@echo "-> (Locale) Esecuzione dei test unitari..."
	go test -v -count=1 ./cmd/test-client ./services/analysis

test-system:
	@echo "-> (Locale) Esecuzione dei test di sistema (end-to-end)..."
	go test -v -count=1 ./tests

clean-all:
	@echo "-> (Locale) ATTENZIONE: Fermo dei container e RIMOZIONE di tutti i volumi..."
	docker compose down -v

clean-influx:
	@echo "-> (Locale) Fermo dei container e rimozione del volume di InfluxDB..."
	docker compose down
	docker volume rm ids-project_influxdb-data || true

clean-grafana:
	@echo "-> (Locale) Fermo dei container e rimozione del volume di Grafana..."
	docker compose down
	docker volume rm ids-project_grafana-data || true


# ==============================================================================
# Comandi Remoti (AWS)
# ==============================================================================

aws-setup:
	@echo "-> (AWS) Esecuzione del setup iniziale su $(AWS_HOST)..."
	@$(AWS_SSH) 'sudo apt-get update -y && sudo apt-get install -y curl git; \
	sudo apt-get remove docker docker-engine docker.io containerd runc -y; \
	curl -fsSL https://get.docker.com | sudo sh; \
	sudo usermod -aG docker $(AWS_USER); \
	sudo apt-get install -y docker-compose-plugin; \
	echo "====== SETUP COMPLETATO SU AWS ======"; \
	echo "Per favore, esci e riconnettiti manualmente UNA VOLTA per applicare i permessi di Docker."'

aws-deploy:
	@echo "-> (AWS) Deploy/Aggiornamento del progetto su $(AWS_HOST)..."
	@$(AWS_SSH) 'if [ -d "IDS_project" ]; then cd IDS_project && git pull; else git clone https://github.com/ANGEL0CADUTO/IDS_project.git && cd IDS_project; fi; make up'

# --- COMANDI MODIFICATI E NUOVI ---

# Ferma i servizi su AWS. Se SERVICE è specificato, ferma solo quello.
aws-down:
	@echo "-> (AWS) Fermo dei servizi su $(AWS_HOST)..."
	@$(AWS_SSH) 'cd IDS_project && docker compose stop $(SERVICE)'

# Avvia i servizi su AWS. Se SERVICE è specificato, avvia solo quello.
aws-up:
	@echo "-> (AWS) Avvio dei servizi su $(AWS_HOST)..."
	@$(AWS_SSH) 'cd IDS_project && docker compose up -d $(SERVICE)'

aws-logs:
	@echo "-> (AWS) Visualizzazione dei log da $(AWS_HOST)..."
	@$(AWS_SSH) 'cd IDS_project && make logs'

aws-clean-all:
	@echo "-> (AWS) Pulizia completa su $(AWS_HOST)..."
	@$(AWS_SSH) 'cd IDS_project && make clean-all'

aws-clean-influx:
	@echo "-> (AWS) Pulizia dei dati di InfluxDB su $(AWS_HOST)..."
	@$(AWS_SSH) 'cd IDS_project && make clean-influx'

aws-test-client-benign:
	@echo "-> (AWS) Esecuzione del client in modalità BENIGNA verso $(AWS_HOST)..."
	go run ./cmd/test-client/main.go -mode=benign -addr=$(AWS_HOST):50051

aws-test-client-malicious:
	@echo "-> (AWS) Esecuzione del client in modalità MALEVOLA verso $(AWS_HOST)..."
	go run ./cmd/test-client/main.go -mode=malicious -addr=$(AWS_HOST):50051