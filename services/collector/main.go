package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sync" // <-- NUOVO IMPORT
	"time"

	"github.com/ANGEL0CADUTO/IDS_project/pkg/consul"
	"github.com/ANGEL0CADUTO/IDS_project/pkg/tracing"
	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	consulapi "github.com/hashicorp/consul/api"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// --- MODIFICA 1: La struct del server ora contiene un pool di connessioni ---
type server struct {
	pb.UnimplementedMetricsCollectorServer
	consulClient        *consulapi.Client
	analysisServiceName string

	// Pool di connessioni per le istanze di analysis-service
	analysisConns   map[string]*grpc.ClientConn
	analysisConnsMu sync.RWMutex // Mutex per proteggere l'accesso alla mappa
}

func (s *server) getAnalysisClient(ctx context.Context) (pb.AnalysisServiceClient, error) {
	// ... la logica di discovery e selezione dell'indirizzo rimane uguale ...
	analysisAddrs, err := consul.DiscoverAllServices(s.consulClient, s.analysisServiceName)
	if err != nil || len(analysisAddrs) == 0 {
		return nil, fmt.Errorf("could not discover any healthy analysis service: %v", err)
	}
	targetAddr := analysisAddrs[time.Now().Unix()%int64(len(analysisAddrs))]

	s.analysisConnsMu.RLock()
	conn, ok := s.analysisConns[targetAddr]
	s.analysisConnsMu.RUnlock()

	if ok {
		return pb.NewAnalysisServiceClient(conn), nil
	}

	s.analysisConnsMu.Lock()
	defer s.analysisConnsMu.Unlock()

	if conn, ok = s.analysisConns[targetAddr]; ok {
		return pb.NewAnalysisServiceClient(conn), nil
	}

	log.Printf("Creating new gRPC connection to analysis service at %s", targetAddr)
	// --- L'UNICA MODIFICA È QUI ---
	// Usiamo grpc.Dial invece di grpc.DialContext. La connessione è agnostica alla richiesta.
	conn, err = grpc.Dial(targetAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to analysis service at %s: %w", targetAddr, err)
	}

	s.analysisConns[targetAddr] = conn
	return pb.NewAnalysisServiceClient(conn), nil
}

// in services/collector/main.go

func (s *server) SendMetric(ctx context.Context, in *pb.Metric) (*pb.CollectorResponse, error) {
	// --- DEBUG LOG 1: Controlliamo lo span in entrata ---
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		log.Printf("[DEBUG] Collector: Received span. TraceID: %s, SpanID: %s",
			span.SpanContext().TraceID().String(),
			span.SpanContext().SpanID().String(),
		)
	} else {
		log.Printf("[DEBUG] Collector: Received a request without a valid span.")
	}

	log.Printf("Received metric from %s.", in.SourceClientId)

	analysisClient, err := s.getAnalysisClient(ctx)
	if err != nil {
		log.Printf("ERROR: Failed to get analysis client: %v", err)
		return &pb.CollectorResponse{Accepted: false, Message: "Upstream analysis service unavailable"}, err
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// --- DEBUG LOG 2: Controlliamo lo span prima della chiamata in uscita ---
	spanForCall := trace.SpanFromContext(ctxWithTimeout)
	if spanForCall.SpanContext().IsValid() {
		log.Printf("[DEBUG] Collector: Making outbound call. TraceID: %s, SpanID: %s",
			spanForCall.SpanContext().TraceID().String(),
			spanForCall.SpanContext().SpanID().String(),
		)
	}

	analysisResp, err := analysisClient.AnalyzeMetric(ctxWithTimeout, in)
	if err != nil {
		log.Printf("ERROR: could not forward metric to analysis service: %v", err)
		return &pb.CollectorResponse{Accepted: false, Message: "Failed to forward metric"}, err
	}

	log.Printf("Metric forwarded successfully. Response: %s", analysisResp.Message)

	return &pb.CollectorResponse{
		Accepted: analysisResp.Processed,
		Message:  analysisResp.Message,
	}, nil
}

func main() {
	// ... (La parte di inizializzazione con tracer, consul, etc. è identica a prima)
	consulAddr := getEnv("CONSUL_ADDR", "localhost:8500")
	jaegerAddr := getEnv("JAEGER_ADDR", "localhost:4317")
	serviceName := "collector-service"

	tp, err := tracing.InitTracerProvider(context.Background(), serviceName, jaegerAddr)
	if err != nil {
		log.Fatalf("Failed to initialize tracer provider: %v", err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	config := consulapi.DefaultConfig()
	config.Address = consulAddr
	consulClient, err := consulapi.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create consul client: %v", err)
	}
	analysisServiceName := getEnv("ANALYSIS_SERVICE_NAME", "analysis-service")

	portStr := getEnv("GRPC_PORT", "50051")
	lis, err := net.Listen("tcp", ":"+portStr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer(
		grpc.UnaryInterceptor(
			tracing.NewConditionalUnaryInterceptor(
				otelgrpc.UnaryServerInterceptor(),
				"/grpc.health.v1.Health/Check",
			),
		),
	)

	// --- MODIFICA 3: Inizializziamo il server con il pool di connessioni vuoto ---
	pb.RegisterMetricsCollectorServer(s, &server{
		consulClient:        consulClient,
		analysisServiceName: analysisServiceName,
		analysisConns:       make(map[string]*grpc.ClientConn), // Inizializza la mappa
	})

	log.Printf("Collector service listening at %v", lis.Addr())
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
