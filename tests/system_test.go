package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Questi parametri devono corrispondere a quelli nel docker-compose.yml
const (
	collectorAddress = "localhost:50051"
	influxURL        = "http://localhost:8086"
	influxToken      = "password123" // Questo è il token di admin
	influxOrg        = "ids-project"
	influxBucket     = "metrics"
)

func TestSystem_HappyPath_MetricIsStored(t *testing.T) {
	// Questo test si aspetta che l'intero stack sia in esecuzione (via docker compose up)

	// Fase 1: Setup - Connessione al Collector Service
	conn, err := grpc.Dial(collectorAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	require.NoError(t, err, "Il test di integrazione richiede che il Collector Service sia in esecuzione su %s", collectorAddress)
	defer conn.Close()
	collectorClient := pb.NewMetricsCollectorClient(conn)

	// Fase 2: Azione - Invia una metrica unica per poterla ritrovare
	uniqueClientID := fmt.Sprintf("integration-test-%d", time.Now().UnixNano())
	testMetric := &pb.Metric{
		SourceClientId: uniqueClientID,
		Type:           "network_traffic",
		Timestamp:      time.Now().Unix(),
		// Usiamo un set minimo di features per non dipendere dal modello ML
		// e assicurarci che venga classificata come normale
		Features: make([]float32, 41),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = collectorClient.SendMetric(ctx, testMetric)
	require.NoError(t, err, "La chiamata SendMetric non doveva fallire")

	// Diamo al sistema un secondo per processare e scrivere il dato
	time.Sleep(2 * time.Second)

	// Fase 3: Verifica - Controlliamo direttamente su InfluxDB
	influxClient := influxdb2.NewClient(influxURL, influxToken)
	queryAPI := influxClient.QueryAPI(influxOrg)

	// Creiamo una query Flux per trovare esattamente la nostra metrica
	query := fmt.Sprintf(`
		from(bucket: "%s")
		|> range(start: -1m)
		|> filter(fn: (r) => r._measurement == "network_traffic")
		|> filter(fn: (r) => r.client_id == "%s")
		|> count()
	`, influxBucket, uniqueClientID)

	result, err := queryAPI.Query(context.Background(), query)
	require.NoError(t, err, "La query su InfluxDB non doveva fallire")

	// Verifichiamo il risultato
	found := false
	for result.Next() {
		if result.Record().Value().(int64) > 0 {
			found = true
		}
	}

	assert.True(t, found, "La metrica inviata con client_id %s non è stata trovata in InfluxDB", uniqueClientID)

	influxClient.Close()
}

// in tests/system_test.go

// TestSystem_AnomalyPath_AlarmIsStored verifica che una metrica anomala
// generi un allarme nel bucket corretto.
func TestSystem_AnomalyPath_AlarmIsStored(t *testing.T) {
	// Fase 1: Setup - Connessioni a Collector e InfluxDB
	conn, err := grpc.Dial(collectorAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	require.NoError(t, err, "Collector Service deve essere in esecuzione")
	defer conn.Close()
	collectorClient := pb.NewMetricsCollectorClient(conn)

	influxClient := influxdb2.NewClient(influxURL, influxToken)
	defer influxClient.Close()
	queryAPI := influxClient.QueryAPI(influxOrg)

	// Il bucket degli allarmi
	const influxAlarmsBucket = "alarms"

	// Fase 2: Azione - Invia una metrica che è quasi certamente un'anomalia
	// Il dataset NSL-KDD ha molti attacchi di tipo "neptune" (Denial of Service)
	// che sono facilmente riconoscibili dal modello. Usiamo i valori di un record "neptune".
	uniqueClientID := fmt.Sprintf("integration-test-anomaly-%d", time.Now().UnixNano())
	// Valori presi da un tipico record di attacco 'neptune'
	anomalousFeatures := []float32{0, 1, 19, 5, 0, 0, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 240, 19, 1, 1, 0.08, 0.08, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	testMetric := &pb.Metric{
		SourceClientId: uniqueClientID,
		Type:           "network_traffic",
		Timestamp:      time.Now().Unix(),
		Features:       anomalousFeatures,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := collectorClient.SendMetric(ctx, testMetric)
	require.NoError(t, err, "La chiamata SendMetric non doveva fallire")
	// Verifichiamo che la risposta indichi un'anomalia
	assert.Contains(t, resp.Message, "Anomaly detected", "La risposta del collector doveva indicare un'anomalia")

	// Diamo al sistema il tempo di processare
	time.Sleep(2 * time.Second)

	// Fase 3: Verifica - Controlliamo il bucket 'alarms' su InfluxDB
	query := fmt.Sprintf(`
		from(bucket: "%s")
		|> range(start: -1m)
		|> filter(fn: (r) => r._measurement == "alarm")
		|> filter(fn: (r) => r.client_id == "%s")
		|> count()
	`, influxAlarmsBucket, uniqueClientID)

	result, err := queryAPI.Query(context.Background(), query)
	require.NoError(t, err, "La query sul bucket 'alarms' non doveva fallire")

	found := false
	for result.Next() {
		if result.Record().Value().(int64) > 0 {
			found = true
		}
	}

	assert.True(t, found, "Un allarme per il client_id %s doveva essere presente nel bucket 'alarms', ma non è stato trovato", uniqueClientID)
}

// in tests/system_test.go

// TestSystem_FaultTolerance_FallbackIsTriggered simula un guasto al servizio di inferenza
// e verifica che il sistema continui a funzionare usando la logica di fallback.
func TestSystem_FaultTolerance_FallbackIsTriggered(t *testing.T) {
	// Questo test è più complesso perché deve interagire con Docker.
	// Per semplicità, lo implementiamo come una sequenza di controlli manuali
	// documentati, ma un test automatizzato userebbe la Docker SDK.
	// Per ora, lo strutturiamo per essere eseguito manualmente.
	t.Skip("Questo test richiede l'interazione manuale con Docker Compose. Eseguire i seguenti passaggi:\n" +
		"1. Avviare lo stack con 'docker compose up'.\n" +
		"2. Eseguire 'docker compose stop inference'.\n" +
		"3. Eseguire il test 'TestSystem_AnomalyPath_FallbackAlarmIsStored' (vedi sotto).\n" +
		"4. Eseguire 'docker compose start inference' per ripristinare.")

	// Poiché non possiamo automatizzare lo stop/start facilmente da qui,
	// creiamo un test separato da eseguire MENTRE inference è offline.
}

// TestSystem_AnomalyPath_FallbackAlarmIsStored è un test da eseguire
// manualmente dopo aver fermato il container 'inference'.
func TestSystem_AnomalyPath_FallbackAlarmIsStored(t *testing.T) {
	t.Log("Esecuzione del test di fallback. Assicurarsi che 'inference-service' sia OFFLINE.")

	// Fase 1: Setup
	conn, err := grpc.Dial(collectorAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	require.NoError(t, err, "Collector Service deve essere in esecuzione")
	defer conn.Close()
	collectorClient := pb.NewMetricsCollectorClient(conn)

	influxClient := influxdb2.NewClient(influxURL, influxToken)
	defer influxClient.Close()
	queryAPI := influxClient.QueryAPI(influxOrg)
	const influxAlarmsBucket = "alarms"

	// Fase 2: Azione - Invia una metrica che attiverà il fallback
	uniqueClientID := fmt.Sprintf("integration-test-fallback-%d", time.Now().UnixNano())
	// Usiamo valori alti per src_bytes per essere sicuri di superare la soglia di fallback (> 95.0)
	fallbackFeatures := make([]float32, 41)
	fallbackFeatures[4] = 150.0

	testMetric := &pb.Metric{
		SourceClientId: uniqueClientID,
		Type:           "network_traffic",
		Timestamp:      time.Now().Unix(),
		Features:       fallbackFeatures,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := collectorClient.SendMetric(ctx, testMetric)
	require.NoError(t, err, "La chiamata SendMetric non doveva fallire anche con inference offline")
	assert.Contains(t, resp.Message, "Threshold (Fallback)", "La risposta doveva indicare che il fallback è stato usato")

	time.Sleep(2 * time.Second)

	// Fase 3: Verifica - Controlliamo che l'allarme di fallback sia nel bucket corretto
	query := fmt.Sprintf(`
		from(bucket: "%s")
		|> range(start: -1m)
		|> filter(fn: (r) => r._measurement == "alarm")
		|> filter(fn: (r) => r.client_id == "%s")
		|> filter(fn: (r) => r.rule_id == "anomaly_by_threshold_(fallback)")
		|> count()
	`, influxAlarmsBucket, uniqueClientID)

	result, err := queryAPI.Query(context.Background(), query)
	require.NoError(t, err, "La query sul bucket 'alarms' non doveva fallire")

	found := false
	for result.Next() {
		if result.Record().Value().(int64) > 0 {
			found = true
		}
	}

	assert.True(t, found, "Un allarme di fallback per il client_id %s doveva essere presente, ma non è stato trovato", uniqueClientID)
}
