package tests

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	collectorAddress  = getEnv("COLLECTOR_ADDR", "localhost:50051")
	influxURL         = getEnv("INFLUXDB_URL", "http://localhost:8086")
	influxToken       = getEnv("INFLUXDB_TOKEN", "password123")
	influxOrg         = getEnv("INFLUXDB_ORG", "ids-project")
	metricsBucket     = "metrics"
	alarmsBucket      = "alarms"
	alarmThreshold, _ = strconv.Atoi(getEnv("ALARM_THRESHOLD", "4"))
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func connectToCollector(t *testing.T) (pb.MetricsCollectorClient, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	conn, err := grpc.DialContext(ctx, collectorAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(t, err, "Il test di sistema richiede che il Collector Service sia raggiungibile su %s", collectorAddress)
	cleanup := func() {
		conn.Close()
		cancel()
	}
	return pb.NewMetricsCollectorClient(conn), cleanup
}

func TestSystem_HappyPath_MetricIsStored(t *testing.T) {
	collectorClient, cleanup := connectToCollector(t)
	defer cleanup()

	uniqueClientID := fmt.Sprintf("integration-test-normal-%d", time.Now().UnixNano())
	testMetric := &pb.Metric{
		SourceClientId: uniqueClientID,
		Type:           "network_traffic",
		Timestamp:      time.Now().Unix(),
		Features:       make([]float32, 41),
	}

	_, err := collectorClient.SendMetric(context.Background(), testMetric)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	influxClient := influxdb2.NewClient(influxURL, influxToken)
	defer influxClient.Close()
	queryAPI := influxClient.QueryAPI(influxOrg)

	query := fmt.Sprintf(`from(bucket: "%s") |> range(start: -1m) |> filter(fn: (r) => r._measurement == "network_traffic" and r.client_id == "%s") |> count()`, metricsBucket, uniqueClientID)
	result, err := queryAPI.Query(context.Background(), query)
	require.NoError(t, err)

	found := false
	if result.Next() && result.Record().Value() != nil && result.Record().Value().(int64) > 0 {
		found = true
	}
	assert.True(t, found, "La metrica inviata non è stata trovata in InfluxDB")
}

func TestSystem_AnomalyPath_AlarmIsStoredAfterCorrelation(t *testing.T) {
	collectorClient, cleanup := connectToCollector(t)
	defer cleanup()

	influxClient := influxdb2.NewClient(influxURL, influxToken)
	defer influxClient.Close()
	queryAPI := influxClient.QueryAPI(influxOrg)

	uniqueClientID := fmt.Sprintf("integration-test-anomaly-%d", time.Now().UnixNano())
	anomalousFeatures := []float32{0, 1, 19, 5, 0, 0, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 240, 19, 1, 1, 0.08, 0.08, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	testMetric := &pb.Metric{
		SourceClientId: uniqueClientID,
		Type:           "network_traffic",
		Timestamp:      time.Now().Unix(),
		Features:       anomalousFeatures,
	}

	t.Logf("Invio di %d metriche sospette (sotto la soglia)...", alarmThreshold-1)
	for i := 0; i < alarmThreshold-1; i++ {
		resp, err := collectorClient.SendMetric(context.Background(), testMetric)
		require.NoError(t, err)
		assert.Contains(t, resp.Message, "Suspicious metric recorded", "La risposta doveva indicare metrica sospetta")
	}

	query := fmt.Sprintf(`from(bucket: "%s") |> range(start: -1m) |> filter(fn: (r) => r.client_id == "%s") |> count()`, alarmsBucket, uniqueClientID)
	result, err := queryAPI.Query(context.Background(), query)
	require.NoError(t, err)
	found := false
	if result.Next() && result.Record().Value() != nil && result.Record().Value().(int64) > 0 {
		found = true
	}
	assert.False(t, found, "NON doveva esserci un allarme prima del superamento della soglia")

	t.Logf("Invio dell'ultima metrica per superare la soglia di %d...", alarmThreshold)
	resp, err := collectorClient.SendMetric(context.Background(), testMetric)
	require.NoError(t, err)

	// --- CORREZIONE QUI ---
	// Il messaggio di successo ora è "Correlated anomaly detected by ML Model and stored"
	assert.Contains(t, resp.Message, "Correlated anomaly detected by ML Model", "La risposta doveva indicare un allarme correlato")

	time.Sleep(2 * time.Second)

	result, err = queryAPI.Query(context.Background(), query)
	require.NoError(t, err)
	found = false
	if result.Next() && result.Record().Value() != nil && result.Record().Value().(int64) > 0 {
		found = true
	}
	assert.True(t, found, "Un allarme doveva essere presente dopo aver superato la soglia")
}

func TestSystem_FaultTolerance_FallbackIsTriggeredAndRecovers(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err, "Impossibile creare il client Docker")
	inferenceContainerName := "ids-project-inference-1"

	t.Run("Fallback Logic Activation", func(t *testing.T) {
		t.Logf("--- Arresto del container '%s' per simulare un guasto...", inferenceContainerName)
		err := cli.ContainerStop(context.Background(), inferenceContainerName, container.StopOptions{})
		require.NoError(t, err, "Impossibile fermare il container di inferenza")
		time.Sleep(5 * time.Second)
		collectorClient, cleanup := connectToCollector(t)
		defer cleanup()
		uniqueClientID := fmt.Sprintf("integration-test-fallback-%d", time.Now().UnixNano())
		fallbackFeatures := make([]float32, 41)
		fallbackFeatures[4] = 150.0
		testMetric := &pb.Metric{
			SourceClientId: uniqueClientID,
			Type:           "network_traffic",
			Timestamp:      time.Now().Unix(),
			Features:       fallbackFeatures,
			Value:          150.0,
		}
		t.Logf("Invio di %d richieste anomale durante il guasto...", alarmThreshold)
		for i := 0; i < alarmThreshold; i++ {
			resp, err := collectorClient.SendMetric(context.Background(), testMetric)
			require.NoError(t, err)
			if i < alarmThreshold-1 {
				assert.Contains(t, resp.Message, "Suspicious metric recorded", "La risposta doveva essere 'sospetta'")
			} else {
				// --- CORREZIONE QUI ---
				assert.Contains(t, resp.Message, "Correlated anomaly detected by Threshold (Fallback)", "L'ultima risposta doveva essere un allarme correlato di fallback")
			}
		}
	})

	t.Run("System Auto-Recovery", func(t *testing.T) {
		t.Logf("--- Riavvio del container '%s' per simulare il recupero...", inferenceContainerName)
		err := cli.ContainerStart(context.Background(), inferenceContainerName, container.StartOptions{})
		require.NoError(t, err, "Impossibile riavviare il container di inferenza")
		t.Logf("In attesa del recupero del Circuit Breaker (circa 35s)...")
		time.Sleep(35 * time.Second)
		collectorClient, cleanup := connectToCollector(t)
		defer cleanup()
		anomalousFeatures := []float32{0, 1, 19, 5, 0, 0, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 240, 19, 1, 1, 0.08, 0.08, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
		testMetric := &pb.Metric{
			SourceClientId: "test-client-recovery",
			Type:           "network_traffic",
			Features:       anomalousFeatures,
		}
		var finalResp *pb.CollectorResponse
		for i := 0; i < alarmThreshold; i++ {
			finalResp, err = collectorClient.SendMetric(context.Background(), testMetric)
			require.NoError(t, err)
		}
		// --- CORREZIONE QUI ---
		assert.Contains(t, finalResp.Message, "Correlated anomaly detected by ML Model", "Il sistema doveva riprendersi e usare di nuovo il modello ML")
		t.Log("Recupero automatico verificato con successo!")
	})
}
