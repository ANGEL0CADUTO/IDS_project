package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/ANGEL0CADUTO/IDS_project/pkg/consul"
	"github.com/ANGEL0CADUTO/IDS_project/pkg/tracing"
	pb "github.com/ANGEL0CADUTO/IDS_project/proto"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

var (
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

// --- StoreMetric salva metrica ---
func (s *server) StoreMetric(ctx context.Context, in *pb.Metric) (*pb.StorageResponse, error) {
	// Creiamo UN SOLO punto per la metrica, identificato dal suo timestamp e client_id
	p := influxdb2.NewPointWithMeasurement(in.Type).
		AddTag("client_id", in.SourceClientId).
		SetTime(time.Unix(in.Timestamp, 0))

	// Controlliamo se la metrica ha le feature complete
	if len(in.Features) == 41 {
		// --- MODIFICA CHIAVE ---
		// Aggiungiamo OGNI feature come un CAMPO SEPARATO allo STESSO punto.
		// Diamo loro nomi significativi per poterli usare nelle query.
		p.AddField("duration", float64(in.Features[0]))
		p.AddField("protocol_type", float64(in.Features[1]))
		p.AddField("service", float64(in.Features[2]))
		p.AddField("flag", float64(in.Features[3]))
		p.AddField("src_bytes", float64(in.Features[4]))
		p.AddField("dst_bytes", float64(in.Features[5]))
		// ... potremmo aggiungerle tutte e 41, ma queste sono sufficienti per i test
		p.AddField("count", float64(in.Features[22]))
		p.AddField("srv_count", float64(in.Features[23]))
		p.AddField("serror_rate", float64(in.Features[24]))
		p.AddField("srv_serror_rate", float64(in.Features[25]))
	} else {
		// Fallback per le metriche semplici senza le 41 feature
		p.AddField("value", in.Value)
	}

	// Scriviamo il punto singolo (che ora contiene più campi)
	s.influxWriteAPI.WritePoint(p)
	log.Printf("Stored metric from %s", in.SourceClientId)
	return &pb.StorageResponse{Success: true, Message: "Metric stored"}, nil
}

// --- StoreAlarm salva allarme ---
func (s *server) StoreAlarm(ctx context.Context, in *pb.Alarm) (*pb.StorageResponse, error) {
	// Creiamo UN SOLO punto per l'allarme
	p := influxdb2.NewPointWithMeasurement("alarm").
		AddTag("rule_id", in.RuleId). // Aggiungiamo la regola come TAG per poter raggruppare!
		AddTag("client_id", in.ClientId).
		SetTime(time.Unix(in.Timestamp, 0))

	// Aggiungiamo i dettagli dell'allarme come CAMPI
	p.AddField("description", in.Description)

	// Se l'allarme contiene la metrica che l'ha scatenato, salviamo anche alcune sue feature
	if in.TriggerMetric != nil && len(in.TriggerMetric.Features) > 0 {
		p.AddField("trigger_src_bytes", float64(in.TriggerMetric.Features[4]))
		p.AddField("trigger_count", float64(in.TriggerMetric.Features[22]))
		p.AddField("trigger_serror_rate", float64(in.TriggerMetric.Features[24]))
	} else if in.TriggerMetric != nil {
		p.AddField("trigger_value", in.TriggerMetric.Value)
	}

	// Scriviamo il punto singolo nel bucket degli allarmi
	s.influxWriteAPIAlarms.WritePoint(p)

	log.Printf("Stored ALARM for client %s, rule %s", in.ClientId, in.RuleId)
	return &pb.StorageResponse{Success: true, Message: "Alarm stored"}, nil
}

func main() {
	// --- Configurazione degli indirizzi dei servizi ---
	consulAddr := getEnv("CONSUL_ADDR", "localhost:8500")
	portStr := getEnv("GRPC_PORT", "50052")
	port, _ := strconv.Atoi(portStr)
	jaegerAddr := getEnv("JAEGER_ADDR", "localhost:4317")
	serviceName := "storage-service"

	// --- Inizializzazione del Tracer Provider di OpenTelemetry ---
	// Lo aggiungiamo anche qui per coerenza e per preparare il terreno
	// per la strumentazione completa di questo servizio.
	tp, err := tracing.InitTracerProvider(context.Background(), serviceName, jaegerAddr)
	if err != nil {
		log.Fatalf("Failed to initialize tracer provider: %v", err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	// --- Registrazione a Consul (invariata) ---
	serviceID := fmt.Sprintf("%s-%s", serviceName, os.Getenv("HOSTNAME"))
	consulClient := consul.RegisterService(consulAddr, serviceName, serviceID, port)
	defer consul.DeregisterService(consulClient, serviceID)

	// --- Configurazione del client InfluxDB (invariata) ---
	client := influxdb2.NewClient(influxURL, influxToken)
	writeAPI := client.WriteAPI(influxOrg, influxBucket)
	writeAPIAlarms := client.WriteAPI(influxOrg, influxAlarmsBucket)
	defer client.Close()
	defer writeAPI.Flush()
	defer writeAPIAlarms.Flush()

	go func() {
		for err := range writeAPI.Errors() {
			log.Printf("InfluxDB write error: %s\n", err.Error())
		}
		for err := range writeAPIAlarms.Errors() {
			log.Printf("InfluxDB (alarms) write error: %s\n", err.Error())
		}
	}()

	// --- Creazione del Listener di rete (invariata) ---
	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", portStr))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// --- Creazione del server gRPC con l'Interceptor Condizionale ---
	// Questa è la modifica chiave: l'interceptor di tracing viene applicato
	// a tutte le chiamate, TRANNE a quella di health check di Consul.
	s := grpc.NewServer(
		grpc.UnaryInterceptor(
			tracing.NewConditionalUnaryInterceptor(
				otelgrpc.UnaryServerInterceptor(),
				"/grpc.health.v1.Health/Check", // Nome del metodo da saltare
			),
		),
	)

	// Registrazione dei servizi sul server gRPC (invariata)
	pb.RegisterStorageServer(s, &server{
		influxWriteAPI:       writeAPI,
		influxWriteAPIAlarms: writeAPIAlarms,
	})
	grpc_health_v1.RegisterHealthServer(s, health.NewServer())

	// Avvio del server
	log.Printf("Storage service listening at %v", lis.Addr())
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
