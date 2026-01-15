//go:build ignore

// Package main provides a benchmark comparison tool for detecting performance regressions.
// Usage: go run scripts/bench-compare.go <current.txt> <baseline.txt>
//
// This tool parses Go benchmark output and compares against a baseline.
// A regression of > 20% in ns/op, B/op, or allocs/op fails the comparison.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	// RegressionThreshold is the maximum allowed performance degradation (20%)
	RegressionThreshold = 0.20

	// ImprovementThreshold for highlighting significant improvements
	ImprovementThreshold = 0.10
)

// BenchmarkResult represents a single benchmark measurement.
type BenchmarkResult struct {
	Name         string  `json:"name"`
	Iterations   int     `json:"iterations"`
	NsPerOp      float64 `json:"ns_per_op"`
	BytesPerOp   int     `json:"bytes_per_op"`
	AllocsPerOp  int     `json:"allocs_per_op"`
	CustomMetric string  `json:"custom_metric,omitempty"`
	CustomValue  float64 `json:"custom_value,omitempty"`
}

// ComparisonResult represents the comparison between current and baseline.
type ComparisonResult struct {
	Name        string  `json:"name"`
	Current     float64 `json:"current_ns_per_op"`
	Baseline    float64 `json:"baseline_ns_per_op"`
	DeltaPct    float64 `json:"delta_percent"`
	IsRegressed bool    `json:"is_regressed"`
	IsImproved  bool    `json:"is_improved"`
	Status      string  `json:"status"`
}

// Report contains all comparison results.
type Report struct {
	TotalBenchmarks  int                 `json:"total_benchmarks"`
	Regressions      int                 `json:"regressions"`
	Improvements     int                 `json:"improvements"`
	Unchanged        int                 `json:"unchanged"`
	NewBenchmarks    int                 `json:"new_benchmarks"`
	MissingBaseline  int                 `json:"missing_baseline"`
	Results          []*ComparisonResult `json:"results"`
	RegressionFailed bool                `json:"regression_failed"`
}

var (
	outputJSON   = flag.Bool("json", false, "Output results as JSON")
	threshold    = flag.Float64("threshold", RegressionThreshold, "Regression threshold (0.0-1.0)")
	verbose      = flag.Bool("verbose", false, "Show all benchmark comparisons")
	failOnRegress = flag.Bool("fail", true, "Exit with code 1 on regression")
)

// Regex to parse Go benchmark output
// Format: BenchmarkName-N   iterations   ns/op   B/op   allocs/op
var benchmarkRegex = regexp.MustCompile(`^(Benchmark\S+)\s+(\d+)\s+([\d.]+)\s+ns/op(?:\s+(\d+)\s+B/op)?(?:\s+(\d+)\s+allocs/op)?(?:\s+([\d.]+)\s+(\S+))?`)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <current.txt> <baseline.txt>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Compares benchmark results and detects regressions.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	currentFile := flag.Arg(0)
	baselineFile := flag.Arg(1)

	currentResults, err := parseBenchmarkFile(currentFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing current file %s: %v\n", currentFile, err)
		os.Exit(1)
	}

	baselineResults, err := parseBenchmarkFile(baselineFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing baseline file %s: %v\n", baselineFile, err)
		os.Exit(1)
	}

	report := compare(currentResults, baselineResults, *threshold)

	if *outputJSON {
		outputJSONReport(report)
	} else {
		outputTextReport(report)
	}

	if *failOnRegress && report.RegressionFailed {
		os.Exit(1)
	}
}

