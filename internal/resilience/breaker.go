// Package resilience holds the two stability patterns this POC demonstrates:
// a circuit breaker and a bulkhead. Both are stdlib-only and hand-rolled so
// the demo can show exactly how they work, without hiding behavior inside a
// dependency.
package resilience

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// BreakerState is one of the three circuit-breaker states.
type BreakerState int

const (
	Closed BreakerState = iota
	Open
	HalfOpen
)

func (s BreakerState) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Breaker is a classic circuit breaker. While closed, calls pass through and
// consecutive failures are counted; once maxFailures consecutive failures
// happen the breaker opens and calls fail fast without touching the
// downstream. After cooldown the breaker lets one trial call through
// (half-open); if the trial succeeds the breaker closes, otherwise it opens
// again. Every state transition is logged so the demo can show it.
type Breaker struct {
	name        string
	maxFailures int
	cooldown    time.Duration

	mu        sync.Mutex
	state     BreakerState
	failures  int
	openedAt  time.Time
	trialLive atomic.Bool
}

// NewBreaker builds a breaker that opens after maxFailures consecutive
// failures and allows a trial call after cooldown.
func NewBreaker(name string, maxFailures int, cooldown time.Duration) *Breaker {
	return &Breaker{name: name, maxFailures: maxFailures, cooldown: cooldown}
}

// ErrOpen is returned when the breaker is open and the call is refused
// without touching the downstream.
var ErrOpen = fmt.Errorf("circuit breaker open: call refused without touching downstream")

// Execute runs fn through the breaker.
func (b *Breaker) Execute(fn func() error) error {
	b.mu.Lock()
	switch b.state {
	case Open:
		if time.Since(b.openedAt) < b.cooldown {
			b.mu.Unlock()
			return ErrOpen
		}
		// Cooldown elapsed: go half-open and allow a single trial call.
		b.setStateLocked(HalfOpen, "cooldown elapsed, allowing trial call")
		if !b.trialLive.CompareAndSwap(false, true) {
			// Another goroutine is already running the trial; fail fast.
			b.mu.Unlock()
			return ErrOpen
		}
		b.mu.Unlock()
		err := fn()
		b.trialLive.Store(false)
		b.mu.Lock()
		if err != nil {
			b.setStateLocked(Open, fmt.Sprintf("trial call failed: %v", err))
			b.mu.Unlock()
			return err
		}
		b.failures = 0
		b.setStateLocked(Closed, "trial call succeeded")
		b.mu.Unlock()
		return nil
	case HalfOpen:
		// Should not normally happen (single trial is fenced by trialLive),
		// but fail safe rather than stacking calls on a sick downstream.
		b.mu.Unlock()
		return ErrOpen
	}
	b.mu.Unlock()

	err := fn()

	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		b.failures++
		if b.state == Closed && b.failures >= b.maxFailures {
			b.setStateLocked(Open, fmt.Sprintf("%d consecutive failures", b.failures))
		}
		return err
	}
	b.failures = 0
	return nil
}

// State returns the current breaker state (for /health).
func (b *Breaker) State() BreakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Breaker) setStateLocked(next BreakerState, reason string) {
	if b.state == next {
		return
	}
	prev := b.state
	b.state = next
	if next == Open {
		b.openedAt = time.Now()
	}
	log.Printf("[breaker %s] %s -> %s (%s)", b.name, prev, next, reason)
}
