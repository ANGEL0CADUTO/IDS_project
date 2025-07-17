package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	// Import del client ufficiale di InfluxDB
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"

	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	"google.golang.org/grpc"
)

// Le porte e gli indirizzi non sono più hard-coded
var (
	grpcPort           = getEnv("GRPC_PORT", "50052")
	influxURL          = getEnv("INFLUXDB_URL", "http://influxdb:8086")
	influxToken        = getEnv("INFLUXDB_TOKEN", "password123")
	influxOrg          = getEnv("INFLUXDB_ORG", "ids-project")
	influxBucket       = getEnv("INFLUXDB_BUCKET", "metrics")
	influxAlarmsBucket = getEnv("INFLUXDB_ALARMS_BUCKET", "alarms")
)

type server struct {
	pb.UnimplementedStorageServer
	influxWriteAPI       api.WriteAPI
	influxWriteAPIAlarms api.WriteAPI
}

// Implementazione di StoreMetric
func (s *server) StoreMetric(ctx context.Context, in *pb.Metric) (*pb.StorageResponse, error) {
	// Creiamo un "punto" dati nel formato richiesto da InfluxDB
	p := influxdb2.NewPointWithMeasurement(in.GetType()).
		AddTag("client_id", in.GetSourceClientId()).
		AddField("value", in.GetValue()).
		SetTime(time.Unix(in.GetTimestamp(), 0))

	// Scriviamo il punto. La scrittura è asincrona.
	s.influxWriteAPI.WritePoint(p)

	log.Printf("Stored metric from %s", in.GetSourceClientId())

	return &pb.StorageResponse{Success: true, Message: "Metric stored"}, nil
}

// Implementazione (vuota per ora) di StoreAlarm
func (s *server) StoreAlarm(ctx context.Context, in *pb.Alarm) (*pb.StorageResponse, error) {
	p := influxdb2.NewPointWithMeasurement("alarm").
		AddTag("rule_id", in.GetRuleId()).
		AddTag("client_id", in.GetClientId()).
		AddField("description", in.GetDescription()).
		SetTime(time.Unix(in.GetTimestamp(), 0))

	s.influxWriteAPIAlarms.WritePoint(p)

	log.Printf("Stored ALARM for client %s", in.GetClientId())
	return &pb.StorageResponse{Success: true, Message: "Alarm stored"}, nil
}

func main() {
	// Creazione del client InfluxDB
	client := influxdb2.NewClient(influxURL, influxToken)
	// Otteniamo un'API per la scrittura non bloccante (asincrona)
	writeAPI := client.WriteAPI(influxOrg, influxBucket)
	writeAPIAlarms := client.WriteAPI(influxOrg, influxAlarmsBucket)

	// Gestione degli errori di scrittura in background
	go func() {
		for err := range writeAPI.Errors() {
			log.Printf("InfluxDB write error: %s\n", err.Error())
		}
	}()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", grpcPort))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	// Registriamo la nostra implementazione, passandogli le API di scrittura
	pb.RegisterStorageServer(s, &server{
		influxWriteAPI:       writeAPI,
		influxWriteAPIAlarms: writeAPIAlarms,
	})

	log.Printf("Storage service listening at %v", lis.Addr())

	// Pulizia finale: fluttua tutti i dati bufferizzati prima di chiudere.
	defer client.Close()
	defer writeAPI.Flush()

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

// Funzione helper per leggere le variabili d'ambiente con un valore di default.
// Questo è un primo passo verso la configurazione esterna.
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
