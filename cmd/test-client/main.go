package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
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
	buildCategoricalMaps(trainFilePath)

	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewMetricsCollectorClient(conn)

	file, err := os.Open(testFilePath)
	if err != nil {
		log.Fatalf("Impossibile aprire il file di test %s: %v", testFilePath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comment = '@'

	log.Printf("Inizio invio dati dal file di test: %s", testFilePath)

	for {
		record, err := reader.Read()
		if err == io.EOF {
			log.Println("Fine del file di test raggiunto. Il generatore si ferma.")
			break
		}
		if err != nil {
			log.Printf("Riga saltata nel file di test (errore CSV): %v", err)
			continue
		}

		features, err := recordToFeatures(record)
		if err != nil {
			log.Printf("Riga saltata (conversione fallita): %v", err)
			continue
		}

		metric := &pb.Metric{
			SourceClientId: "kdd-test-generator",
			Type:           "network_traffic",
			Value:          float64(features[4]),
			Timestamp:      time.Now().Unix(),
			Features:       features,
		}

		// La penultima colonna (indice 41) è l'etichetta
		label := "unknown"
		if len(record) > 41 {
			label = record[41]
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		// SALVIAMO LA RISPOSTA NELLA VARIABILE 'resp'
		resp, err := c.SendMetric(ctx, metric)
		if err != nil {
			log.Printf("could not send metric: %v", err)
		} else {
			// ...E STAMPIAMO TUTTO INSIEME!
			log.Printf("Invio record (Etichetta: %s) -> Predizione del sistema: %s", label, resp.Message)
		}
		cancel()

		time.Sleep(500 * time.Millisecond)
	}
}
