package main

import (
	"context"
	"log"
	"net"

	// Importa il package gRPC
	"google.golang.org/grpc"

	// Importa il package generato dai file .proto
	// Assicurati che il percorso corrisponda al tuo go.mod
	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
)

// Definiamo una porta costante per il server.
// In futuro la sposteremo in un file di configurazione.
const (
	port = ":50051"
)

// 'server' è la nostra implementazione dell'interfaccia MetricsCollectorServer.
// Deve includere pb.UnimplementedMetricsCollectorServer per compatibilità futura.
type server struct {
	pb.UnimplementedMetricsCollectorServer
}

// SendMetric è l'implementazione del nostro RPC.
// Riceve una metrica da un client e restituisce una risposta.
func (s *server) SendMetric(ctx context.Context, in *pb.Metric) (*pb.CollectorResponse, error) {
	// Per ora, ci limitiamo a stampare un log per confermare la ricezione.
	log.Printf("Received metric: ClientID=%s, Type=%s, Value=%.2f", in.GetSourceClientId(), in.GetType(), in.GetValue())

	// In un sistema reale, qui inoltreremmo il dato all'Analysis e allo Storage service.
	// Per ora, simuliamo una risposta di successo.
	return &pb.CollectorResponse{Accepted: true, Message: "Metric received successfully"}, nil
}

func main() {
	// 1. Crea un listener TCP sulla porta definita.
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// 2. Crea una nuova istanza del server gRPC.
	s := grpc.NewServer()

	// 3. Registra la nostra implementazione del servizio ('server') con il server gRPC.
	pb.RegisterMetricsCollectorServer(s, &server{})
	log.Printf("Collector service listening at %v", lis.Addr())

	// 4. Avvia il server gRPC in modo che inizi ad accettare connessioni sul listener.
	// s.Serve() è un'operazione bloccante.
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
