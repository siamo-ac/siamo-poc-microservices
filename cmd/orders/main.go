// Command orders runs the orders bounded context on :8081. It talks to the
// inventory service (default http://localhost:8082) only through the
// InventoryPort, protected by a circuit breaker and a bulkhead.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/siamosystems/siamo-poc-microservices/internal/orders"
)

func main() {
	invBase := os.Getenv("INVENTORY_BASE")
	if invBase == "" {
		invBase = "http://localhost:8082"
	}
	// Breaker opens after 3 consecutive downstream failures; a trial call is
	// allowed 5s after opening. Bulkhead allows 3 concurrent downstream calls.
	client := orders.NewHTTPInventoryClient(invBase, 3, 3, 5*time.Second)
	svc := orders.NewService(client)
	srv := orders.NewServer(svc, client)
	log.Println("[orders] listening on :8081, inventory at", invBase)
	if err := http.ListenAndServe(":8081", srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
