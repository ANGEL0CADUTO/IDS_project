package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/ANGEL0CADUTO/IDS_project/pkg/consul"
	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	consulapi "github.com/hashicorp/consul/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// La struct del server ora include un contatore per il Round-Robin.
// Usiamo un uint32 per operazioni atomiche sicure tra goroutine.
type server struct {
	pb.UnimplementedMetricsCollectorServer
	consulClient         *consulapi.Client
	analysisServiceName  string
	nextAnalysisInstance uint32 // Contatore per il Round-Robin
}

// SendMetric ora implementa la logica di load balancing.
func (s *server) SendMetric(ctx context.Context, in *pb.Metric) (*pb.CollectorResponse, error) {
	log.Printf("Received metric from %s. Discovering analysis services...", in.SourceClientId)

	// 1. SCOPERTA DINAMICA: Ad ogni chiamata, chiediamo a Consul la lista aggiornata
	//    delle istanze sane del servizio di analisi.
	analysisAddrs, err := consul.DiscoverAllServices(s.consulClient, s.analysisServiceName)
	if err != nil {
		log.Printf("FATAL: Could not discover any analysis service: %v", err)
		return &pb.CollectorResponse{Accepted: false, Message: "Upstream analysis service unavailable"}, err
	}
	if len(analysisAddrs) == 0 {
		log.Printf("FATAL: No healthy analysis service instances found.")
		return &pb.CollectorResponse{Accepted: false, Message: "No healthy upstream analysis service"}, fmt.Errorf("no healthy instances found")
	}

	// 2. LOGICA ROUND-ROBIN: Selezioniamo l'indirizzo della prossima istanza da usare.
	//    L'operazione atomica garantisce che anche con molte richieste concorrenti
	//    il contatore venga incrementato in modo sicuro.
	instanceIndex := atomic.AddUint32(&s.nextAnalysisInstance, 1) % uint32(len(analysisAddrs))
	targetAddr := analysisAddrs[instanceIndex]
	log.Printf("Forwarding metric to analysis instance #%d at %s", instanceIndex, targetAddr)

	// 3. CONNESSIONE e CHIAMATA: Stabiliamo una connessione "usa e getta" con l'istanza scelta.
	//    Questo è più semplice e resiliente di un pool di connessioni per il nostro caso d'uso.
	conn, err := grpc.Dial(targetAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		log.Printf("ERROR: Failed to connect to analysis service at %s: %v", targetAddr, err)
		return &pb.CollectorResponse{Accepted: false, Message: "Failed to connect to upstream service"}, err
	}
	defer conn.Close()

	analysisClient := pb.NewAnalysisServiceClient(conn)

	outCtx, cancel := context.WithTimeout(context.Background(), time.Second*5) // Aumentato leggermente il timeout
	defer cancel()

	analysisResp, err := analysisClient.AnalyzeMetric(outCtx, in)
	if err != nil {
		log.Printf("ERROR: could not forward metric to analysis service %s: %v", targetAddr, err)
		return &pb.CollectorResponse{Accepted: false, Message: "Failed to forward metric to " + targetAddr}, err
	}

	log.Printf("Metric forwarded successfully to %s. Response: %s", targetAddr, analysisResp.Message)

	return &pb.CollectorResponse{
		Accepted: analysisResp.Processed,
		Message:  analysisResp.Message,
	}, nil
}

func main() {
	consulAddr := getEnv("CONSUL_ADDR", "localhost:8500")

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

	s := grpc.NewServer()

	// Inizializziamo il server con le informazioni necessarie
	pb.RegisterMetricsCollectorServer(s, &server{
		consulClient:        consulClient,
		analysisServiceName: analysisServiceName,
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
