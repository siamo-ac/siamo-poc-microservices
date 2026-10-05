// Package inventory is the inventory bounded context: it owns stock levels
// and reservations, and exposes them over HTTP. It knows nothing about
// orders; the seam between the two services is pure HTTP/JSON.
package inventory

import (
	"log"
	"math/rand"
	"sync"
	"time"
)

// Stock is the inventory write model: SKU -> units on hand.
type Stock struct {
	mu     sync.Mutex
	levels map[string]int

	// Demo knobs (set via /admin/flakiness):
	failRate float64       // probability a request fails outright
	latency  time.Duration // artificial latency per request
}

// NewStock returns seeded demo stock.
func NewStock() *Stock {
	return &Stock{levels: map[string]int{}}
}

// Seed loads the demo catalog.
func (s *Stock) Seed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.levels = map[string]int{"widget": 100, "gadget": 50, "sprocket": 25}
	log.Printf("[inventory] seeded stock: %v", s.levels)
}

// SetFlakiness configures the simulated unreliability used by the demo.
func (s *Stock) SetFlakiness(rate float64, latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failRate = rate
	s.latency = latency
	log.Printf("[inventory] flakiness set: fail_rate=%.2f latency=%s", rate, latency)
}

// maybeFlake sleeps for the configured latency and, with probability
// failRate, returns true meaning "pretend the downstream just died".
func (s *Stock) maybeFlake() bool {
	s.mu.Lock()
	rate, latency := s.failRate, s.latency
	s.mu.Unlock()
	if latency > 0 {
		time.Sleep(latency)
	}
	return rand.Float64() < rate //nolint:gosec // demo randomness
}

// Check reports whether qty units of sku are available.
func (s *Stock) Check(sku string, qty int) (bool, error) {
	if s.maybeFlake() {
		return false, errSimulated
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.levels[sku] >= qty, nil
}

// Reserve decrements stock. It is idempotent per reservation id in spirit,
// but this POC keeps it simple: no duplicate detection.
func (s *Stock) Reserve(sku string, qty int) error {
	if s.maybeFlake() {
		return errSimulated
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.levels[sku] < qty {
		return errInsufficient
	}
	s.levels[sku] -= qty
	log.Printf("[inventory] reserved %d x %s (remaining=%d)", qty, sku, s.levels[sku])
	return nil
}

var (
	errSimulated   = errorString("simulated downstream failure")
	errInsufficient = errorString("insufficient stock")
)

type errorString string

func (e errorString) Error() string { return string(e) }
