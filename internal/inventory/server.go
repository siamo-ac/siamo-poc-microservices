package inventory

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Server is the inventory HTTP adapter. Ports: the domain (Stock) is the
// core; this file is the driving adapter exposing it over HTTP.
type Server struct {
	stock *Stock
	mux   *http.ServeMux
}

// NewServer wires routes to the stock core.
func NewServer(stock *Stock) *Server {
	s := &Server{stock: stock, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("POST /admin/seed", s.seed)
	s.mux.HandleFunc("POST /admin/flakiness", s.flakiness)
	s.mux.HandleFunc("POST /check", s.check)
	s.mux.HandleFunc("POST /reserve", s.reserve)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

type itemReq struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"service": "inventory", "status": "ok"})
}

func (s *Server) seed(w http.ResponseWriter, _ *http.Request) {
	s.stock.Seed()
	writeJSON(w, http.StatusOK, map[string]string{"status": "seeded"})
}

func (s *Server) flakiness(w http.ResponseWriter, r *http.Request) {
	rate, _ := strconv.ParseFloat(r.URL.Query().Get("rate"), 64)
	lat, _ := time.ParseDuration(r.URL.Query().Get("latency"))
	s.stock.SetFlakiness(rate, lat)
	writeJSON(w, http.StatusOK, map[string]any{"fail_rate": rate, "latency": lat.String()})
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	var req itemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	ok, err := s.stock.Check(req.SKU, req.Qty)
	if err != nil {
		log.Printf("[inventory] /check FAILED sku=%s qty=%d err=%v", req.SKU, req.Qty, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sku": req.SKU, "qty": req.Qty, "available": ok})
}

func (s *Server) reserve(w http.ResponseWriter, r *http.Request) {
	var req itemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if err := s.stock.Reserve(req.SKU, req.Qty); err != nil {
		status := http.StatusInternalServerError
		if err == errInsufficient {
			status = http.StatusConflict
		}
		log.Printf("[inventory] /reserve FAILED sku=%s qty=%d err=%v", req.SKU, req.Qty, err)
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sku": req.SKU, "qty": req.Qty, "reserved": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
