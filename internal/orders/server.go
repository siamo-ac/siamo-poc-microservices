package orders

import (
	"encoding/json"
	"net/http"
)

// Server is the orders HTTP adapter (driving adapter). It translates HTTP
// into Service calls and domain errors into status codes.
type Server struct {
	svc    *Service
	client *HTTPInventoryClient
	mux    *http.ServeMux
}

// NewServer wires routes to the orders core.
func NewServer(svc *Service, client *HTTPInventoryClient) *Server {
	s := &Server{svc: svc, client: client, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("POST /orders", s.placeOrder)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":        "orders",
		"status":         "ok",
		"breaker":        s.client.BreakerState(),
		"bulkhead":       s.client.BulkheadStats(),
	})
}

type orderReq struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

func (s *Server) placeOrder(w http.ResponseWriter, r *http.Request) {
	var req orderReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	order, err := s.svc.PlaceOrder(r.Context(), req.SKU, req.Qty)
	if err != nil {
		status, msg := MapError(err)
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	writeJSON(w, http.StatusCreated, order)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
