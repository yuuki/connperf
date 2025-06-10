package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/rcrowley/go-metrics"
)

func toMicroseconds(n int64) int64 {
	return time.Duration(n).Microseconds()
}

func toMicrosecondsf(n float64) int64 {
	return time.Duration(n).Microseconds()
}

type Printer struct {
	writer io.Writer
}

func NewPrinter(w io.Writer) *Printer {
	return &Printer{writer: w}
}

func (p *Printer) PrintStatHeader() {
	fmt.Fprintf(p.writer, "%-20s %-10s %-15s %-15s %-15s %-15s %-15s %-15s %-10s\n",
		"PEER", "CNT", "LAT_MAX(µs)", "LAT_MIN(µs)", "LAT_MEAN(µs)",
		"LAT_90p(µs)", "LAT_95p(µs)", "LAT_99p(µs)", "RATE(/s)")
}

func (p *Printer) PrintStatLine(addr string, stat metrics.Timer) {
	fmt.Fprintf(p.writer, "%-20s %-10d %-15d %-15d %-15d %-15d %-15d %-15d %-10.2f\n",
		addr,
		stat.Count(),
		toMicroseconds(stat.Max()),
		toMicroseconds(stat.Min()),
		toMicrosecondsf(stat.Mean()),
		toMicrosecondsf(stat.Percentile(0.9)),
		toMicrosecondsf(stat.Percentile(0.95)),
		toMicrosecondsf(stat.Percentile(0.99)),
		stat.RateMean(),
	)
}

func (p *Printer) PrintReport(addrs []string, mergeResultsEachHost bool) {
	fmt.Fprintln(p.writer, "--- A result during total execution time ---")
	if mergeResultsEachHost {
		ts := getOrRegisterTimer("total.latency", "", mergeResultsEachHost)
		p.PrintStatLine(fmt.Sprintf("merged(%d hosts)", len(addrs)), ts)
		return
	}
	for _, addr := range addrs {
		ts := getOrRegisterTimer("total.latency", addr, mergeResultsEachHost)
		p.PrintStatLine(addr, ts)
	}
}

type JSONLinesResult struct {
	Peer        string  `json:"peer"`
	Count       int64   `json:"count"`
	LatencyMax  int64   `json:"latency_max_us"`
	LatencyMin  int64   `json:"latency_min_us"`
	LatencyMean int64   `json:"latency_mean_us"`
	Latency90p  int64   `json:"latency_90p_us"`
	Latency95p  int64   `json:"latency_95p_us"`
	Latency99p  int64   `json:"latency_99p_us"`
	Rate        float64 `json:"rate_per_sec"`
	Timestamp   string  `json:"timestamp"`
}

func (p *Printer) PrintJSONLinesReport(addrs []string, mergeResultsEachHost bool) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	results := []JSONLinesResult{}

	if mergeResultsEachHost {
		ts := getOrRegisterTimer("total.latency", "", mergeResultsEachHost)
		results = append(results, JSONLinesResult{
			Peer:        fmt.Sprintf("merged(%d hosts)", len(addrs)),
			Count:       ts.Count(),
			LatencyMax:  toMicroseconds(ts.Max()),
			LatencyMin:  toMicroseconds(ts.Min()),
			LatencyMean: toMicrosecondsf(ts.Mean()),
			Latency90p:  toMicrosecondsf(ts.Percentile(0.9)),
			Latency95p:  toMicrosecondsf(ts.Percentile(0.95)),
			Latency99p:  toMicrosecondsf(ts.Percentile(0.99)),
			Rate:        ts.RateMean(),
			Timestamp:   timestamp,
		})
	} else {
		for _, addr := range addrs {
			ts := getOrRegisterTimer("total.latency", addr, mergeResultsEachHost)
			results = append(results, JSONLinesResult{
				Peer:        addr,
				Count:       ts.Count(),
				LatencyMax:  toMicroseconds(ts.Max()),
				LatencyMin:  toMicroseconds(ts.Min()),
				LatencyMean: toMicrosecondsf(ts.Mean()),
				Latency90p:  toMicrosecondsf(ts.Percentile(0.9)),
				Latency95p:  toMicrosecondsf(ts.Percentile(0.95)),
				Latency99p:  toMicrosecondsf(ts.Percentile(0.99)),
				Rate:        ts.RateMean(),
				Timestamp:   timestamp,
			})
		}
	}

	for _, result := range results {
		if err := json.NewEncoder(p.writer).Encode(result); err != nil {
			fmt.Fprintf(p.writer, "Error encoding JSON result: %v\n", err)
		}
	}
}
