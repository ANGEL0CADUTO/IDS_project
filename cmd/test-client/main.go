package main

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	// Importa il package generato, come nel server
	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
)

// Indirizzo del server collector. Corrisponde alla porta nel codice del server.
const (
	address = "localhost:50051"
)

func main() {
	// 1. Stabilisce una connessione con il server gRPC.
	// Usiamo `insecure.NewCredentials()` perché non stiamo usando TLS/SSL.
	// `WithBlock()` fa sì che il Dial sia sincrono e blocchi fino alla connessione.
	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	// Chiudiamo la connessione quando la funzione main termina.
	defer conn.Close()

	// 2. Crea uno "stub" del client dal codice generato.
	// Questo oggetto contiene i metodi per chiamare gli RPC del servizio.
	c := pb.NewMetricsCollectorClient(conn)

	// 3. Crea un contesto per la nostra chiamata RPC.
	// Un contesto può trasportare deadline, informazioni di cancellazione e altri valori.
	// Qui impostiamo un timeout di 1 secondo.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// 4. Esegue la chiamata RPC `SendMetric`.
	// Creiamo un oggetto Metric da inviare, popolandolo con dati di esempio.
	r, err := c.SendMetric(ctx, &pb.Metric{
		SourceClientId: "test-client-01",
		Type:           "cpu_usage",
		Value:          88.5,
		Timestamp:      time.Now().Unix(),
	})
	if err != nil {
		log.Fatalf("could not send metric: %v", err)
	}

	// 5. Stampa la risposta ricevuta dal server.
	log.Printf("Server Response: Accepted=%t, Message='%s'", r.GetAccepted(), r.GetMessage())
}
