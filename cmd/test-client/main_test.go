package main

import (
	"reflect"
	"testing"
)

// TestRecordToFeatures è la nostra funzione di test.
func TestRecordToFeatures(t *testing.T) {
	// Fase 1: Setup
	// Inizializziamo le mappe categoriche con valori noti per avere un test prevedibile.
	// Non chiamiamo buildCategoricalMaps() per non dipendere da un file esterno.
	protocolMap = map[string]float32{"tcp": 1.0, "udp": 2.0}
	serviceMap = map[string]float32{"http": 10.0, "ftp": 20.0}
	flagMap = map[string]float32{"SF": 0.0, "REJ": 1.0}

	// Fase 2: Definire i casi di test
	// Creiamo una struct per rendere i casi di test più leggibili.
	testCases := []struct {
		name          string    // Nome del caso di test
		inputRecord   []string  // L'input da passare alla funzione
		expectedFeats []float32 // L'output che ci aspettiamo
		expectError   bool      // Ci aspettiamo un errore?
	}{
		{
			name: "Record valido e completo",
			// Un record di 41+ elementi con valori numerici e categorici noti
			inputRecord:   []string{"0", "tcp", "http", "SF", "491", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "2", "2", "0.00", "0.00", "0.00", "0.00", "1.00", "0.00", "0.00", "150", "25", "0.17", "0.03", "0.17", "0.00", "0.00", "0.00", "0.05", "0.00", "normal"},
			expectedFeats: []float32{0, 1.0, 10.0, 0.0, 491, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 2, 0, 0, 0, 0, 1, 0, 0, 150, 25, 0.17, 0.03, 0.17, 0, 0, 0, 0.05, 0},
			expectError:   false,
		},
		{
			name:          "Record troppo corto",
			inputRecord:   []string{"1", "2", "3"}, // Meno di 41 elementi
			expectedFeats: nil,
			expectError:   true,
		},
		{
			name: "Valori categorici non mappati",
			// 'icmp' e 'other' non sono nelle nostre mappe di test
			inputRecord: []string{"0", "icmp", "other", "S0", "10", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "2", "2", "0.00", "0.00", "0.00", "0.00", "1.00", "0.00", "0.00", "150", "25", "0.17", "0.03", "0.17", "0.00", "0.00", "0.00", "0.05", "0.00", "normal"},
			// La funzione dovrebbe usare 0 per i valori non trovati
			expectedFeats: []float32{0, 0, 0, 0, 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 2, 0, 0, 0, 0, 1, 0, 0, 150, 25, 0.17, 0.03, 0.17, 0, 0, 0, 0.05, 0},
			expectError:   false,
		},
	}

	// Fase 3: Eseguire i test
	for _, tc := range testCases {
		// t.Run permette di raggruppare i test e dà un output più chiaro
		t.Run(tc.name, func(t *testing.T) {
			// Eseguiamo la funzione che vogliamo testare
			actualFeats, err := recordToFeatures(tc.inputRecord)

			// Fase 4: Verificare i risultati
			if tc.expectError {
				// Se ci aspettavamo un errore...
				if err == nil {
					t.Errorf("atteso un errore, ma non ne ho ricevuto nessuno")
				}
			} else {
				// Se non ci aspettavamo un errore...
				if err != nil {
					t.Errorf("non atteso un errore, ma ne ho ricevuto uno: %v", err)
				}
				// Confrontiamo l'output effettivo con quello atteso.
				// reflect.DeepEqual è il modo corretto per confrontare slice e mappe.
				if !reflect.DeepEqual(actualFeats, tc.expectedFeats) {
					t.Errorf("output non corretto:\nAtteso: %v\nRicevuto: %v", tc.expectedFeats, actualFeats)
				}
			}
		})
	}
}
