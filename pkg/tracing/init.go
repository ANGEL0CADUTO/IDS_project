package tracing

import (
	"context"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// InitTracerProvider inizializza e registra un provider di tracce OpenTelemetry
// che esporta i dati a un collettore OTLP (come Jaeger).
func InitTracerProvider(ctx context.Context, serviceName, jaegerEndpoint string) (*sdktrace.TracerProvider, error) {
	// Definiamo la risorsa: un'etichetta che identifica tutte le tracce
	// provenienti da questo servizio specifico.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	// Creiamo una connessione gRPC verso Jaeger.
	conn, err := grpc.DialContext(ctx, jaegerEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, err
	}

	// Configuriamo l'esportatore di tracce che userà quella connessione gRPC.
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, err
	}

	// Creiamo il TracerProvider. È il cuore del sistema di tracing.
	// Utilizza un BatchSpanProcessor per inviare le tracce in batch,
	// che è più efficiente.
	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // Campiona tutte le tracce (ottimo per lo sviluppo)
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)

	// Impostiamo il nostro TracerProvider come provider globale per l'applicazione.
	otel.SetTracerProvider(tp)
	// Impostiamo anche i propagatori globali. Questo è FONDAMENTALE.
	// Dice a OpenTelemetry come iniettare e estrarre i dati di contesto
	// (come il Trace ID) dalle chiamate di rete, permettendo di collegare le tracce tra i servizi.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	log.Printf("Tracer provider initialized for service '%s', exporting to %s", serviceName, jaegerEndpoint)

	return tp, nil
}
