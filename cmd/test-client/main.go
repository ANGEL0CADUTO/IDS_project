package main

import (
	"context"
	"encoding/csv"
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

const (
	address       = "localhost:50051"
	trainFilePath = "KDDTrain+.txt"
	testFilePath  = "KDDTest+.txt"
)

var (
	protocolMap = make(map[string]float32)
	serviceMap  = make(map[string]float32)
	flagMap     = make(map[string]float32)
)

func buildCategoricalMaps(filePath string) {
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Impossibile aprire il file di training %s per costruire le mappe: %v", filePath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// Salta l'header ARFF se presente
	reader.Comment = '@'

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if len(record) < 4 {
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
	log.Println("Mappe per le feature categoriche costruite con successo dal dataset di training.")
}

func recordToFeatures(record []string) ([]float32, error) {
	// I dati utili sono sempre le prime 41 colonne
	if len(record) < 41 {
		return nil, fmt.Errorf("record con meno di 41 feature: %d", len(record))
	}

	features := make([]float32, 41)

	for i := 0; i < 41; i++ {
		var val float64
		var err error

		switch i {
		case 1:
			if v, ok := protocolMap[record[i]]; ok {
				features[i] = v
			}
		case 2:
			if v, ok := serviceMap[record[i]]; ok {
				features[i] = v
			}
		case 3:
			if v, ok := flagMap[record[i]]; ok {
				features[i] = v
			}
		default:
			val, err = strconv.ParseFloat(strings.TrimSpace(record[i]), 32)
			if err != nil {
				features[i] = 0
			} else {
				features[i] = float32(val)
			}
		}
	}
	return features, nil
}

func main() {
	const numClients = 5
	buildCategoricalMaps(trainFilePath)
	var wg sync.WaitGroup

	for i := 1; i <= numClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			log.Printf("[Client %d] Avvio...", clientID)

			// ... (connessione gRPC rimane uguale)
			conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				log.Printf("[Client %d] Errore di connessione: %v", clientID, err)
				return
			}
			defer conn.Close()
			c := pb.NewMetricsCollectorClient(conn)

			file, err := os.Open(testFilePath)
			if err != nil {
				log.Printf("[Client %d] Impossibile aprire il file di test %s: %v", clientID, testFilePath, err)
				return
			}
			defer file.Close()
			reader := csv.NewReader(file)
			reader.Comment = '@'

			// --- INIZIO LOGICA MODIFICATA ---

			// Definiamo quali client sono "rumorosi"
			isNoisyClient := (clientID == 1 || clientID == 3) // Ad esempio, il client 1 e 3

			for j := 0; j < 10000; j++ {
				record, err := reader.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					continue
				}

				label := "unknown"
				if len(record) > 41 {
					label = record[41]
				}

				// Logica di invio differenziata
				// I client rumorosi inviano sempre i dati etichettati come attacchi.
				// Gli altri client inviano preferenzialmente i dati etichettati come normali.
				if isNoisyClient {
					// Se questo client è rumoroso, cerca un record di attacco da inviare
					if label == "normal" {
						continue // Salta questo record e passa al prossimo
					}
				} else {
					// Se questo client è "benigno", cerca un record normale da inviare
					if label != "normal" {
						continue // Salta questo record e passa al prossimo
					}
				}

				features, err := recordToFeatures(record)
				if err != nil {
					continue
				}

				sourceID := fmt.Sprintf("concurrent-client-%d", clientID)
				metric := &pb.Metric{
					SourceClientId: sourceID,
					Type:           "network_traffic",
					Value:          float64(features[4]),
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

				time.Sleep(time.Duration(500+rand.Intn(500)) * time.Millisecond)
			}
			log.Printf("[Client %d] Invio completato.", clientID)
			// --- FINE LOGICA MODIFICATA ---
		}(i)
	}

	wg.Wait()
	log.Println("Tutti i client hanno terminato.")
}
