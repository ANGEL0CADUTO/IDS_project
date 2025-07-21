package main

import (
	"context"

	"fmt"
	"hash/fnv"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/ANGEL0CADUTO/IDS_project/pkg/consul"
	"github.com/ANGEL0CADUTO/IDS_project/pkg/tracing"
	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	consulapi "github.com/hashicorp/consul/api"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type server struct {
	pb.UnimplementedMetricsCollectorServer
	consulClient        *consulapi.Client
	analysisServiceName string
	// Manteniamo un pool di connessioni per riutilizzarle
	analysisConns   map[string]*grpc.ClientConn
	analysisConnsMu sync.RWMutex
}

// getAnalysisClientForMetric implementa il Consistent Hashing
func (s *server) getAnalysisClientForMetric(ctx context.Context, clientID string) (pb.AnalysisServiceClient, error) {
	// 1. Scopri tutte le istanze sane disponibili
	analysisAddrs, err := consul.DiscoverAllServices(s.consulClient, s.analysisServiceName)
	if err != nil || len(analysisAddrs) == 0 {
		return nil, fmt.Errorf("could not discover any healthy analysis service: %v", err)
	}

	// 2. Calcola l'hash del client ID per scegliere un'istanza in modo deterministico
	h := fnv.New32a()
	h.Write([]byte(clientID))
	hash := h.Sum32()
	index := int(hash % uint32(len(analysisAddrs)))
	targetAddr := analysisAddrs[index]

	// 3. Controlla se abbiamo già una connessione a quell'istanza nel nostro pool
	s.analysisConnsMu.RLock()
	conn, ok := s.analysisConns[targetAddr]
	s.analysisConnsMu.RUnlock()

	if ok {
		return pb.NewAnalysisServiceClient(conn), nil
	}

	// 4. Se non c'è una connessione, ne creiamo una nuova e la aggiungiamo al pool
	s.analysisConnsMu.Lock()
	defer s.analysisConnsMu.Unlock()

	// Ricontrolliamo nel caso un'altra goroutine l'abbia creata mentre aspettavamo il lock
	if conn, ok = s.analysisConns[targetAddr]; ok {
		return pb.NewAnalysisServiceClient(conn), nil
	}

	log.Printf("Creating new gRPC connection to analysis service at %s (for client %s)", targetAddr, clientID)
	conn, err = grpc.DialContext(ctx, targetAddr,
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

func (s *server) SendMetric(ctx context.Context, in *pb.Metric) (*pb.CollectorResponse, error) {
	log.Printf("Received metric from %s.", in.SourceClientId)

	// Usa il nuovo metodo di selezione basato sul client ID
	analysisClient, err := s.getAnalysisClientForMetric(ctx, in.SourceClientId)
	if err != nil {
		log.Printf("ERROR: Failed to get analysis client: %v", err)
		return &pb.CollectorResponse{Accepted: false, Message: "Upstream analysis service unavailable"}, err
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	analysisResp, err := analysisClient.AnalyzeMetric(ctxWithTimeout, in)
	if err != nil {
		log.Printf("ERROR: could not forward metric to analysis service: %v", err)
		return &pb.CollectorResponse{Accepted: false, Message: "Failed to forward metric"}, err
	}

	log.Printf("Metric forwarded successfully to instance. Response: %s", analysisResp.Message)

	return &pb.CollectorResponse{
		Accepted: analysisResp.Processed,
		Message:  analysisResp.Message,
	}, nil
}

func main() {
	consulAddr := getEnv("CONSUL_ADDR", "localhost:8500")
	jaegerAddr := getEnv("JAEGER_ADDR", "localhost:4317")
	serviceName := "collector-service"
	portStr := getEnv("GRPC_PORT", "50051")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatalf("Porta non valida: %v", err)
	}

	tp, err := tracing.InitTracerProvider(context.Background(), serviceName, jaegerAddr)
	if err != nil {
		log.Fatalf("Failed to initialize tracer provider: %v", err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	log.Println("Registrazione a Consul...")
	serviceID := fmt.Sprintf("%s-%s", serviceName, os.Getenv("HOSTNAME"))
	consulClientForRegistration := consul.RegisterService(consulAddr, serviceName, serviceID, port)
	defer consul.DeregisterService(consulClientForRegistration, serviceID)
	log.Println("Registrato a Consul con successo.")

	config := consulapi.DefaultConfig()
	config.Address = consulAddr
	consulClientForDiscovery, err := consulapi.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create consul client for discovery: %v", err)
	}
	analysisServiceName := getEnv("ANALYSIS_SERVICE_NAME", "analysis-service")

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

	pb.RegisterMetricsCollectorServer(s, &server{
		consulClient:        consulClientForDiscovery,
		analysisServiceName: analysisServiceName,
		analysisConns:       make(map[string]*grpc.ClientConn),
	})

	grpc_health_v1.RegisterHealthServer(s, health.NewServer())

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
