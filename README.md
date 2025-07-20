# Sistema di Rilevamento Intrusioni Distribuito (IDS)

Questo progetto è un'implementazione di un Intrusion Detection System (IDS) basato su un'architettura a microservizi, sviluppato per il corso di Sistemi Distribuiti e Cloud Computing. Il sistema è progettato per essere resiliente, scalabile e osservabile, utilizzando un moderno stack tecnologico incentrato su Go, gRPC e Docker.

## Architettura

Il sistema è composto da 4 microservizi principali, un service registry, un database time-series e uno stack di osservabilità completo.

- **Collector Service (Go):** Punto di ingresso (entrypoint) che riceve le metriche di rete dai client.
- **Analysis Service (Go):** Il cuore del sistema. Orchestra l'analisi delle metriche, interroga il modello di Machine Learning e implementa una logica di fallback tramite un Circuit Breaker.
- **Inference Service (Python):** Servizio gRPC specializzato che ospita un modello ML (Isolation Forest) per il rilevamento delle anomalie.
- **Storage Service (Go):** Unico punto di accesso al database, responsabile della persistenza di metriche e allarmi.

L'infrastruttura di supporto include:
- **Consul:** Per il Service Discovery e l'Health Checking.
- **InfluxDB:** Come database time-series per la memorizzazione dei dati.
- **Jaeger:** Per il Distributed Tracing e l'analisi delle performance.
- **Grafana:** Per la visualizzazione dei dati e il monitoraggio in tempo reale.

![Diagramma Architettura](path/to/your/architecture_diagram.png)
*(diagramma della architettura)*

## Pattern Implementati

- **Service Registry & Health Check (Consul)**
- **Circuit Breaker (in `analysis-service`)**
- **Client-Side Load Balancing (in `collector-service`)**
- **Distributed Tracing (OpenTelemetry & Jaeger)**
- **Externalized Configuration (Docker Compose)**
- **Container per Service (Docker)**

## Prerequisiti

Per eseguire il progetto, sono necessari i seguenti strumenti:
- [Docker](https://www.docker.com/products/docker-desktop/)
- [Docker Compose](https://docs.docker.com/compose/) (solitamente incluso in Docker Desktop)
- [Go](https://go.dev/doc/install) (versione 1.20 o superiore, necessario per il client di test)

## Guida all'Esecuzione

Questo progetto utilizza un `Makefile` per semplificare la gestione dell'applicazione. Aprire un terminale nella root del progetto ed eseguire i seguenti comandi.

### 1. Avvio Completo del Sistema

Questo comando costruisce le immagini Docker (se non esistono) e avvia tutti i servizi in background.

```bash
make up

**2. Generare Traffico di Test:**
Per popolare il sistema con dati, esegui il client di test. Il client simula 5 utenti concorrenti che inviano dati dal dataset NSL-KDD.
```bash
make test-client
```

**3. Monitorare il Sistema:**
Mentre il sistema è in esecuzione, puoi accedere alle seguenti interfacce web:

| Servizio | URL | Credenziali |
| :--- | :--- | :--- |
| **Grafana** | `http://localhost:3000` | `admin` / `admin` |
| **Jaeger** | `http://localhost:16686` | N/A |
| **Consul** | `http://localhost:8500` | N/A |

**4. Visualizzare i Log:**
Per vedere i log di tutti i servizi in tempo reale:
```bash
make logs
```

**5. Fermare l'Applicazione:**
Per fermare e rimuovere tutti i container:
```bash
make down
```

**6. Pulizia Completa (ATTENZIONE):**
Per fermare i container e rimuovere tutti i dati persistenti (database e configurazioni di Grafana):
```bash
make clean
```

## Test di Resilienza (Circuit Breaker)
Per testare la capacità del sistema di resistere a guasti:
1. Avvia il sistema con `make up` e genera traffico con `make test-client`.
2. Osserva la dashboard di Grafana.
3. Spegni il servizio di inferenza per simulare un guasto:
   ```bash
   docker compose stop inference
   ```
4. Osserva i log di `analysis-service` e la reazione della dashboard: il sistema continuerà a funzionare utilizzando la logica di fallback.
5. Riavvia il servizio per vedere l'auto-guarigione:
   ```bash
   docker compose start inference
   ```

## Autore
- **Angelo Romano**