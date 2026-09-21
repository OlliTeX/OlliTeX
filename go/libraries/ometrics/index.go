package ometrics

import (
	"encoding/json"
	"net/http"
)

// MetricsHandler mirrors index.injectMetricsRoute: an http.Handler serving the
// current metric snapshot as JSON (the express app route in Node). The oracle
// does not exercise the route, so this is a thin Go binding for parity.
func MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(DefaultRegistry.GetMetricsAsJSON())
	})
}
