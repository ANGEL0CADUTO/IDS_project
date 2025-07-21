package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv" // Import necessario per convertire la porta
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
	analysisConns       map[string]*grpc.ClientConn
	analysisConnsMu     sync.RWMutex
}

func (s *server) getAnalysisClient(ctx context.Context) (pb.AnalysisServiceClient, error) {
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

func (s *server) SendMetric(ctx context.Context, in *pb.Metric) (*pb.CollectorResponse, error) {
	log.Printf("Received metric from %s.", in.SourceClientId)

	analysisClient, err := s.getAnalysisClient(ctx)
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

	log.Printf("Metric forwarded successfully. Response: %s", analysisResp.Message)

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
	// --- MODIFICA 1: Converti la porta in un intero per la registrazione a Consul ---
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

	// --- MODIFICA 2: Registra il servizio a Consul all'avvio ---
	log.Println("Registrazione a Consul...")
	serviceID := fmt.Sprintf("%s-%s", serviceName, os.Getenv("HOSTNAME"))
	// NOTA: Usiamo il client restituito da RegisterService solo per la de-registrazione.
	// Il client per la service discovery viene creato separatamente sotto.
	consulClientForRegistration := consul.RegisterService(consulAddr, serviceName, serviceID, port)
	defer consul.DeregisterService(consulClientForRegistration, serviceID)
	log.Println("Registrato a Consul con successo.")

	// Creazione del client Consul per la Service Discovery (logica esistente)
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

	// Inizializza il server con il pool di connessioni e il client per la discovery
	pb.RegisterMetricsCollectorServer(s, &server{
		consulClient:        consulClientForDiscovery,
		analysisServiceName: analysisServiceName,
		analysisConns:       make(map[string]*grpc.ClientConn),
	})

	// --- MODIFICA 3: Registra il servizio di health check ---
	// Questo è fondamentale per permettere a Consul di verificare se il servizio è sano.
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
