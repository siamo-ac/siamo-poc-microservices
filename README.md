# siamo-poc-microservices

**Concept, in plain language.** A monolith is one program that does everything;
a microservice architecture splits the program into small services that each
own one business area ("bounded context") and talk to each other over the
network. This POC has two:

- **orders** (`:8081`) — owns orders. It knows nothing about stock levels.
- **inventory** (`:8082`) — owns stock levels and reservations. It knows
  nothing about orders.

The seam between them is plain HTTP/JSON, defined by one Go interface
(`orders.InventoryPort`). Because every call crosses a network, two classic
stability patterns guard it:

- **Circuit breaker** — when inventory starts failing, orders stops calling
  it for a while instead of hammering a sick downstream and timing out every
  customer request. States: `closed` (normal) → `open` (failing fast) →
  `half-open` (one trial call) → `closed` again when healthy.
- **Bulkhead** — at most 3 calls to inventory may be in flight at once. When
  inventory gets slow, extra requests are rejected immediately (503) instead
  of queueing up and exhausting orders' resources. (Named after ship
  bulkheads: one flooded compartment shouldn't sink the ship.)

Layout is hexagonal-light: `cmd/` holds the two service mains,
`internal/` holds the domain cores (`orders`, `inventory`), the resilience
patterns (`resilience`), and the HTTP adapters. The breaker and bulkhead are
hand-rolled in stdlib so the demo shows exactly how they work.

## Run the demo

```bash
cd ~/workspace/services/pocs/siamo-poc-microservices
chmod +x scripts/demo.sh
./scripts/demo.sh
```

Or by hand:

```bash
go build -o bin/inventory ./cmd/inventory
go build -o bin/orders ./cmd/orders
./bin/inventory &          # :8082
./bin/orders &            # :8081 (INVENTORY_BASE env overrides the default)

# healthy order
curl -X POST localhost:8081/orders -d '{"sku":"widget","qty":2}'

# make inventory 100% flaky, then hammer orders and watch the breaker trip
curl -X POST "localhost:8082/admin/flakiness?rate=1.0"
for i in 1 2 3 4 5; do curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8081/orders -d '{"sku":"widget","qty":1}'; done

# heal, wait out the 5s cooldown, send one trial order -> half-open -> closed
curl -X POST "localhost:8082/admin/flakiness?rate=0"
sleep 6
curl -X POST localhost:8081/orders -d '{"sku":"gadget","qty":1}'

# bulkhead: slow inventory down, fire a burst
curl -X POST "localhost:8082/admin/flakiness?rate=0&latency=800ms"
for i in 1 2 3 4 5 6; do (curl -s -o /dev/null -w "burst-$i: %{http_code}\n" -X POST localhost:8081/orders -d '{"sku":"sprocket","qty":1}' &); done; wait

# always-on observability
curl localhost:8081/health   # breaker state + bulkhead in_flight/max/rejected
curl localhost:8082/health
```

## What to observe

1. **Breaker transitions in the orders log.** After 3 consecutive inventory
   failures you see `[breaker inventory] closed -> open (3 consecutive
   failures)`; further orders return **503 immediately** without touching
   inventory (fail fast). After healing and the 5s cooldown:
   `open -> half-open (cooldown elapsed, allowing trial call)`, then
   `half-open -> closed (trial call succeeded)`.
2. **Bulkhead capping concurrency.** In the burst, 3 orders succeed and 3 get
   503; the log shows `[bulkhead inventory] REJECTED: no slot within 300ms`;
   `GET /health` reports `"max_in_flight":3` (never exceeded) and
   `"rejected_total":3`.
3. **The seam.** `orders` never imports inventory's domain package — the only
   contract is `InventoryPort` + HTTP. Each service has its own `main` and can
   be deployed, scaled, and restarted independently.

## Honest limits

- **POC — not production hardening.** No retries with backoff, no request
  timeouts per downstream call beyond the HTTP client default, no metrics
  exporter, no distributed tracing, no auth between services.
- The flakiness knobs (`/admin/flakiness`) are demo tooling, not something a
  real service would expose.
- Stock reservations are not idempotent and there is no saga/compensating
  transaction if `reserve` succeeds but order recording later fails (in this
  POC it can't, but a real split would need one).
- In-memory stock: restart inventory and the catalog resets (seeded on boot).
- The breaker counts any 5xx/transport error as a failure; 4xx business
  answers (e.g. insufficient stock) deliberately do not trip it.
