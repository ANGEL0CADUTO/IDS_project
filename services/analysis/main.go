package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ANGEL0CADUTO/IDS_project/pkg/consul"
	"github.com/ANGEL0CADUTO/IDS_project/pkg/tracing"
	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	"github.com/sony/gobreaker"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// --- PARAMETRI CONFIGURABILI PER LA CORRELAZIONE ---
var (
	anomalyThreshold  int
	timeWindow        time.Duration
	fallbackThreshold float64 // NUOVA VARIABILE
)

type server struct {
	pb.UnimplementedAnalysisServiceServer
	storageClient     pb.StorageClient
	inferenceClient   pb.InferenceClient
	circuitBreaker    *gobreaker.CircuitBreaker
	suspiciousClients map[string][]time.Time
	mu                sync.Mutex
}

func (s *server) AnalyzeMetric(ctx context.Context, in *pb.Metric) (*pb.AnalysisResponse, error) {
	log.Printf("Analyzing metric from %s: type=%s", in.SourceClientId, in.Type)

	if len(in.Features) != 41 {
		log.Printf("WARN: Metric received without 41 features. Skipping analysis.")
		return &pb.AnalysisResponse{Processed: true, Message: "Metric skipped (incomplete features)"}, nil
	}

	isAnomaly := false
	analysisSource := ""

	response, err := s.circuitBreaker.Execute(func() (interface{}, error) {
		log.Println("[DEBUG] Chiamata al servizio di inferenza (dentro Circuit Breaker)...")
		req := &pb.InferenceRequest{Features: in.Features}
		return s.inferenceClient.Predict(ctx, req)
	})

	if err != nil {

		log.Printf("WARN: CircuitBreaker is OPEN or inference call failed: %v. Falling back to threshold logic.", err)
		analysisSource = "Threshold (Fallback)"
		var triggerValue float64
		if len(in.Features) > 4 {
			triggerValue = float64(in.Features[4])
		}
		if triggerValue > fallbackThreshold {
			isAnomaly = true
		}
	} else {
		analysisSource = "ML Model"
		infResp := response.(*pb.InferenceResponse)
		if infResp.Prediction == -1 { // Usiamo il modello IsolationForest
			isAnomaly = true
		}
	}

	if !isAnomaly {
		log.Printf("Metric is normal (Source: %s). Forwarding to storage.", analysisSource)
		storeCtx := ctx
		if analysisSource == "Threshold (Fallback)" {
			storeCtx = context.Background()
		}
		_, err = s.storageClient.StoreMetric(storeCtx, in)
		if err != nil {
			log.Printf("ERROR: could not store metric: %v", err)
			return &pb.AnalysisResponse{Processed: false, Message: "Failed to store metric"}, err
		}
		return &pb.AnalysisResponse{Processed: true, Message: "Metric analyzed as normal and stored"}, nil
	}

	// Se arriviamo qui, la metrica è stata classificata come anomala
	log.Printf("Potential anomaly detected for client %s from %s. Checking recent history...", in.SourceClientId, analysisSource)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	clientHistory := s.suspiciousClients[in.SourceClientId]
	var validTimestamps []time.Time
	for _, ts := range clientHistory {
		if now.Sub(ts) < timeWindow {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	validTimestamps = append(validTimestamps, now)
	s.suspiciousClients[in.SourceClientId] = validTimestamps

	if len(validTimestamps) >= anomalyThreshold {
		log.Printf("--- ALARM TRIGGERED for client %s! (%d anomalies in last %v from %s) ---", in.SourceClientId, len(validTimestamps), timeWindow, analysisSource)
		s.suspiciousClients[in.SourceClientId] = []time.Time{} // Resetta la storia

		alarm := &pb.Alarm{
			RuleId:        fmt.Sprintf("correlated_anomaly_by_%s", strings.ToLower(strings.ReplaceAll(analysisSource, " ", "_"))),
			ClientId:      in.SourceClientId,
			Description:   fmt.Sprintf("Correlated anomaly detected for client %s by %s", in.SourceClientId, analysisSource),
			Timestamp:     time.Now().Unix(),
			TriggerMetric: in,
		}
		storeCtx := ctx
		if analysisSource == "Threshold (Fallback)" {
			storeCtx = context.Background()
		}
		_, err = s.storageClient.StoreAlarm(storeCtx, alarm)
		if err != nil {
			log.Printf("ERROR: could not store alarm: %v", err)
			return &pb.AnalysisResponse{Processed: false, Message: "Failed to store alarm"}, err
		}
		return &pb.AnalysisResponse{Processed: true, Message: fmt.Sprintf("Correlated anomaly detected by %s and stored", analysisSource)}, nil
	}

	// Se non abbiamo raggiunto la soglia, l'anomalia è "sospetta".
	// La salviamo come una metrica normale.
	log.Printf("Suspicious metric from %s logged as normal. Count: %d/%d. Not enough to trigger alarm.", in.SourceClientId, len(validTimestamps), anomalyThreshold)
	storeCtx := ctx
	if analysisSource == "Threshold (Fallback)" {
		storeCtx = context.Background()
	}
	_, err = s.storageClient.StoreMetric(storeCtx, in) // <-- Salva come metrica normale
	if err != nil {
		log.Printf("ERROR: could not store suspicious metric: %v", err)
		return &pb.AnalysisResponse{Processed: false, Message: "Failed to store metric"}, err
	}
	return &pb.AnalysisResponse{Processed: true, Message: "Suspicious metric recorded, alarm not triggered"}, nil
}

func main() {
	log.Println("--- Avvio Analysis Service ---")

	// --- PARAMETRI RESI CONFIGURABILI ---
	thresholdStr := getEnv("ALARM_THRESHOLD", "3")
	windowStr := getEnv("ALARM_WINDOW_SECONDS", "60")
	fallbackThresholdStr := getEnv("FALLBACK_THRESHOLD", "95.0")
	var err error
	anomalyThreshold, err = strconv.Atoi(thresholdStr)
	if err != nil {
		log.Fatalf("Invalid ALARM_THRESHOLD: %v", err)
	}
	windowSec, err := strconv.Atoi(windowStr)
	if err != nil {
		log.Fatalf("Invalid ALARM_WINDOW_SECONDS: %v", err)
	}
	timeWindow = time.Duration(windowSec) * time.Second
	fallbackThreshold, err = strconv.ParseFloat(fallbackThresholdStr, 64)
	if err != nil {
		log.Fatalf("Invalid FALLBACK_THRESHOLD: %v", err)
	}

	consulAddr := getEnv("CONSUL_ADDR", "localhost:8500")
	jaegerAddr := getEnv("JAEGER_ADDR", "localhost:4317")
	portStr := getEnv("GRPC_PORT", "50053")
	port, _ := strconv.Atoi(portStr)
	storageServiceName := getEnv("STORAGE_SERVICE_NAME", "storage-service")
	inferenceServiceName := getEnv("INFERENCE_SERVICE_NAME", "inference-service")
	serviceName := "analysis-service"
	tp, err := tracing.InitTracerProvider(context.Background(), serviceName, jaegerAddr)
	if err != nil {
		log.Fatalf("FALLIMENTO CRITICO: Impossibile inizializzare tracer provider: %v", err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Errore durante lo shutdown del tracer provider: %v", err)
		}
	}()
	log.Println("Tracer Provider inizializzato.")
	st := gobreaker.Settings{
		Name:        "inference-service-cb",
		Timeout:     15 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool { return counts.ConsecutiveFailures > 5 },
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Printf("!!!!!!!! CircuitBreaker '%s' cambiato stato da %s a %s !!!!!!!!!!", name, from, to)
		},
	}
	cb := gobreaker.NewCircuitBreaker(st)
	log.Println("Registrazione a Consul...")
	serviceID := fmt.Sprintf("%s-%s", serviceName, os.Getenv("HOSTNAME"))
	consulClient := consul.RegisterService(consulAddr, serviceName, serviceID, port)
	defer consul.DeregisterService(consulClient, serviceID)
	log.Println("Registrato a Consul con successo.")
	storageSvcAddr, err := consul.DiscoverService(consulClient, storageServiceName)
	if err != nil {
		log.Fatalf("FALLIMENTO CRITICO: Impossibile fare discovery di %s: %v", storageServiceName, err)
	}
	storageConn, err := grpc.Dial(storageSvcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(), grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()))
	if err != nil {
		log.Fatalf("FALLIMENTO CRITICO: Impossibile connettersi a %s: %v", storageServiceName, err)
	}
	defer storageConn.Close()
	storageClient := pb.NewStorageClient(storageConn)
	log.Printf("Connesso a %s.", storageServiceName)
	inferenceSvcAddr, err := consul.DiscoverService(consulClient, inferenceServiceName)
	if err != nil {
		log.Fatalf("FALLIMENTO CRITICO: Impossibile fare discovery di %s: %v", inferenceServiceName, err)
	}
	inferenceConn, err := grpc.Dial(inferenceSvcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(), grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()))
	if err != nil {
		log.Fatalf("FALLIMENTO CRITICO: Impossibile connettersi a %s: %v", inferenceServiceName, err)
	}
	defer inferenceConn.Close()
	inferenceClient := pb.NewInferenceClient(inferenceConn)
	log.Printf("Connesso a %s.", inferenceServiceName)
	lis, err := net.Listen("tcp", ":"+portStr)
	if err != nil {
		log.Fatalf("FALLIMENTO CRITICO: failed to listen: %v", err)
	}
	s := grpc.NewServer(
		grpc.UnaryInterceptor(
			tracing.NewConditionalUnaryInterceptor(
				otelgrpc.UnaryServerInterceptor(),
				"/grpc.health.v1.Health/Check",
			),
		),
	)

	serverInstance := &server{
		storageClient:     storageClient,
		inferenceClient:   inferenceClient,
		circuitBreaker:    cb,
		suspiciousClients: make(map[string][]time.Time),
		mu:                sync.Mutex{},
	}
	pb.RegisterAnalysisServiceServer(s, serverInstance)

	grpc_health_v1.RegisterHealthServer(s, health.NewServer())
	log.Printf("Analysis service in ascolto su %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("FALLIMENTO CRITICO: failed to serve: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
