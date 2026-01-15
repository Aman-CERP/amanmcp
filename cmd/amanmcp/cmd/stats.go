package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/store"
	"github.com/Aman-CERP/amanmcp/internal/telemetry"
)

func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show statistics and telemetry",
		Long:  `Display statistics about query patterns, performance, and usage.`,
	}

	cmd.AddCommand(newStatsQueriesCmd())
	return cmd
}

func newStatsQueriesCmd() *cobra.Command {
	var jsonOutput bool
	var days int

	cmd := &cobra.Command{
		Use:   "queries",
		Short: "Show query pattern statistics",
		Long: `Display query pattern telemetry including:
  - Query type distribution (lexical/semantic/mixed)
  - Top query terms
  - Zero-result queries
  - Latency distribution`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatsQueries(cmd.Context(), cmd, jsonOutput, days)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	cmd.Flags().IntVar(&days, "days", 7, "Number of days to include")

	return cmd
}

// StatsQueriesOutput is the JSON output format for query stats.
type StatsQueriesOutput struct {
	Summary             StatsQueriesSummary    `json:"summary"`
	QueryTypeCounts     map[string]int64       `json:"query_type_counts"`
	TopTerms            []StatsTermCount       `json:"top_terms"`
	ZeroResultQueries   []string               `json:"zero_result_queries"`
	LatencyDistribution map[string]int64       `json:"latency_distribution"`
}

// StatsQueriesSummary provides overview statistics.
type StatsQueriesSummary struct {
	TotalQueries  int64   `json:"total_queries"`
	ZeroResultPct float64 `json:"zero_result_pct"`
}

// StatsTermCount represents a term and its frequency.
type StatsTermCount struct {
	Term  string `json:"term"`
	Count int64  `json:"count"`
}

func runStatsQueries(ctx context.Context, cmd *cobra.Command, jsonOutput bool, days int) error {
	// Find project root
	root, err := config.FindProjectRoot(".")
	if err != nil {
		cwd, _ := os.Getwd()
		root = cwd
	}

	dataDir := filepath.Join(root, ".amanmcp")
	metadataPath := filepath.Join(dataDir, "metadata.db")

	// Check if index exists
	if !fileExists(metadataPath) {
		return fmt.Errorf("no index found in %s\nRun 'amanmcp index' to create one", root)
	}

	// Open metadata store (which now includes telemetry tables)
	metadata, err := store.NewSQLiteStore(metadataPath)
	if err != nil {
		return fmt.Errorf("failed to open metadata store: %w", err)
	}
	defer func() { _ = metadata.Close() }()

	// Create telemetry store using the same DB connection
	db := metadata.DB()
	metricsStore, err := telemetry.NewSQLiteMetricsStore(db)
	if err != nil {
		return fmt.Errorf("failed to open metrics store: %w", err)
	}

	// Get query metrics from store
	output, err := getQueryStats(metricsStore, days)
	if err != nil {
		return fmt.Errorf("failed to get query stats: %w", err)
	}

	if jsonOutput {
		return printStatsJSON(cmd, output)
	}

	return printStatsFormatted(cmd, output)
}

func getQueryStats(store *telemetry.SQLiteMetricsStore, days int) (*StatsQueriesOutput, error) {
	// For now, we just show an empty result since in-session metrics aren't persisted yet
	// The full implementation would query from SQLite based on date range

	// Get top terms
	topTerms, err := store.GetTopTerms(10)
	if err != nil {
		return nil, fmt.Errorf("get top terms: %w", err)
	}

	// Get zero-result queries
	zeroResults, err := store.GetZeroResultQueries(10)
	if err != nil {
		return nil, fmt.Errorf("get zero-result queries: %w", err)
	}

	// Build output
	output := &StatsQueriesOutput{
		Summary: StatsQueriesSummary{
			TotalQueries:  0,
			ZeroResultPct: 0,
		},
		QueryTypeCounts:     make(map[string]int64),
		TopTerms:            make([]StatsTermCount, 0, len(topTerms)),
		ZeroResultQueries:   zeroResults,
		LatencyDistribution: make(map[string]int64),
	}

	for _, tc := range topTerms {
		output.TopTerms = append(output.TopTerms, StatsTermCount{
			Term:  tc.Term,
			Count: tc.Count,
		})
	}

	return output, nil
}

func printStatsJSON(cmd *cobra.Command, output *StatsQueriesOutput) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func printStatsFormatted(cmd *cobra.Command, output *StatsQueriesOutput) error {
	w := cmd.OutOrStdout()

	fmt.Fprintln(w, "Query Statistics")
	fmt.Fprintln(w, "================")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "Total Queries: %d\n", output.Summary.TotalQueries)
	fmt.Fprintf(w, "Zero Results:  %.1f%%\n", output.Summary.ZeroResultPct)
	fmt.Fprintln(w)

	// Query Type Distribution
	if len(output.QueryTypeCounts) > 0 {
		fmt.Fprintln(w, "Query Type Distribution:")
		for qt, count := range output.QueryTypeCounts {
			fmt.Fprintf(w, "  %s: %d\n", qt, count)
		}
		fmt.Fprintln(w)
	}

	// Top Query Terms
	if len(output.TopTerms) > 0 {
		fmt.Fprintln(w, "Top Query Terms:")
		for i, tc := range output.TopTerms {
			fmt.Fprintf(w, "  %d. %s (%d)\n", i+1, tc.Term, tc.Count)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "Top Query Terms: (none recorded yet)")
		fmt.Fprintln(w)
	}

	// Zero-Result Queries
	if len(output.ZeroResultQueries) > 0 {
		fmt.Fprintln(w, "Recent Zero-Result Queries:")
		for _, q := range output.ZeroResultQueries {
			fmt.Fprintf(w, "  - \"%s\"\n", q)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "Recent Zero-Result Queries: (none)")
		fmt.Fprintln(w)
	}

	// Latency Distribution
	if len(output.LatencyDistribution) > 0 {
		fmt.Fprintln(w, "Latency Distribution:")
		buckets := []string{"p10", "p50", "p100", "p500", "p1000"}
		labels := map[string]string{
			"p10":   "<10ms",
			"p50":   "10-50ms",
			"p100":  "50-100ms",
			"p500":  "100-500ms",
			"p1000": ">500ms",
		}
		for _, b := range buckets {
			if count, ok := output.LatencyDistribution[b]; ok {
				fmt.Fprintf(w, "  %s: %d\n", labels[b], count)
			}
		}
	}

	return nil
}
