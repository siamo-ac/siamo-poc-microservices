// Package orders is the orders bounded context. It owns orders; everything
// it knows about inventory comes through the InventoryPort interface. The
// HTTP adapter in client.go implements that port, wrapping the real
// inventory service with a circuit breaker and a bulkhead.
package orders

import "context"

// InventoryPort is the seam between the two services: orders talks to
// inventory only through this interface. In production the adapter is the
// HTTP client; in tests it can be a fake.
type InventoryPort interface {
	// CheckStock returns nil when qty units of sku are available.
	CheckStock(ctx context.Context, sku string, qty int) error
	// ReserveStock decrements inventory for an order.
	ReserveStock(ctx context.Context, sku string, qty int) error
}
