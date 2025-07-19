package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ANGEL0CADUTO/IDS_project/pkg/tracing"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/trace"
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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
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
	// Log di debug per verificare il contesto in entrata
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		log.Printf("[DEBUG] Analysis: Received span. TraceID: %s, SpanID: %s",
			span.SpanContext().TraceID().String(),
			span.SpanContext().SpanID().String(),
		)
	} else {
		log.Printf("[DEBUG] Analysis: Received a request without a valid span.")
	}

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

	// NOTA: il client s.httpClient è già strumentato con otelhttp.NewTransport,
	// quindi non dobbiamo passare esplicitamente il contesto qui,
	// lo estrarrà dalla richiesta in arrivo associata al contesto.
	responseBody, err := s.circuitBreaker.Execute(func() (interface{}, error) {
		resp, err := s.httpClient.Post(s.inferenceSvcAddr, "application/json", bytes.NewBuffer(reqBody))

		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
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

	// --- Logica di salvataggio unificata con il CONTESTO CORRETTO ---
	if isAnomaly {
		log.Printf("--- ANOMALY DETECTED (Source: %s) ---", analysisSource)

		alarm := &pb.Alarm{
			RuleId:        fmt.Sprintf("anomaly_by_%s", strings.ToLower(strings.ReplaceAll(analysisSource, " ", "_"))),
			ClientId:      in.SourceClientId,
			Description:   fmt.Sprintf("Anomaly detected for metric type %s by %s", in.Type, analysisSource),
			Timestamp:     time.Now().Unix(),
			TriggerMetric: in,
		}

		// *** CORREZIONE FONDAMENTALE: Passiamo 'ctx' invece di context.Background() ***
		_, err = s.storageClient.StoreAlarm(ctx, alarm)
		if err != nil {
			log.Printf("ERROR: could not store alarm: %v", err)
			return &pb.AnalysisResponse{Processed: false, Message: "Failed to store alarm"}, err
		}
		return &pb.AnalysisResponse{Processed: true, Message: fmt.Sprintf("Anomaly detected by %s and stored", analysisSource)}, nil
	}

	log.Printf("Metric is normal (Source: %s). Forwarding to storage.", analysisSource)

	// *** CORREZIONE FONDAMENTALE: Passiamo 'ctx' invece di context.Background() ***
	_, err = s.storageClient.StoreMetric(ctx, in)
	if err != nil {
		log.Printf("ERROR: could not store metric: %v", err)
		return &pb.AnalysisResponse{Processed: false, Message: "Failed to store metric"}, err
	}
	return &pb.AnalysisResponse{Processed: true, Message: "Metric analyzed as normal and stored"}, nil
}

func main() {
	// --- Configurazione degli indirizzi dei servizi ---
	inferenceAddr := getEnv("INFERENCE_SERVICE_ADDR", "http://localhost:5000/predict")
	jaegerAddr := getEnv("JAEGER_ADDR", "localhost:4317")
	consulAddr := getEnv("CONSUL_ADDR", "localhost:8500")
	portStr := getEnv("GRPC_PORT", "50053")
	port, _ := strconv.Atoi(portStr)

	// --- Inizializzazione del Tracer Provider di OpenTelemetry ---
	serviceName := "analysis-service"
	tp, err := tracing.InitTracerProvider(context.Background(), serviceName, jaegerAddr)
	if err != nil {
		log.Fatalf("Failed to initialize tracer provider: %v", err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	// --- Configurazione del Circuit Breaker (invariata) ---
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

	// --- Registrazione a Consul (invariata) ---
	serviceID := fmt.Sprintf("%s-%s", serviceName, os.Getenv("HOSTNAME"))
	consulClient := consul.RegisterService(consulAddr, serviceName, serviceID, port)
	defer consul.DeregisterService(consulClient, serviceID)

	// --- Scoperta e Connessione al servizio di Storage (invariata) ---
	storageServiceName := getEnv("STORAGE_SERVICE_NAME", "storage-service")
	storageSvcAddr, err := consul.DiscoverService(consulClient, storageServiceName)
	if err != nil {
		log.Fatalf("Could not discover storage service: %v", err)
	}

	conn, err := grpc.Dial(storageSvcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()), // <-- AGGIUNGI QUESTA OPZIONE
	)
	if err != nil {
		log.Fatalf("Failed to connect to storage service: %v", err)
	}
	defer conn.Close()
	storageClient := pb.NewStorageClient(conn)
	log.Printf("Connected to storage service at %s", storageSvcAddr)
	// --- Creazione del Listener di rete (invariata) ---
	lis, err := net.Listen("tcp", ":"+portStr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// --- Creazione del server gRPC con l'interceptor per il Tracing ---
	s := grpc.NewServer(
		grpc.UnaryInterceptor(
			tracing.NewConditionalUnaryInterceptor(
				otelgrpc.UnaryServerInterceptor(),
				"/grpc.health.v1.Health/Check",
			),
		),
	)

	// Creiamo un client HTTP "strumentato".
	// otelhttp.NewTransport wrappa il transport HTTP di default e si occupa
	// di creare uno span figlio e iniettare gli header di traccia (W3C Trace Context)
	// in ogni richiesta HTTP in uscita.
	httpClient := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   1 * time.Second,
	}

	// La registrazione del servizio rimane la stessa, ma ora userà il nuovo client strumentato.
	pb.RegisterAnalysisServiceServer(s, &server{
		storageClient:    storageClient,
		inferenceSvcAddr: inferenceAddr,
		circuitBreaker:   cb,
		httpClient:       httpClient,
	})

	// Registrazione dell'Health Server (invariata)
	grpc_health_v1.RegisterHealthServer(s, health.NewServer())

	// Avvio del server
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
