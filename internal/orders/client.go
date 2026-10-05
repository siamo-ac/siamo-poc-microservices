package orders

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/siamosystems/siamo-poc-microservices/internal/resilience"
)

// HTTPInventoryClient is the driven adapter for InventoryPort: it calls the
// real inventory service over HTTP, with two resilience patterns in front:
//
//	bulkhead -> circuit breaker -> HTTP call
//
// The bulkhead bounds how many calls may be in flight at once (the rest fail
// fast); the breaker stops calling a downstream that is clearly sick.
type HTTPInventoryClient struct {
	base    string
	http    *http.Client
	breaker *resilience.Breaker
	bulk    *resilience.Bulkhead
}

// NewHTTPInventoryClient builds the client. maxConcurrent bounds in-flight
// calls to inventory; breaker opens after maxFailures consecutive failures
// and allows a trial call after cooldown.
func NewHTTPInventoryClient(base string, maxConcurrent int, maxFailures int, cooldown time.Duration) *HTTPInventoryClient {
	return &HTTPInventoryClient{
		base:    base,
		http:    &http.Client{Timeout: 5 * time.Second},
		breaker: resilience.NewBreaker("inventory", maxFailures, cooldown),
		bulk:    resilience.NewBulkhead("inventory", maxConcurrent, 300*time.Millisecond),
	}
}

// BreakerState exposes the current breaker state for /health.
func (c *HTTPInventoryClient) BreakerState() string { return c.breaker.State().String() }

// BulkheadStats exposes bulkhead metrics for /health.
func (c *HTTPInventoryClient) BulkheadStats() resilience.BulkheadStats { return c.bulk.Stats() }

func (c *HTTPInventoryClient) CheckStock(ctx context.Context, sku string, qty int) error {
	return c.call(ctx, "/check", sku, qty)
}

func (c *HTTPInventoryClient) ReserveStock(ctx context.Context, sku string, qty int) error {
	return c.call(ctx, "/reserve", sku, qty)
}

type itemPayload struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

func (c *HTTPInventoryClient) call(ctx context.Context, path, sku string, qty int) error {
	if err := c.bulk.Acquire(ctx); err != nil {
		return err // bulkhead full: fail fast, don't even reach the breaker
	}
	defer c.bulk.Release()

	var bizErr error
	err := c.breaker.Execute(func() error {
		body, _ := json.Marshal(itemPayload{SKU: sku, Qty: qty})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 500 {
			return fmt.Errorf("inventory %s -> %d: %s", path, resp.StatusCode, string(respBody))
		}
		if resp.StatusCode >= 400 {
			// 4xx is a business answer (e.g. insufficient stock), not a
			// downstream outage: record it without tripping the breaker.
			bizErr = businessError(fmt.Sprintf("inventory %s -> %d: %s", path, resp.StatusCode, string(respBody)))
		}
		return nil
	})
	if err != nil {
		return err
	}
	return bizErr
}

// businessError marks an error as "not a downstream failure" so the caller
// can distinguish it from outages. (A fuller design would use typed errors
// from the inventory contract; the POC keeps it small.)
type businessError string

func (e businessError) Error() string { return string(e) }

// IsBusinessError reports whether err is a business answer, not an outage.
func IsBusinessError(err error) bool {
	_, ok := err.(businessError)
	return ok
}
