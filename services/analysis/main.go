package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ANGEL0CADUTO/IDS_project/pkg/consul"
	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	"github.com/sony/gobreaker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// Struct per la richiesta/risposta JSON
type InferenceRequest struct {
	Features []float32 `json:"features"`
}
type InferenceResponse struct {
	Prediction int `json:"prediction"`
}

// ***  Aggiunto httpClient alla struct del server ***
type server struct {
	pb.UnimplementedAnalysisServiceServer
	storageClient    pb.StorageClient
	inferenceSvcAddr string
	circuitBreaker   *gobreaker.CircuitBreaker
	httpClient       *http.Client // risolve problema client che attende troppo con circuitbreaker open
}

func (s *server) AnalyzeMetric(ctx context.Context, in *pb.Metric) (*pb.AnalysisResponse, error) {
	log.Printf("Analyzing metric from %s: type=%s", in.SourceClientId, in.Type)

	if len(in.Features) != 41 {
		log.Printf("WARN: Metric received without 41 features. Skipping analysis.")
		return &pb.AnalysisResponse{Processed: true, Message: "Metric skipped (incomplete features)"}, nil
	}

	isAnomaly := false
	analysisSource := ""
	metricFeatures := in.Features
	reqBody, err := json.Marshal(InferenceRequest{Features: metricFeatures})
	if err != nil {
		log.Printf("FATAL: could not marshal inference request: %v", err)
		return &pb.AnalysisResponse{Processed: false, Message: "Internal server error"}, err
	}

	responseBody, err := s.circuitBreaker.Execute(func() (interface{}, error) {
		// *** Utilizzo del client HTTP personalizzato (s.httpClient) ***
		// chiamata ora fallirà rapidamente (dopo 1 secondo) se il servizio non risponde.
		resp, err := s.httpClient.Post(s.inferenceSvcAddr, "application/json", bytes.NewBuffer(reqBody))

		if err != nil {
			return nil, err // L'errore (es. timeout) viene propagato al circuit breaker
		}

		if resp.StatusCode != http.StatusOK {
			// È importante chiudere il body anche in caso di risposta non-200
			// per evitare perdite di risorse.
			resp.Body.Close()
			return nil, fmt.Errorf("inference service returned status code %d", resp.StatusCode)
		}

		return resp, nil
	})

	if err != nil {
		log.Printf("WARN: CircuitBreaker is OPEN or inference call failed: %v. Falling back to threshold logic.", err)

		analysisSource = "Threshold (Fallback)"
		triggerValue := in.Value
		if len(in.Features) > 4 {
			triggerValue = float64(in.Features[4])
		}
		if triggerValue > 95.0 {
			isAnomaly = true
		}

	} else {
		analysisSource = "ML Model"
		resp := responseBody.(*http.Response)
		defer resp.Body.Close()

		var infResp InferenceResponse
		if err := json.NewDecoder(resp.Body).Decode(&infResp); err != nil {
			log.Printf("ERROR: could not decode inference response: %v", err)
			return &pb.AnalysisResponse{Processed: false, Message: "Invalid inference response"}, err
		}

		if infResp.Prediction == -1 {
			isAnomaly = true
		}
	}

	// Logica di salvataggio unificata
	if isAnomaly {
		log.Printf("--- ANOMALY DETECTED (Source: %s) ---", analysisSource)

		alarm := &pb.Alarm{
			RuleId:        fmt.Sprintf("anomaly_by_%s", strings.ToLower(strings.ReplaceAll(analysisSource, " ", "_"))),
			ClientId:      in.SourceClientId,
			Description:   fmt.Sprintf("Anomaly detected for metric type %s by %s", in.Type, analysisSource),
			Timestamp:     time.Now().Unix(),
			TriggerMetric: in,
		}

		_, err = s.storageClient.StoreAlarm(context.Background(), alarm)
		if err != nil {
			log.Printf("ERROR: could not store alarm: %v", err)
			return &pb.AnalysisResponse{Processed: false, Message: "Failed to store alarm"}, err
		}
		return &pb.AnalysisResponse{Processed: true, Message: fmt.Sprintf("Anomaly detected by %s and stored", analysisSource)}, nil
	}

	log.Printf("Metric is normal (Source: %s). Forwarding to storage.", analysisSource)
	_, err = s.storageClient.StoreMetric(context.Background(), in)
	if err != nil {
		log.Printf("ERROR: could not store metric: %v", err)
		return &pb.AnalysisResponse{Processed: false, Message: "Failed to store metric"}, err
	}
	return &pb.AnalysisResponse{Processed: true, Message: "Metric analyzed as normal and stored"}, nil
}

func main() {
	inferenceAddr := getEnv("INFERENCE_SERVICE_ADDR", "http://localhost:5000/predict")

	st := gobreaker.Settings{
		Name:        "inference-http",
		MaxRequests: 3,
		Interval:    0,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures > 5
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Printf("!!!!!!!! CircuitBreaker '%s' changed state from %s to %s !!!!!!!!!!", name, from, to)
		},
	}
	cb := gobreaker.NewCircuitBreaker(st)

	consulAddr := getEnv("CONSUL_ADDR", "localhost:8500")
	portStr := getEnv("GRPC_PORT", "50053")
	port, _ := strconv.Atoi(portStr)

	serviceName := "analysis-service"
	serviceID := fmt.Sprintf("%s-%s", serviceName, os.Getenv("HOSTNAME"))
	consulClient := consul.RegisterService(consulAddr, serviceName, serviceID, port)
	defer consul.DeregisterService(consulClient, serviceID)

	storageServiceName := getEnv("STORAGE_SERVICE_NAME", "storage-service")
	storageSvcAddr, err := consul.DiscoverService(consulClient, storageServiceName)
	if err != nil {
		log.Fatalf("Could not discover storage service: %v", err)
	}

	conn, err := grpc.Dial(storageSvcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Failed to connect to storage service: %v", err)
	}
	defer conn.Close()
	storageClient := pb.NewStorageClient(conn)
	log.Printf("Connected to storage service at %s", storageSvcAddr)

	lis, err := net.Listen("tcp", ":"+portStr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()

	// *** MODIFICA 3: Creazione e inizializzazione del client HTTP personalizzato ***
	httpClient := &http.Client{
		Timeout: 1 * time.Second, // Impostiamo un timeout aggressivo di 1 secondo
	}
	pb.RegisterAnalysisServiceServer(s, &server{
		storageClient:    storageClient,
		inferenceSvcAddr: inferenceAddr,
		circuitBreaker:   cb,
		httpClient:       httpClient, // Inizializziamo il nuovo campo nel server
	})

	grpc_health_v1.RegisterHealthServer(s, health.NewServer())

	log.Printf("Analysis service listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
