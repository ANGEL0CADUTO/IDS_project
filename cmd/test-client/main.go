package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// I percorsi dei file rimangono costanti perché sono legati al codice
const (
	trainFilePath = "KDDTrain+.txt"
	testFilePath  = "KDDTest+.txt"
)

var (
	protocolMap = make(map[string]float32)
	serviceMap  = make(map[string]float32)
	flagMap     = make(map[string]float32)
)

// Le funzioni buildCategoricalMaps e recordToFeatures rimangono invariate...
func buildCategoricalMaps(filePath string) {
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Impossibile aprire il file: %v", filePath)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.Comment = '@'
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) < 4 {
			continue
		}
		if _, exists := protocolMap[record[1]]; !exists {
			protocolMap[record[1]] = float32(len(protocolMap))
		}
		if _, exists := serviceMap[record[2]]; !exists {
			serviceMap[record[2]] = float32(len(serviceMap))
		}
		if _, exists := flagMap[record[3]]; !exists {
			flagMap[record[3]] = float32(len(flagMap))
		}
	}
}

func recordToFeatures(record []string) ([]float32, error) {
	if len(record) < 41 {
		return nil, fmt.Errorf("record con meno di 41 feature: %d", len(record))
	}
	features := make([]float32, 41)
	for i := 0; i < 41; i++ {
		var val float64
		var err error
		switch i {
		case 1:
			features[i] = protocolMap[record[i]]
		case 2:
			features[i] = serviceMap[record[i]]
		case 3:
			features[i] = flagMap[record[i]]
		default:
			val, err = strconv.ParseFloat(strings.TrimSpace(record[i]), 32)
			if err == nil {
				features[i] = float32(val)
			}
		}
	}
	return features, nil
}

func main() {
	// --- GESTIONE DEI PARAMETRI TRAMITE FLAG ---
	collectorAddr := flag.String("addr", "localhost:50051", "Indirizzo del collector-service (es. 'localhost:50051' o 'IP_EC2:50051')")
	mode := flag.String("mode", "benign", "Modalità di esecuzione: 'benign' o 'malicious'")
	numClients := flag.Int("clients", 5, "Numero di client concorrenti da avviare")
	recordsPerClient := flag.Int("records", 200, "Numero di record che ogni client invierà (0 per infinito)")
	delayMs := flag.Int("delay", 500, "Pausa media in millisecondi tra un invio e l'altro")
	flag.Parse()

	log.Printf("--- Avvio Data Generator ---")
	log.Printf("Target: %s | Modalità: %s | Client: %d | Record per client: %d", *collectorAddr, *mode, *numClients, *recordsPerClient)

	buildCategoricalMaps(trainFilePath)
	var wg sync.WaitGroup

	for i := 1; i <= *numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			isMaliciousClient := (*mode == "malicious")
			log.Printf("[Client %d] Avvio in modalità %s.", clientID, strings.ToUpper(*mode))

			time.Sleep(time.Duration(rand.Intn(500)) * time.Millisecond)

			conn, err := grpc.Dial(*collectorAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				log.Printf("[Client %d] Errore di connessione: %v", clientID, err)
				return
			}
			defer conn.Close()
			c := pb.NewMetricsCollectorClient(conn)

			file, err := os.Open(testFilePath)
			if err != nil {
				log.Printf("[Client %d] Impossibile aprire file: %v", clientID, err)
				return
			}
			defer file.Close()
			reader := csv.NewReader(file)
			reader.Comment = '@'

			recordsSent := 0
			for {
				record, err := reader.Read()
				if err == io.EOF {
					file.Seek(0, 0)
					reader = csv.NewReader(file)
					continue
				}
				if err != nil {
					continue
				}

				label := "unknown"
				if len(record) > 41 {
					label = record[41]
				}

				if (isMaliciousClient && label == "normal") || (!isMaliciousClient && label != "normal") {
					continue
				}

				features, err := recordToFeatures(record)
				if err != nil {
					continue
				}

				metric := &pb.Metric{
					SourceClientId: fmt.Sprintf("concurrent-client-%d", clientID),
					Type:           "network_traffic",
					Timestamp:      time.Now().Unix(),
					Features:       features,
				}

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				resp, err := c.SendMetric(ctx, metric)
				if err != nil {
					log.Printf("[Client %d] Impossibile inviare metrica: %v", clientID, err)
				} else {
					log.Printf("[Client %d] Invio record (Etichetta: %s) -> Risposta: %s", clientID, label, resp.Message)
				}
				cancel()

				recordsSent++
				if *recordsPerClient > 0 && recordsSent >= *recordsPerClient {
					break
				}

				// Pausa variabile intorno al valore specificato
				time.Sleep(time.Duration(*delayMs/2+rand.Intn(*delayMs)) * time.Millisecond)
			}
			log.Printf("[Client %d] Invio completato.", clientID)
		}(i)
	}
	wg.Wait()
	log.Println("Tutti i client hanno terminato.")
}
