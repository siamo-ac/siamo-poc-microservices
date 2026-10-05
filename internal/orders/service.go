package orders

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/siamosystems/siamo-poc-microservices/internal/resilience"
)

// Order is the orders bounded context's own record. It stores only order
// facts; stock numbers live in the inventory service, never here — that is
// the decomposition seam.
type Order struct {
	ID  string `json:"id"`
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

// Service is the orders core: place an order by checking stock, reserving
// it, then recording the order. It depends only on the InventoryPort.
type Service struct {
	inventory InventoryPort
	mu        sync.Mutex
	orders    map[string]Order
	seq       atomic.Int64
}

// NewService wires the core to its inventory port.
func NewService(inv InventoryPort) *Service {
	return &Service{inventory: inv, orders: map[string]Order{}}
}

// PlaceOrder checks stock then reserves it, recording the order on success.
func (s *Service) PlaceOrder(ctx context.Context, sku string, qty int) (Order, error) {
	if qty <= 0 {
		return Order{}, fmt.Errorf("qty must be positive")
	}
	if err := s.inventory.CheckStock(ctx, sku, qty); err != nil {
		return Order{}, fmt.Errorf("stock check failed: %w", err)
	}
	if err := s.inventory.ReserveStock(ctx, sku, qty); err != nil {
		return Order{}, fmt.Errorf("reservation failed: %w", err)
	}
	id := fmt.Sprintf("ord-%d", s.seq.Add(1))
	o := Order{ID: id, SKU: sku, Qty: qty}
	s.mu.Lock()
	s.orders[id] = o
	s.mu.Unlock()
	log.Printf("[orders] placed %s (%d x %s)", id, qty, sku)
	return o, nil
}

// MapError converts a domain error into an HTTP status + message for the
// driving adapter.
func MapError(err error) (int, string) {
	switch {
	case errors.Is(err, resilience.ErrOpen):
		return 503, "inventory circuit breaker is OPEN: downstream unhealthy, request refused fast"
	case errors.Is(err, resilience.ErrBulkheadFull):
		return 503, "inventory bulkhead is full: too many concurrent downstream calls, request refused fast"
	case IsBusinessError(err):
		return 409, err.Error()
	default:
		return 502, err.Error()
	}
}
