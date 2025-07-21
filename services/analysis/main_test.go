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

// --- Mock e Strutture di Supporto (Corretti) ---
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

// CORREZIONE: La risposta del mock non contiene più il campo 'Label'
func (m *mockInferenceClient) Predict(ctx context.Context, in *pb.InferenceRequest, opts ...grpc.CallOption) (*pb.InferenceResponse, error) {
	if m.prediction == 0 {
		return nil, errors.New("simulated inference failure")
	}
	// Restituisce solo il campo 'prediction', come fa il vero servizio Python
	return &pb.InferenceResponse{Prediction: m.prediction}, nil
}

// --- TEST AGGIORNATI ---

func TestAnalyzeMetric_NormalMetric(t *testing.T) {
	// Setup
	mockStore := &mockStorageClient{}
	// CORREZIONE: Il mock non ha più bisogno del campo 'label'
	mockInference := &mockInferenceClient{prediction: 1} // 1 = Normale

	analysisServer := &server{
		storageClient:     mockStore,
		inferenceClient:   mockInference,
		circuitBreaker:    gobreaker.NewCircuitBreaker(gobreaker.Settings{}),
		suspiciousClients: make(map[string][]time.Time),
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

func TestAnalyzeMetric_TriggersAlarm_AfterThreshold(t *testing.T) {
	// --- SETUP: Impostiamo le variabili globali usate dalla funzione ---
	anomalyThreshold = 3
	timeWindow = 1 * time.Minute

	// Setup dei mock
	mockStore := &mockStorageClient{}
	mockInference := &mockInferenceClient{prediction: -1} // -1 = Anomalia

	analysisServer := &server{
		storageClient:     mockStore,
		inferenceClient:   mockInference,
		circuitBreaker:    gobreaker.NewCircuitBreaker(gobreaker.Settings{}),
		suspiciousClients: make(map[string][]time.Time),
		mu:                sync.Mutex{},
	}

	anomalousMetric := &pb.Metric{SourceClientId: "test-client", Features: make([]float32, 41)}

	// Esecuzione
	for i := 1; i <= 3; i++ {
		_, err := analysisServer.AnalyzeMetric(context.Background(), anomalousMetric)
		if err != nil {
			t.Fatalf("Errore inatteso alla chiamata %d: %v", i, err)
		}
	}

	// Verifica
	if mockStore.storeMetricCalledCount != 2 {
		t.Errorf("StoreMetric doveva essere chiamato 2 volte per le metriche sospette, ma è stato chiamato %d volte", mockStore.storeMetricCalledCount)
	}
	if mockStore.storeAlarmCalledCount != 1 {
		t.Errorf("StoreAlarm doveva essere chiamato 1 volta, ma è stato chiamato %d volte", mockStore.storeAlarmCalledCount)
	}
}

func TestAnalyzeMetric_Fallback_TriggersAlarm_AfterThreshold(t *testing.T) {
	// --- SETUP: Impostiamo le variabili globali ---
	anomalyThreshold = 3
	timeWindow = 1 * time.Minute
	fallbackThreshold = 95.0

	// Setup dei mock
	mockStore := &mockStorageClient{}
	mockInference := &mockInferenceClient{prediction: 0} // 0 = Fallimento

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
		suspiciousClients: make(map[string][]time.Time),
		mu:                sync.Mutex{},
	}

	metricForFallback := &pb.Metric{SourceClientId: "test-client", Features: make([]float32, 41)}
	// NOTA: Il test precedente non impostava 'Value', ora lo facciamo per coerenza
	// anche se il codice attuale lo prende da Features[4]
	metricForFallback.Value = 100.0
	metricForFallback.Features[4] = 100.0

	// Esecuzione
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
	expectedRuleId := "correlated_anomaly_by_threshold_(fallback)"
	if mockStore.lastAlarm.RuleId != expectedRuleId {
		t.Errorf("L'allarme doveva avere RuleId '%s', ma ha '%s'", expectedRuleId, mockStore.lastAlarm.RuleId)
	}
}
