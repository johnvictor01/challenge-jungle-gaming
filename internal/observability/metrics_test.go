package observability

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsExposeCountersAndLatency(t *testing.T) {
	metrics := NewMetrics()
	metrics.Inc("wager_result_processed")
	metrics.Observe("wager_processing_latency", 250*time.Millisecond)
	response := httptest.NewRecorder()
	metrics.ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body := response.Body.String()
	if !strings.Contains(body, "backend_wager_result_processed 1") || !strings.Contains(body, "backend_wager_processing_latency_count 1") || !strings.Contains(body, "backend_wager_processing_latency_sum_seconds 0.25") {
		t.Fatalf("metrics output missing observations: %s", body)
	}
}
