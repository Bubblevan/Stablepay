package fiat

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func Handler(service *Service, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
			writeJSON(w, 200, map[string]string{"service": "payment-service-fiat", "mode": service.Mode()})
			return
		}
		if r.URL.Path == "/v1/fiat/alipay/notify" {
			if r.Method != http.MethodPost {
				http.Error(w, "failure", 405)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "failure", 400)
				return
			}
			params := map[string]string{}
			for key, values := range r.PostForm {
				if len(values) != 1 {
					http.Error(w, "failure", 400)
					return
				}
				params[key] = values[0]
			}
			if _, err := service.Notify(params); err != nil {
				http.Error(w, "failure", 400)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, "success")
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(token) < 24 || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path == "/v1/fiat/status" && r.Method == http.MethodGet {
			writeJSON(w, 200, map[string]any{"mode": service.Mode(), "ready": true, "currency": "CNY", "max_amount_minor": service.MaxAmountMinor})
			return
		}
		if r.URL.Path == "/v1/fiat/orders" && r.Method == http.MethodPost {
			var request CreateRequest
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&request); err != nil {
				http.Error(w, "invalid request", 400)
				return
			}
			if decoder.Decode(&struct{}{}) != io.EOF {
				http.Error(w, "invalid request", 400)
				return
			}
			order, err := service.Create(request)
			respond(w, order, err)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/fiat/orders/"), "/")
		if !strings.HasPrefix(r.URL.Path, "/v1/fiat/orders/") || len(parts) > 2 {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 1 && r.Method == http.MethodGet {
			order, err := service.Get(parts[0])
			respond(w, order, err)
			return
		}
		if len(parts) == 2 {
			switch {
			case parts[1] == "checkout" && r.Method == http.MethodPost:
				link, err := service.Checkout(parts[0])
				respond(w, map[string]any{"mode": service.Mode(), "cashier_url": link}, err)
				return
			case parts[1] == "reconcile" && r.Method == http.MethodPost:
				order, err := service.Reconcile(r.Context(), parts[0])
				respond(w, order, err)
				return
			case parts[1] == "simulate" && r.Method == http.MethodPost:
				order, err := service.Simulate(parts[0])
				respond(w, order, err)
				return
			}
		}
		http.Error(w, "method or route not allowed", 405)
	})
}
func respond(w http.ResponseWriter, value any, err error) {
	if err == nil {
		writeJSON(w, 200, value)
		return
	}
	status := 400
	if errors.Is(err, ErrNotFound) {
		status = 404
	}
	if errors.Is(err, ErrConflict) {
		status = 409
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
