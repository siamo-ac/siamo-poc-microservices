// Command inventory runs the inventory bounded context on :8082.
package main

import (
	"log"
	"net/http"

	"github.com/siamosystems/siamo-poc-microservices/internal/inventory"
)

func main() {
	stock := inventory.NewStock()
	stock.Seed()
	srv := inventory.NewServer(stock)
	log.Println("[inventory] listening on :8082")
	if err := http.ListenAndServe(":8082", srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
