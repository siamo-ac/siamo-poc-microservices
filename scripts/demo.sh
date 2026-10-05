#!/usr/bin/env bash
# Demo: circuit breaker + bulkhead between the orders and inventory services.
# Builds both binaries, starts them, then drives the failure story end to end.
set -u
cd "$(dirname "$0")/.."

echo "=== 1. Build ==="
go build -o bin/inventory ./cmd/inventory
go build -o bin/orders ./cmd/orders

echo "=== 2. Start inventory (:8082) and orders (:8081) ==="
./bin/inventory > /tmp/poc1-inventory.log 2>&1 &
INV_PID=$!
./bin/orders > /tmp/poc1-orders.log 2>&1 &
ORD_PID=$!
trap 'kill $INV_PID $ORD_PID 2>/dev/null' EXIT
sleep 1

echo "=== 3. Healthy order (breaker closed) ==="
curl -s -X POST localhost:8081/orders -d '{"sku":"widget","qty":2}'; echo
curl -s localhost:8081/health; echo

echo
echo "=== 4. Make inventory 100% flaky, hammer orders ==="
curl -s -X POST "localhost:8082/admin/flakiness?rate=1.0"; echo
for i in 1 2 3 4 5; do
  printf "attempt %d -> " "$i"
  curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8081/orders -d '{"sku":"widget","qty":1}'
done
echo "breaker state now: $(curl -s localhost:8081/health | grep -o '"breaker":"[a-z-]*"')"
echo "Watch it in the log:"
grep '\[breaker' /tmp/poc1-orders.log | tail -3

echo
echo "=== 5. Heal inventory, wait out the 5s cooldown, one trial order ==="
curl -s -X POST "localhost:8082/admin/flakiness?rate=0"; echo
sleep 6
curl -s -X POST localhost:8081/orders -d '{"sku":"gadget","qty":1}'; echo
echo "breaker state now: $(curl -s localhost:8081/health | grep -o '"breaker":"[a-z-]*"')"
grep '\[breaker' /tmp/poc1-orders.log | tail -3

echo
echo "=== 6. Bulkhead: slow inventory to 800ms, fire 6 concurrent orders ==="
curl -s -X POST "localhost:8082/admin/flakiness?rate=0&latency=800ms"; echo
pids=()
for i in 1 2 3 4 5 6; do
  curl -s -o /dev/null -w "burst-$i: %{http_code}\n" -X POST localhost:8081/orders -d '{"sku":"sprocket","qty":1}' &
  pids+=($!)
done
wait "${pids[@]}"
sleep 1
curl -s localhost:8081/health; echo
echo "REJECTED lines in log:"
grep -c 'REJECTED' /tmp/poc1-orders.log
curl -s -X POST "localhost:8082/admin/flakiness?rate=0&latency=0s" > /dev/null

echo
echo "=== Done. Full logs: /tmp/poc1-orders.log /tmp/poc1-inventory.log ==="
