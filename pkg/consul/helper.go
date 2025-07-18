package consul

import (
	"fmt"
	"log"
	"os"
	"time"

	consulapi "github.com/hashicorp/consul/api"
)

// RegisterService si registra al Consul locale
func RegisterService(consulAddr, serviceName, serviceID string, servicePort int) *consulapi.Client {
	config := consulapi.DefaultConfig()
	config.Address = consulAddr
	client, err := consulapi.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create consul client: %v", err)
	}

	// NOTA: In un ambiente containerizzato, l'hostname è un indirizzo affidabile
	// per la comunicazione interna alla rete Docker.
	hostname, _ := os.Hostname()
	serviceAddr := hostname

	registration := &consulapi.AgentServiceRegistration{
		ID:      serviceID,
		Name:    serviceName,
		Port:    servicePort,
		Address: serviceAddr,
		Check: &consulapi.AgentServiceCheck{
			GRPC:                           fmt.Sprintf("%s:%d", serviceAddr, servicePort),
			Interval:                       "10s",
			Timeout:                        "1s",
			DeregisterCriticalServiceAfter: "1m",
		},
	}

	err = client.Agent().ServiceRegister(registration)
	if err != nil {
		log.Fatalf("Failed to register service with consul: %v", err)
	}

	log.Printf("Successfully registered service '%s' with ID '%s' to Consul at %s:%d", serviceName, serviceID, serviceAddr, servicePort)
	return client
}

// DiscoverService è una funzione di convenienza che usa DiscoverAllServices e restituisce solo il primo indirizzo.
// Utile per i servizi che necessitano di connettersi a un singleton.
func DiscoverService(client *consulapi.Client, serviceName string) (string, error) {
	addrs, err := DiscoverAllServices(client, serviceName)
	if err != nil {
		return "", err
	}
	return addrs[0], nil
}

// DiscoverAllServices interroga Consul per trovare TUTTI gli indirizzi sani di un servizio.
// Questa è la funzione chiave per il load balancing.
func DiscoverAllServices(client *consulapi.Client, serviceName string) ([]string, error) {
	var lastErr error
	// Ciclo di retry per attendere che almeno un servizio sia disponibile
	for i := 0; i < 5; i++ {
		// Il terzo parametro 'true' filtra per i soli servizi 'passing' (sani)
		services, _, err := client.Health().Service(serviceName, "", true, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to query consul for service '%s': %w", serviceName, err)
			log.Printf("Retrying discovery for '%s' in 2 seconds...", serviceName)
			time.Sleep(2 * time.Second)
			continue
		}

		if len(services) > 0 {
			var addrs []string
			for _, service := range services {
				// Costruiamo l'indirizzo usando l'Address del servizio, che Consul
				// sa risolvere correttamente all'interno della rete Docker.
				addr := fmt.Sprintf("%s:%d", service.Service.Address, service.Service.Port)
				addrs = append(addrs, addr)
			}
			log.Printf("Successfully discovered %d healthy instances for service '%s': %v", len(addrs), serviceName, addrs)
			return addrs, nil
		}

		lastErr = fmt.Errorf("no healthy instances found for service '%s'", serviceName)
		log.Printf("Retrying discovery for '%s' in 2 seconds...", serviceName)
		time.Sleep(2 * time.Second)
	}

	return nil, lastErr
}

// DeregisterService si deregistra da Consul
func DeregisterService(client *consulapi.Client, serviceID string) {
	err := client.Agent().ServiceDeregister(serviceID)
	if err != nil {
		log.Printf("Warning: failed to deregister service '%s': %v", serviceID, err)
	} else {
		log.Printf("Successfully deregistered service '%s'", serviceID)
	}
}
