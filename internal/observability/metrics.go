package observability

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Metrics keeps bounded-cardinality counters used by HTTP and background workers.
type Metrics struct {
	mu       sync.Mutex
	counters map[string]uint64
	latency  map[string]durationSummary
}

type durationSummary struct {
	count uint64
	sum   time.Duration
}

func NewMetrics() *Metrics {
	return &Metrics{counters: map[string]uint64{}, latency: map[string]durationSummary{}}
}

var Default = NewMetrics()

func (m *Metrics) Inc(name string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.counters[name]++
	m.mu.Unlock()
}

func (m *Metrics) Observe(name string, duration time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	entry := m.latency[name]
	entry.count++
	entry.sum += duration
	m.latency[name] = entry
	m.mu.Unlock()
}

func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	counters := make(map[string]uint64, len(m.counters))
	for key, value := range m.counters {
		counters[key] = value
	}
	latencies := make(map[string]durationSummary, len(m.latency))
	for key, value := range m.latency {
		latencies[key] = value
	}
	m.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	keys := make([]string, 0, len(counters))
	for key := range counters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		_, _ = fmt.Fprintf(w, "%s %d\n", metricName(key), counters[key])
	}
	keys = keys[:0]
	for key := range latencies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry := latencies[key]
		name := metricName(key)
		_, _ = fmt.Fprintf(w, "%s_count %d\n%s_sum_seconds %g\n", name, entry.count, name, entry.sum.Seconds())
	}
}

func metricName(name string) string {
	name = strings.ToLower(name)
	var result strings.Builder
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_' {
			result.WriteRune(char)
		} else {
			result.WriteByte('_')
		}
	}
	return "backend_" + result.String()
}
