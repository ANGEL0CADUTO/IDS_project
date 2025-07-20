package main

import (
	"context"
	"fmt"
	"github.com/ANGEL0CADUTO/IDS_project/pkg/tracing"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ANGEL0CADUTO/IDS_project/pkg/consul"
	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	"github.com/sony/gobreaker"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// La struct del server ora usa un client gRPC per l'inferenza
type server struct {
	pb.UnimplementedAnalysisServiceServer
	storageClient   pb.StorageClient
	inferenceClient pb.InferenceClient // Sostituisce httpClient e inferenceSvcAddr
	circuitBreaker  *gobreaker.CircuitBreaker
	// --- NUOVI CAMPI PER GESTIRE I FALSI POSITIVI ---
	suspiciousClients map[string][]time.Time
	mu                sync.Mutex
}

func (s *server) AnalyzeMetric(ctx context.Context, in *pb.Metric) (*pb.AnalysisResponse, error) {
	log.Printf("Analyzing metric from %s: type=%s", in.SourceClientId, in.Type)

	if len(in.Features) != 41 { /* ... (codice invariato) */
	}

	isAnomaly := false
	analysisSource := ""

	response, err := s.circuitBreaker.Execute(func() (interface{}, error) {
		req := &pb.InferenceRequest{Features: in.Features}
		// Usiamo il contesto originale per la chiamata primaria per propagare timeout e tracing
		return s.inferenceClient.Predict(ctx, req)
	})

	if err != nil {
		// --- Logica di Fallback (INVARIATA) ---
		// ... (tutta la tua logica di fallback è corretta e rimane qui)
	} else {
		// La chiamata ha avuto successo
		analysisSource = "ML Model"
		infResp := response.(*pb.InferenceResponse)
		if infResp.Prediction == -1 {
			isAnomaly = true
		}
	}

	// --- INIZIO BLOCCO MODIFICATO: Logica di Corroborazione e Salvataggio ---

	if !isAnomaly {
		// Se non è un'anomalia, salva come metrica normale e termina.
		log.Printf("Metric is normal (Source: %s). Forwarding to storage.", analysisSource)
		_, err = s.storageClient.StoreMetric(ctx, in)
		if err != nil {
			log.Printf("ERROR: could not store metric: %v", err)
			return &pb.AnalysisResponse{Processed: false, Message: "Failed to store metric"}, err
		}
		return &pb.AnalysisResponse{Processed: true, Message: "Metric analyzed as normal and stored"}, nil
	}

	// Se siamo qui, 'isAnomaly' è true. Ora applichiamo la logica di corroborazione.
	log.Printf("Potential anomaly detected for client %s. Checking recent history...", in.SourceClientId)

	const (
		anomalyThreshold = 3               // Numero di anomalie per far scattare un allarme
		timeWindow       = 1 * time.Minute // Finestra temporale
	)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Pulisci i timestamp vecchi
	now := time.Now()
	clientHistory := s.suspiciousClients[in.SourceClientId]
	validTimestamps := []time.Time{}
	for _, ts := range clientHistory {
		if now.Sub(ts) < timeWindow {
			validTimestamps = append(validTimestamps, ts)
		}
	}

	// Aggiungi l'anomalia corrente
	validTimestamps = append(validTimestamps, now)
	s.suspiciousClients[in.SourceClientId] = validTimestamps

	// Controlla se abbiamo superato la soglia
	if len(validTimestamps) >= anomalyThreshold {
		log.Printf("--- ALARM TRIGGERED for client %s! (%d anomalies in last %v) ---", in.SourceClientId, len(validTimestamps), timeWindow)

		// Resetta i contatori per questo client per evitare allarmi a raffica
		s.suspiciousClients[in.SourceClientId] = []time.Time{}

		// Crea e salva l'allarme
		alarm := &pb.Alarm{
			RuleId:        fmt.Sprintf("anomaly_by_%s", strings.ToLower(strings.ReplaceAll(analysisSource, " ", "_"))),
			ClientId:      in.SourceClientId,
			Description:   fmt.Sprintf("Corroborated anomaly detected for client %s by %s", in.SourceClientId, analysisSource),
			Timestamp:     time.Now().Unix(),
			TriggerMetric: in,
		}
		_, err = s.storageClient.StoreAlarm(ctx, alarm)
		if err != nil {
			log.Printf("ERROR: could not store alarm: %v", err)
			return &pb.AnalysisResponse{Processed: false, Message: "Failed to store alarm"}, err
		}
		return &pb.AnalysisResponse{Processed: true, Message: fmt.Sprintf("Anomaly detected by %s and stored", analysisSource)}, nil
	}

	// Se non abbiamo superato la soglia, è solo un'anomalia sospetta. La registriamo come metrica normale.
	log.Printf("Suspicious metric from %s logged. Count: %d/%d. Not enough to trigger alarm.", in.SourceClientId, len(validTimestamps), anomalyThreshold)
	_, err = s.storageClient.StoreMetric(ctx, in) // La salviamo comunque, magari con un tag speciale in futuro
	if err != nil {
		log.Printf("ERROR: could not store suspicious metric: %v", err)
		return &pb.AnalysisResponse{Processed: false, Message: "Failed to store metric"}, err
	}
	return &pb.AnalysisResponse{Processed: true, Message: "Suspicious metric recorded, alarm not triggered"}, nil
}

// in services/analysis/main.go

func main() {
	log.Println("--- Avvio Analysis Service ---")

	// --- Configurazione Indirizzi e Servizi ---
	consulAddr := getEnv("CONSUL_ADDR", "localhost:8500")
	jaegerAddr := getEnv("JAEGER_ADDR", "localhost:4317")
	portStr := getEnv("GRPC_PORT", "50053")
	port, _ := strconv.Atoi(portStr)
	storageServiceName := getEnv("STORAGE_SERVICE_NAME", "storage-service")
	inferenceServiceName := getEnv("INFERENCE_SERVICE_NAME", "inference-service")

	// --- Inizializzazione Tracing ---
	log.Println("Inizializzazione Tracer Provider...")
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

	// --- BLOCCO CORRETTO: Configurazione del Circuit Breaker ---
	// Riempiamo la struct con impostazioni reattive e logging per il debug.
	st := gobreaker.Settings{
		Name:    "inference-service-cb", // Nome descrittivo per il circuit breaker
		Timeout: 15 * time.Second,       // Passa da Aperto a Semi-Aperto dopo 15 secondi
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Apre il circuito dopo 5 fallimenti consecutivi
			return counts.ConsecutiveFailures > 5
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			// Questo log è utilissimo per capire quando il circuito scatta
			log.Printf("!!!!!!!! CircuitBreaker '%s' cambiato stato da %s a %s !!!!!!!!!!", name, from, to)
		},
	}
	cb := gobreaker.NewCircuitBreaker(st)

	// --- Consul, Service Discovery e Connessioni gRPC ---
	log.Println("Registrazione a Consul...")
	serviceID := fmt.Sprintf("%s-%s", serviceName, os.Getenv("HOSTNAME"))
	consulClient := consul.RegisterService(consulAddr, serviceName, serviceID, port)
	defer consul.DeregisterService(consulClient, serviceID)
	log.Println("Registrato a Consul con successo.")

	// Connessione a Storage Service
	log.Printf("Discovery del servizio: %s...", storageServiceName)
	storageSvcAddr, err := consul.DiscoverService(consulClient, storageServiceName)
	if err != nil {
		log.Fatalf("FALLIMENTO CRITICO: Impossibile fare discovery di %s: %v", storageServiceName, err)
	}
	log.Printf("Servizio %s trovato a %s. Connessione in corso...", storageServiceName, storageSvcAddr)
	storageConn, err := grpc.Dial(storageSvcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
	)
	if err != nil {
		log.Fatalf("FALLIMENTO CRITICO: Impossibile connettersi a %s: %v", storageServiceName, err)
	}
	defer storageConn.Close()
	storageClient := pb.NewStorageClient(storageConn)
	log.Printf("Connesso a %s.", storageServiceName)

	// Connessione a Inference Service
	log.Printf("Discovery del servizio: %s...", inferenceServiceName)
	inferenceSvcAddr, err := consul.DiscoverService(consulClient, inferenceServiceName)
	if err != nil {
		log.Fatalf("FALLIMENTO CRITICO: Impossibile fare discovery di %s: %v", inferenceServiceName, err)
	}
	log.Printf("Servizio %s trovato a %s. Connessione in corso...", inferenceServiceName, inferenceSvcAddr)
	inferenceConn, err := grpc.Dial(inferenceSvcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
	)
	if err != nil {
		log.Fatalf("FALLIMENTO CRITICO: Impossibile connettersi a %s: %v", inferenceServiceName, err)
	}
	defer inferenceConn.Close()
	inferenceClient := pb.NewInferenceClient(inferenceConn)
	log.Printf("Connesso a %s.", inferenceServiceName)

	// --- Avvio del Server gRPC ---
	log.Println("Avvio del listener gRPC...")
	lis, err := net.Listen("tcp", ":"+portStr)
	if err != nil {
		log.Fatalf("FALLIMENTO CRITICO: failed to listen: %v", err)
	}

	// --- BLOCCO CORRETTO: Creazione del Server gRPC con Interceptor per il Tracing ---
	// Questo risolve il problema della traccia spezzata. Il server ora sa
	// come intercettare le chiamate in ingresso e continuare la traccia esistente.
	s := grpc.NewServer(
		grpc.UnaryInterceptor(
			tracing.NewConditionalUnaryInterceptor(
				otelgrpc.UnaryServerInterceptor(),
				"/grpc.health.v1.Health/Check", // Escludiamo gli health check dal tracing
			),
		),
	)

	// Registrazione dei servizi sul server gRPC
	pb.RegisterAnalysisServiceServer(s, &server{
		storageClient:   storageClient,
		inferenceClient: inferenceClient,
		circuitBreaker:  cb,
	})
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
