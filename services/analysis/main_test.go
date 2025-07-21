package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	"github.com/sony/gobreaker"
	"google.golang.org/grpc"
)

type mockStorageClient struct {
	pb.StorageClient
	storeMetricCalledCount int
	storeAlarmCalledCount  int
	lastAlarm              *pb.Alarm
}

func (m *mockStorageClient) StoreMetric(ctx context.Context, in *pb.Metric, opts ...grpc.CallOption) (*pb.StorageResponse, error) {
	m.storeMetricCalledCount++
	return &pb.StorageResponse{Success: true}, nil
}

func (m *mockStorageClient) StoreAlarm(ctx context.Context, in *pb.Alarm, opts ...grpc.CallOption) (*pb.StorageResponse, error) {
	m.storeAlarmCalledCount++
	m.lastAlarm = in
	return &pb.StorageResponse{Success: true}, nil
}

type mockInferenceClient struct {
	pb.InferenceClient
	prediction int32
}

func (m *mockInferenceClient) Predict(ctx context.Context, in *pb.InferenceRequest, opts ...grpc.CallOption) (*pb.InferenceResponse, error) {
	if m.prediction == 0 { // Usiamo 0 per indicare un fallimento
		return nil, errors.New("simulated inference failure")
	}
	return &pb.InferenceResponse{Prediction: m.prediction}, nil
}

// Test per una metrica normale che deve essere salvata correttamente.
func TestAnalyzeMetric_NormalMetric(t *testing.T) {
	// Setup
	mockStore := &mockStorageClient{}
	mockInference := &mockInferenceClient{prediction: 1} // 1 = Normale

	analysisServer := &server{
		storageClient:     mockStore,
		inferenceClient:   mockInference,
		circuitBreaker:    gobreaker.NewCircuitBreaker(gobreaker.Settings{}),
		suspiciousClients: make(map[string][]time.Time), // Inizializziamo la mappa!
		mu:                sync.Mutex{},
	}

	normalMetric := &pb.Metric{Features: make([]float32, 41)}

	// Esecuzione
	_, err := analysisServer.AnalyzeMetric(context.Background(), normalMetric)

	// Verifica
	if err != nil {
		t.Fatalf("Errore inatteso: %v", err)
	}
	if mockStore.storeMetricCalledCount != 1 {
		t.Errorf("StoreMetric doveva essere chiamato 1 volta, ma è stato chiamato %d volte", mockStore.storeMetricCalledCount)
	}
	if mockStore.storeAlarmCalledCount != 0 {
		t.Errorf("StoreAlarm NON doveva essere chiamato")
	}
}

// Test per verificare che un allarme scatti solo dopo aver superato la soglia.
func TestAnalyzeMetric_TriggersAlarm_AfterThreshold(t *testing.T) {
	// Setup
	mockStore := &mockStorageClient{}
	mockInference := &mockInferenceClient{prediction: -1} // -1 = Anomalia

	analysisServer := &server{
		storageClient:     mockStore,
		inferenceClient:   mockInference,
		circuitBreaker:    gobreaker.NewCircuitBreaker(gobreaker.Settings{}),
		suspiciousClients: make(map[string][]time.Time), // Inizializziamo la mappa!
		mu:                sync.Mutex{},
	}

	anomalousMetric := &pb.Metric{SourceClientId: "test-client", Features: make([]float32, 41)}

	// Esecuzione: Simuliamo 3 chiamate anomale.
	// Le prime 2 dovrebbero solo registrare la metrica come sospetta.
	// La terza dovrebbe far scattare l'allarme.
	for i := 1; i <= 3; i++ {
		_, err := analysisServer.AnalyzeMetric(context.Background(), anomalousMetric)
		if err != nil {
			t.Fatalf("Errore inatteso alla chiamata %d: %v", i, err)
		}
	}

	// Verifica
	// Le prime 2 anomalie vengono salvate come metriche "sospette".
	if mockStore.storeMetricCalledCount != 2 {
		t.Errorf("StoreMetric doveva essere chiamato 2 volte per le metriche sospette, ma è stato chiamato %d volte", mockStore.storeMetricCalledCount)
	}
	// La terza anomalia fa scattare l'allarme.
	if mockStore.storeAlarmCalledCount != 1 {
		t.Errorf("StoreAlarm doveva essere chiamato 1 volta, ma è stato chiamato %d volte", mockStore.storeAlarmCalledCount)
	}
}

// Test per verificare che la logica di fallback funzioni e attivi la corroborazione.
func TestAnalyzeMetric_Fallback_TriggersAlarm_AfterThreshold(t *testing.T) {
	// Setup
	mockStore := &mockStorageClient{}
	mockInference := &mockInferenceClient{prediction: 0} // 0 = Fallimento

	// Apriamo il circuit breaker artificialmente per forzare il fallback
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{})
	for i := 0; i < 10; i++ {
		_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })
	}
	if cb.State() != gobreaker.StateOpen {
		t.Fatalf("Il Circuit Breaker non è aperto")
	}

	analysisServer := &server{
		storageClient:     mockStore,
		inferenceClient:   mockInference,
		circuitBreaker:    cb,
		suspiciousClients: make(map[string][]time.Time), // Inizializziamo la mappa!
		mu:                sync.Mutex{},
	}

	// Questa metrica attiverà la soglia del fallback (src_bytes > 95.0)
	metricForFallback := &pb.Metric{SourceClientId: "test-client", Features: make([]float32, 41)}
	metricForFallback.Features[4] = 100.0

	// Esecuzione: 3 chiamate per superare la soglia di corroborazione
	for i := 1; i <= 3; i++ {
		_, err := analysisServer.AnalyzeMetric(context.Background(), metricForFallback)
		if err != nil {
			t.Fatalf("Errore inatteso alla chiamata %d: %v", i, err)
		}
	}

	// Verifica
	if mockStore.storeMetricCalledCount != 2 {
		t.Errorf("StoreMetric doveva essere chiamato 2 volte, ma è stato chiamato %d volte", mockStore.storeMetricCalledCount)
	}
	if mockStore.storeAlarmCalledCount != 1 {
		t.Errorf("StoreAlarm doveva essere chiamato 1 volta, ma è stato chiamato %d volte", mockStore.storeAlarmCalledCount)
	}
	if mockStore.lastAlarm.RuleId != "anomaly_by_threshold_(fallback)" {
		t.Errorf("L'allarme doveva essere di tipo fallback, ma è '%s'", mockStore.lastAlarm.RuleId)
	}
}
