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

// --- INIZIO BLOCCO CORRETTO ---

// DiscoverService interroga Consul per trovare l'indirizzo di un altro servizio.
// Utile per i servizi che necessitano di connettersi a un singleton.
func DiscoverService(client *consulapi.Client, serviceName string) (string, error) {
	const maxRetries = 15
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		// La funzione Health().Service() restituisce una lista di istanze sane
		services, _, err := client.Health().Service(serviceName, "", true, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to query consul for service '%s': %w", serviceName, err)
			log.Printf("Retrying discovery for '%s' in 2 seconds... (Attempt %d/%d)", serviceName, i+1, maxRetries)
			time.Sleep(2 * time.Second)
			continue
		}

		// Se troviamo almeno un'istanza sana, prendiamo la prima e usciamo.
		if len(services) > 0 {
			// 'service' qui è di tipo *api.ServiceEntry
			service := services[0].Service
			addr := fmt.Sprintf("%s:%d", service.Address, service.Port)
			log.Printf("Successfully discovered service '%s' at %s", serviceName, addr)
			return addr, nil
		}

		lastErr = fmt.Errorf("no healthy instances found for service '%s'", serviceName)
		log.Printf("Retrying discovery for '%s' in 2 seconds... (Attempt %d/%d)", serviceName, i+1, maxRetries)
		time.Sleep(2 * time.Second)
	}

	return "", lastErr
}

// DiscoverAllServices interroga Consul per trovare TUTTI gli indirizzi sani di un servizio.
// Questa è la funzione chiave per il load balancing.
func DiscoverAllServices(client *consulapi.Client, serviceName string) ([]string, error) {
	const maxRetries = 15
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		services, _, err := client.Health().Service(serviceName, "", true, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to query consul for service '%s': %w", serviceName, err)
			log.Printf("Retrying discovery for '%s' in 2 seconds... (Attempt %d/%d)", serviceName, i+1, maxRetries)
			time.Sleep(2 * time.Second)
			continue
		}

		if len(services) > 0 {
			var addrs []string
			// Qui cicliamo su TUTTE le istanze trovate
			for _, serviceEntry := range services {
				// 'serviceEntry' è di tipo *api.ServiceEntry, quindi accediamo a .Service
				service := serviceEntry.Service
				addr := fmt.Sprintf("%s:%d", service.Address, service.Port)
				addrs = append(addrs, addr)
			}
			log.Printf("Successfully discovered %d healthy instances for service '%s': %v", len(addrs), serviceName, addrs)
			return addrs, nil
		}

		lastErr = fmt.Errorf("no healthy instances found for service '%s'", serviceName)
		log.Printf("Retrying discovery for '%s' in 2 seconds... (Attempt %d/%d)", serviceName, i+1, maxRetries)
		time.Sleep(2 * time.Second)
	}

	return nil, lastErr
}

// --- FINE BLOCCO CORRETTO ---

// DeregisterService si deregistra da Consul
func DeregisterService(client *consulapi.Client, serviceID string) {
	err := client.Agent().ServiceDeregister(serviceID)
	if err != nil {
		log.Printf("Warning: failed to deregister service '%s': %v", serviceID, err)
	} else {
		log.Printf("Successfully deregistered service '%s'", serviceID)
	}
}
