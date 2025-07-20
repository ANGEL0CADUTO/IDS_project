from concurrent import futures
import logging
import os
import signal
import time
import grpc
import joblib
import numpy as np
from consul import Consul

# --- CORREZIONE 1: Aggiunta import di OpenTelemetry ---
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.grpc import GrpcInstrumentorServer
from opentelemetry.sdk.resources import Resource, SERVICE_NAME as SERVICE_NAME_KEY

# Importa le classi generate da .proto
import inference_pb2
import inference_pb2_grpc

# Importa le librerie per l'health check di gRPC
from grpc_health.v1 import health, health_pb2, health_pb2_grpc


# --- CORREZIONE 2: Funzione per inizializzare il tracer ---
def init_tracer(service_name):
    """Inizializza il provider di tracce OpenTelemetry che esporta a Jaeger."""
    jaeger_endpoint = os.getenv('JAEGER_ADDR', 'localhost:4317')

    resource = Resource(attributes={
        SERVICE_NAME_KEY: service_name
    })

    # Crea un exporter OTLP che punta a Jaeger
    trace_exporter = OTLPSpanExporter(
        endpoint=jaeger_endpoint,
        insecure=True  # Usare True perché comunichiamo all'interno della rete Docker senza TLS
    )

    # BatchSpanProcessor invia le tracce in batch, che è più efficiente
    processor = BatchSpanProcessor(trace_exporter)

    provider = TracerProvider(resource=resource)
    provider.add_span_processor(processor)

    # Imposta il provider globale, così l'instrumentazione sa dove inviare le tracce
    trace.set_tracer_provider(provider)
    logging.info(f"Tracer provider inizializzato per il servizio '{service_name}', esporta a {jaeger_endpoint}")


def register_to_consul(hostname, service_name, service_port):
    """Registra il servizio a Consul con retry."""
    consul_host = os.getenv('CONSUL_HOST', 'consul')
    consul_port = int(os.getenv('CONSUL_PORT', 8500))
    service_id = f"{service_name}-{hostname}"

    logging.info(f"Tentativo di connessione a Consul a {consul_host}:{consul_port}...")
    consul_client = Consul(host=consul_host, port=consul_port)

    check_config = {
        "GRPC": f"{hostname}:{service_port}",
        "GRPCUseTLS": False,
        "Interval": "10s",
        "DeregisterCriticalServiceAfter": "1m"
    }

    logging.info(f"Registrazione del servizio '{service_id}' su Consul...")
    for i in range(5):
        try:
            consul_client.agent.service.register(
                name=service_name,
                service_id=service_id,
                address=hostname,
                port=service_port,
                check=check_config
            )
            logging.info(f"Servizio '{service_id}' registrato con successo.")
            return consul_client, service_id
        except Exception as e:
            logging.warning(f"Tentativo {i+1} fallito: Impossibile connettersi a Consul. Riprovo... ({e})")
            time.sleep(3)

    raise RuntimeError("Impossibile registrare il servizio su Consul.")


def deregister_from_consul(consul_client, service_id):
    """De-registra il servizio da Consul."""
    if consul_client and service_id:
        logging.info(f"De-registrazione del servizio '{service_id}' da Consul...")
        consul_client.agent.service.deregister(service_id)
        logging.info("Servizio de-registrato.")


class InferenceService(inference_pb2_grpc.InferenceServicer):
    def __init__(self, model):
        self.model = model

    def Predict(self, request, context):
        try:
            features = np.array(request.features).reshape(1, -1)
            prediction = self.model.predict(features)
            result = int(prediction[0])
            return inference_pb2.InferenceResponse(prediction=result)
        except Exception as e:
            logging.error(f"Errore durante la predizione: {e}")
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details("Errore interno del server durante la predizione.")
            return inference_pb2.InferenceResponse()


def serve():
    """Funzione principale che avvia il server gRPC."""
    logging.info("Avvio del servizio di inferenza...")
    service_name = 'inference-service'

    # --- CORREZIONE 3: Inizializza il tracer e instrumenta il server gRPC ---
    init_tracer(service_name)
    grpc_server_instrumentor = GrpcInstrumentorServer()
    grpc_server_instrumentor.instrument()

    # Carica il modello
    try:
        model = joblib.load('isolation_forest_model.joblib')
        logging.info("Modello di inferenza caricato.")
    except Exception as e:
        logging.critical(f"Impossibile caricare il modello: {e}")
        return

    # Crea il server gRPC
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))

    # Aggiungi il servizio di inferenza
    inference_pb2_grpc.add_InferenceServicer_to_server(InferenceService(model), server)

    # Configura e aggiungi il servizio di health check
    health_servicer = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    # Imposta lo stato di salute per il nostro servizio
    health_servicer.set(service_name, health_pb2.HealthCheckResponse.SERVING)
    # Imposta anche lo stato di salute generico per le sonde che non specificano un servizio
    health_servicer.set("", health_pb2.HealthCheckResponse.SERVING)

    # Metti il server in ascolto
    service_port = int(os.getenv('GRPC_PORT', 5000))
    server.add_insecure_port(f'[::]:{service_port}')

    server.start()
    logging.info(f'Server gRPC in ascolto sulla porta {service_port}.')

    # Registra a Consul SOLO DOPO che il server è effettivamente partito
    hostname = os.getenv('HOSTNAME', 'localhost')
    consul_client, service_id = register_to_consul(hostname, service_name, service_port)

    # Gestisci la chiusura pulita
    def handle_shutdown(signum, frame):
        logging.info("Richiesta di spegnimento ricevuta, avvio chiusura pulita...")
        deregister_from_consul(consul_client, service_id)

        # --- CORREZIONE 4: Spegni la strumentazione prima di chiudere il server ---
        grpc_server_instrumentor.uninstrument()

        server.stop(5).wait()
        logging.info("Server fermato.")

    signal.signal(signal.SIGINT, handle_shutdown)
    signal.signal(signal.SIGTERM, handle_shutdown)

    server.wait_for_termination()


if __name__ == '__main__':
    logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
    serve()