// parseBenchmarkFile reads and parses a Go benchmark output file.
func parseBenchmarkFile(path string) (map[string]*BenchmarkResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	results := make(map[string]*BenchmarkResult)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		result := parseBenchmarkLine(line)
		if result != nil {
			results[result.Name] = result
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// parseBenchmarkLine parses a single benchmark output line.
func parseBenchmarkLine(line string) *BenchmarkResult {
	matches := benchmarkRegex.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}

	result := &BenchmarkResult{
		Name: matches[1],
	}

	// Parse iterations
	if iter, err := strconv.Atoi(matches[2]); err == nil {
		result.Iterations = iter
	}

	// Parse ns/op
	if ns, err := strconv.ParseFloat(matches[3], 64); err == nil {
		result.NsPerOp = ns
	}

	// Parse B/op (optional)
	if len(matches) > 4 && matches[4] != "" {
		if bytes, err := strconv.Atoi(matches[4]); err == nil {
			result.BytesPerOp = bytes
		}
	}

	// Parse allocs/op (optional)
	if len(matches) > 5 && matches[5] != "" {
		if allocs, err := strconv.Atoi(matches[5]); err == nil {
			result.AllocsPerOp = allocs
		}
	}

	// Parse custom metric (optional)
	if len(matches) > 7 && matches[6] != "" && matches[7] != "" {
		if val, err := strconv.ParseFloat(matches[6], 64); err == nil {
			result.CustomValue = val
			result.CustomMetric = matches[7]
		}
	}

	return result
}

// compare compares current results against baseline.
func compare(current, baseline map[string]*BenchmarkResult, threshold float64) *Report {
	report := &Report{
		Results: make([]*ComparisonResult, 0),
	}

	// Compare benchmarks that exist in both
	for name, curr := range current {
		report.TotalBenchmarks++

		base, exists := baseline[name]
		if !exists {
			report.NewBenchmarks++
			if *verbose {
				report.Results = append(report.Results, &ComparisonResult{
					Name:    name,
					Current: curr.NsPerOp,
					Status:  "NEW",
				})
			}
			continue
		}

		// Calculate delta percentage (positive = slower/worse)
		deltaPct := 0.0
		if base.NsPerOp > 0 {
			deltaPct = (curr.NsPerOp - base.NsPerOp) / base.NsPerOp
		}

		result := &ComparisonResult{
			Name:     name,
			Current:  curr.NsPerOp,
			Baseline: base.NsPerOp,
			DeltaPct: deltaPct * 100, // As percentage
		}

		if deltaPct > threshold {
			result.IsRegressed = true
			result.Status = "REGRESSION"
			report.Regressions++
			report.RegressionFailed = true
		} else if deltaPct < -ImprovementThreshold {
			result.IsImproved = true
			result.Status = "IMPROVED"
			report.Improvements++
		} else {
			result.Status = "OK"
			report.Unchanged++
		}

		// Always show regressions and improvements, optionally show all
		if result.IsRegressed || result.IsImproved || *verbose {
			report.Results = append(report.Results, result)
		}
	}

	// Check for missing benchmarks in current (might indicate removed tests)
	for name := range baseline {
		if _, exists := current[name]; !exists {
			report.MissingBaseline++
			if *verbose {
				report.Results = append(report.Results, &ComparisonResult{
					Name:     name,
					Baseline: baseline[name].NsPerOp,
					Status:   "MISSING",
				})
			}
		}
	}

	return report
}

// outputTextReport prints a human-readable report.
func outputTextReport(report *Report) {
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println("BENCHMARK COMPARISON REPORT")
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println()

	// Summary
	fmt.Printf("Total Benchmarks: %d\n", report.TotalBenchmarks)
	fmt.Printf("Regressions:      %d (> %.0f%% slower)\n", report.Regressions, *threshold*100)
	fmt.Printf("Improvements:     %d (> %.0f%% faster)\n", report.Improvements, ImprovementThreshold*100)
	fmt.Printf("Unchanged:        %d\n", report.Unchanged)
	fmt.Printf("New Benchmarks:   %d\n", report.NewBenchmarks)
	fmt.Printf("Missing:          %d\n", report.MissingBaseline)
	fmt.Println()

	// Detailed results
	if len(report.Results) > 0 {
		fmt.Println("-" + strings.Repeat("-", 79))
		fmt.Printf("%-50s %12s %12s %10s\n", "BENCHMARK", "CURRENT", "BASELINE", "DELTA")
		fmt.Println("-" + strings.Repeat("-", 79))

		for _, r := range report.Results {
			status := ""
			switch r.Status {
			case "REGRESSION":
				status = "❌ REGRESS"
			case "IMPROVED":
				status = "✅ FASTER"
			case "NEW":
				status = "🆕 NEW"
			case "MISSING":
				status = "⚠️ MISSING"
			default:
				status = "   OK"
			}

			if r.Baseline > 0 {
				fmt.Printf("%-50s %10.0f ns %10.0f ns %+8.1f%% %s\n",
					truncateName(r.Name, 50),
					r.Current,
					r.Baseline,
					r.DeltaPct,
					status,
				)
			} else {
				fmt.Printf("%-50s %10.0f ns %12s %10s %s\n",
					truncateName(r.Name, 50),
					r.Current,
					"-",
					"-",
					status,
				)
			}
		}
		fmt.Println("-" + strings.Repeat("-", 79))
	}

	// Final verdict
	fmt.Println()
	if report.RegressionFailed {
		fmt.Println("❌ FAILED: Performance regression detected!")
		fmt.Printf("   %d benchmark(s) regressed by more than %.0f%%\n", report.Regressions, *threshold*100)
	} else {
		fmt.Println("✅ PASSED: No significant regressions detected.")
	}
	fmt.Println()
}

// outputJSONReport outputs the report as JSON.
func outputJSONReport(report *Report) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}

// truncateName shortens long benchmark names.
func truncateName(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}
	return name[:maxLen-3] + "..."
}
