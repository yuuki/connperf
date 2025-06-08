package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rcrowley/go-metrics"
)

func TestPrintJSONLinesReport_SingleHost(t *testing.T) {
	metrics.DefaultRegistry = metrics.NewRegistry()
	defer func() {
		metrics.DefaultRegistry = metrics.NewRegistry()
	}()

	addr := "localhost:8080"
	timer := metrics.NewTimer()
	metrics.Register("total.latency."+addr, timer)

	timer.Update(100 * time.Microsecond)
	timer.Update(200 * time.Microsecond)
	timer.Update(300 * time.Microsecond)

	var buf bytes.Buffer
	printJSONLinesReport(&buf, []string{addr}, false)

	output := strings.TrimSpace(buf.String())
	if output == "" {
		t.Fatal("Expected JSON output, got empty string")
	}

	var result JSONLinesResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if result.Peer != addr {
		t.Errorf("Expected peer %s, got %s", addr, result.Peer)
	}

	if result.Count != 3 {
		t.Errorf("Expected count 3, got %d", result.Count)
	}

	if result.LatencyMax != 300 {
		t.Errorf("Expected max latency 300, got %d", result.LatencyMax)
	}

	if result.LatencyMin != 100 {
		t.Errorf("Expected min latency 100, got %d", result.LatencyMin)
	}

	if result.Timestamp == "" {
		t.Error("Expected timestamp to be set")
	}

	_, err := time.Parse(time.RFC3339, result.Timestamp)
	if err != nil {
		t.Errorf("Expected valid RFC3339 timestamp, got %s", result.Timestamp)
	}
}

func TestPrintJSONLinesReport_MultipleHosts(t *testing.T) {
	metrics.DefaultRegistry = metrics.NewRegistry()
	defer func() {
		metrics.DefaultRegistry = metrics.NewRegistry()
	}()

	addrs := []string{"localhost:8080", "localhost:8081"}

	for i, addr := range addrs {
		timer := metrics.NewTimer()
		metrics.Register("total.latency."+addr, timer)

		for j := 0; j < (i+1)*2; j++ {
			timer.Update(time.Duration(100*(j+1)) * time.Microsecond)
		}
	}

	var buf bytes.Buffer
	printJSONLinesReport(&buf, addrs, false)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 JSON lines, got %d", len(lines))
	}

	for i, line := range lines {
		var result JSONLinesResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("Failed to unmarshal JSON line %d: %v", i, err)
		}

		if result.Peer != addrs[i] {
			t.Errorf("Line %d: Expected peer %s, got %s", i, addrs[i], result.Peer)
		}

		expectedCount := int64((i + 1) * 2)
		if result.Count != expectedCount {
			t.Errorf("Line %d: Expected count %d, got %d", i, expectedCount, result.Count)
		}

		if result.Timestamp == "" {
			t.Errorf("Line %d: Expected timestamp to be set", i)
		}
	}
}

func TestPrintJSONLinesReport_MergedHosts(t *testing.T) {
	metrics.DefaultRegistry = metrics.NewRegistry()
	defer func() {
		metrics.DefaultRegistry = metrics.NewRegistry()
	}()

	addrs := []string{"localhost:8080", "localhost:8081"}
	timer := metrics.NewTimer()
	metrics.Register("total.latency", timer)

	timer.Update(100 * time.Microsecond)
	timer.Update(200 * time.Microsecond)
	timer.Update(300 * time.Microsecond)
	timer.Update(400 * time.Microsecond)

	var buf bytes.Buffer
	printJSONLinesReport(&buf, addrs, true)

	output := strings.TrimSpace(buf.String())
	lines := strings.Split(output, "\n")
	if len(lines) != 1 {
		t.Fatalf("Expected 1 JSON line for merged hosts, got %d", len(lines))
	}

	var result JSONLinesResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	expectedPeer := "merged(2 hosts)"
	if result.Peer != expectedPeer {
		t.Errorf("Expected peer %s, got %s", expectedPeer, result.Peer)
	}

	if result.Count != 4 {
		t.Errorf("Expected count 4, got %d", result.Count)
	}

	if result.LatencyMax != 400 {
		t.Errorf("Expected max latency 400, got %d", result.LatencyMax)
	}

	if result.LatencyMin != 100 {
		t.Errorf("Expected min latency 100, got %d", result.LatencyMin)
	}
}

func TestJSONLinesResultFields(t *testing.T) {
	metrics.DefaultRegistry = metrics.NewRegistry()
	defer func() {
		metrics.DefaultRegistry = metrics.NewRegistry()
	}()

	addr := "localhost:8080"
	timer := metrics.NewTimer()
	metrics.Register("total.latency."+addr, timer)

	testDurations := []time.Duration{
		50 * time.Microsecond,
		100 * time.Microsecond,
		150 * time.Microsecond,
		200 * time.Microsecond,
		250 * time.Microsecond,
		300 * time.Microsecond,
		350 * time.Microsecond,
		400 * time.Microsecond,
		450 * time.Microsecond,
		500 * time.Microsecond,
	}

	for _, d := range testDurations {
		timer.Update(d)
	}

	var buf bytes.Buffer
	printJSONLinesReport(&buf, []string{addr}, false)

	var result JSONLinesResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if result.Count != int64(len(testDurations)) {
		t.Errorf("Expected count %d, got %d", len(testDurations), result.Count)
	}

	if result.LatencyMax <= 0 {
		t.Errorf("Expected positive max latency, got %d", result.LatencyMax)
	}

	if result.LatencyMin <= 0 {
		t.Errorf("Expected positive min latency, got %d", result.LatencyMin)
	}

	if result.LatencyMean <= 0 {
		t.Errorf("Expected positive mean latency, got %d", result.LatencyMean)
	}

	if result.Latency90p <= 0 {
		t.Errorf("Expected positive 90p latency, got %d", result.Latency90p)
	}

	if result.Latency95p <= 0 {
		t.Errorf("Expected positive 95p latency, got %d", result.Latency95p)
	}

	if result.Latency99p <= 0 {
		t.Errorf("Expected positive 99p latency, got %d", result.Latency99p)
	}

	if result.Rate < 0 {
		t.Errorf("Expected non-negative rate, got %f", result.Rate)
	}

	expectedFields := []string{"peer", "count", "latency_max_us", "latency_min_us", "latency_mean_us", "latency_90p_us", "latency_95p_us", "latency_99p_us", "rate_per_sec", "timestamp"}

	var jsonMap map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &jsonMap); err != nil {
		t.Fatalf("Failed to unmarshal JSON to map: %v", err)
	}

	for _, field := range expectedFields {
		if _, exists := jsonMap[field]; !exists {
			t.Errorf("Expected field %s to exist in JSON output", field)
		}
	}
}
