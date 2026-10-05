// Package resilience holds the resilience patterns used across the POCs.
// (Core types live in breaker.go; this file adds the bulkhead.)
package resilience

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"
)

// ErrBulkheadFull is returned when no worker slot frees up within the
// acquisition timeout: the bulkhead fails fast instead of queueing forever.
var ErrBulkheadFull = fmt.Errorf("bulkhead full: no worker slot available")

// Bulkhead bounds how many calls may hit a downstream at the same time. It
// is a semaphore with a bounded wait: callers that cannot get a slot within
// acquireTimeout are rejected so one slow downstream cannot swallow every
// goroutine of the calling service (the classic bulkhead: the ship takes
// water in one compartment but stays afloat).
type Bulkhead struct {
	name           string
	sem            chan struct{}
	acquireTimeout time.Duration
	inFlight       atomic.Int64
	maxSeen        atomic.Int64
	rejected       atomic.Int64
}

// NewBulkhead builds a bulkhead allowing at most maxConcurrent calls in
// flight; callers wait at most acquireTimeout for a slot.
func NewBulkhead(name string, maxConcurrent int, acquireTimeout time.Duration) *Bulkhead {
	return &Bulkhead{
		name:           name,
		sem:            make(chan struct{}, maxConcurrent),
		acquireTimeout: acquireTimeout,
	}
}

// Acquire takes a slot, blocking up to the acquire timeout. Callers must
// call Release once they hold a slot.
func (b *Bulkhead) Acquire(ctx context.Context) error {
	timer := time.NewTimer(b.acquireTimeout)
	defer timer.Stop()
	select {
	case b.sem <- struct{}{}:
		n := b.inFlight.Add(1)
		for {
			max := b.maxSeen.Load()
			if n <= max || b.maxSeen.CompareAndSwap(max, n) {
				break
			}
		}
		log.Printf("[bulkhead %s] slot acquired (in_flight=%d)", b.name, n)
		return nil
	case <-timer.C:
		b.rejected.Add(1)
		log.Printf("[bulkhead %s] REJECTED: no slot within %s (rejected_total=%d)",
			b.name, b.acquireTimeout, b.rejected.Load())
		return ErrBulkheadFull
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns a slot to the pool.
func (b *Bulkhead) Release() {
	b.inFlight.Add(-1)
	<-b.sem
}

// Stats is a snapshot for /health.
type BulkheadStats struct {
	InFlight int64 `json:"in_flight"`
	MaxSeen  int64 `json:"max_in_flight"`
	Rejected int64 `json:"rejected_total"`
}

// Stats returns current bulkhead metrics.
func (b *Bulkhead) Stats() BulkheadStats {
	return BulkheadStats{
		InFlight: b.inFlight.Load(),
		MaxSeen:  b.maxSeen.Load(),
		Rejected: b.rejected.Load(),
	}
}
