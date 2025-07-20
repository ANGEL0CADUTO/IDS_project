package main

import (
	"context"
	"errors"
	"testing"

	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	"github.com/sony/gobreaker"
	"google.golang.org/grpc"
)

// --- Mock del client gRPC per lo Storage ---
type mockStorageClient struct {
	pb.StorageClient
	storeMetricCalled bool
	storeAlarmCalled  bool
	lastAlarm         *pb.Alarm
}

func (m *mockStorageClient) StoreMetric(ctx context.Context, in *pb.Metric, opts ...grpc.CallOption) (*pb.StorageResponse, error) {
	m.storeMetricCalled = true
	return &pb.StorageResponse{Success: true}, nil
}

func (m *mockStorageClient) StoreAlarm(ctx context.Context, in *pb.Alarm, opts ...grpc.CallOption) (*pb.StorageResponse, error) {
	m.storeAlarmCalled = true
	m.lastAlarm = in
	return &pb.StorageResponse{Success: true}, nil
}

// --- Mock del client gRPC per l'Inference ---
type mockInferenceClient struct {
	pb.InferenceClient
	shouldFail bool  // Per simulare un fallimento
	prediction int32 // La predizione che il mock deve restituire
}

func (m *mockInferenceClient) Predict(ctx context.Context, in *pb.InferenceRequest, opts ...grpc.CallOption) (*pb.InferenceResponse, error) {
	if m.shouldFail {
		return nil, errors.New("simulated inference failure")
	}
	return &pb.InferenceResponse{Prediction: m.prediction}, nil
}

func TestAnalyzeMetric_HappyPath_Normal(t *testing.T) {
	// Fase 1: Setup
	mockStore := &mockStorageClient{}
	mockInference := &mockInferenceClient{prediction: 1} // 1 = Normale
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{})

	analysisServer := &server{
		storageClient:   mockStore,
		inferenceClient: mockInference,
		circuitBreaker:  cb,
	}

	normalMetric := &pb.Metric{
		SourceClientId: "test-client-happy",
		Type:           "network_traffic",
		Features:       make([]float32, 41),
	}

	// Fase 2: Esecuzione
	_, err := analysisServer.AnalyzeMetric(context.Background(), normalMetric)

	// Fase 3: Verifica
	if err != nil {
		t.Fatalf("AnalyzeMetric ha restituito un errore inatteso: %v", err)
	}
	if !mockStore.storeMetricCalled {
		t.Errorf("StoreMetric doveva essere chiamato, ma non lo è stato")
	}
	if mockStore.storeAlarmCalled {
		t.Errorf("StoreAlarm NON doveva essere chiamato")
	}
}

func TestAnalyzeMetric_HappyPath_Anomaly(t *testing.T) {
	// Fase 1: Setup
	mockStore := &mockStorageClient{}
	mockInference := &mockInferenceClient{prediction: -1} // -1 = Anomalia
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{})

	analysisServer := &server{
		storageClient:   mockStore,
		inferenceClient: mockInference,
		circuitBreaker:  cb,
	}

	anomalousMetric := &pb.Metric{
		SourceClientId: "test-client-anomaly",
		Type:           "network_traffic",
		Features:       make([]float32, 41),
	}

	// Fase 2: Esecuzione
	_, err := analysisServer.AnalyzeMetric(context.Background(), anomalousMetric)

	// Fase 3: Verifica
	if err != nil {
		t.Fatalf("AnalyzeMetric ha restituito un errore inatteso: %v", err)
	}
	if !mockStore.storeAlarmCalled {
		t.Errorf("StoreAlarm doveva essere chiamato")
	}
	if mockStore.storeMetricCalled {
		t.Errorf("StoreMetric NON doveva essere chiamato")
	}
}

func TestAnalyzeMetric_FallbackLogic(t *testing.T) {
	// Fase 1: Setup
	mockStore := &mockStorageClient{}
	mockInference := &mockInferenceClient{shouldFail: true} // Il client di inferenza fallirà
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{})

	analysisServer := &server{
		storageClient:   mockStore,
		inferenceClient: mockInference,
		circuitBreaker:  cb,
	}

	// Questa metrica attiverà la soglia di fallback (src_bytes > 95.0)
	metricForFallback := &pb.Metric{
		SourceClientId: "test-client-fallback",
		Type:           "network_traffic",
		Features:       make([]float32, 41),
	}
	metricForFallback.Features[4] = 100.0

	// Fase 2: Esecuzione
	// La prima chiamata fallirà, ma il CB è ancora chiuso.
	// Eseguiamo alcune chiamate per essere sicuri che si apra.
	for i := 0; i < 10; i++ {
		_, _ = analysisServer.AnalyzeMetric(context.Background(), metricForFallback)
	}

	// Fase 3: Verifica
	if cb.State() != gobreaker.StateOpen {
		t.Fatalf("Il Circuit Breaker doveva essere nello stato Open, ma è in %s", cb.State())
	}
	if !mockStore.storeAlarmCalled {
		t.Errorf("StoreAlarm doveva essere chiamato durante il fallback")
	}
	// Verifichiamo che l'allarme sia stato generato dalla regola di fallback
	if mockStore.lastAlarm.RuleId != "anomaly_by_threshold_(fallback)" {
		t.Errorf("L'allarme doveva essere di tipo fallback, ma è '%s'", mockStore.lastAlarm.RuleId)
	}
}
