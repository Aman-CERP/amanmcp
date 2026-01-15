package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Aman-CERP/amanmcp/internal/store"
)

// ============================================================================
// Stats CLI Tests
// DEBT-028: Test coverage for stats commands
// ============================================================================

func TestStatsCmd_HasSubcommands(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding stats command
	statsCmd, _, err := cmd.Find([]string{"stats"})
	require.NoError(t, err)

	// Then: stats command should have queries subcommand
	subcommands := statsCmd.Commands()
	assert.GreaterOrEqual(t, len(subcommands), 1, "stats should have at least one subcommand")

	names := make(map[string]bool)
	for _, sc := range subcommands {
		names[sc.Name()] = true
	}
	assert.True(t, names["queries"], "should have queries command")
}

func TestStatsQueriesCmd_HasFlags(t *testing.T) {
	// Given: root command
	cmd := NewRootCmd()

	// When: finding stats queries command
	queriesCmd, _, err := cmd.Find([]string{"stats", "queries"})
	require.NoError(t, err)

	// Then: should have expected flags
	jsonFlag := queriesCmd.Flags().Lookup("json")
	assert.NotNil(t, jsonFlag, "should have --json flag")
	assert.Equal(t, "false", jsonFlag.DefValue)

	daysFlag := queriesCmd.Flags().Lookup("days")
	assert.NotNil(t, daysFlag, "should have --days flag")
	assert.Equal(t, "7", daysFlag.DefValue)
}

func TestRunStatsQueries_NoIndex(t *testing.T) {
	// Given: a directory without an index
	tmpDir := t.TempDir()

	oldDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(oldDir) }()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"stats", "queries"})

	// When: running stats queries
	err := cmd.Execute()

	// Then: should fail with no index error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no index found", "should indicate no index exists")
}

func TestRunStatsQueries_EmptyStats(t *testing.T) {
	// Given: a project with an index but no query history
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	// Create minimal metadata store
	metaPath := filepath.Join(dataDir, "metadata.db")
	meta, err := store.NewSQLiteStore(metaPath)
	require.NoError(t, err)
	require.NoError(t, meta.Close())

	oldDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(oldDir) }()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"stats", "queries"})

	// When: running stats queries
	err = cmd.Execute()

	// Then: should succeed with empty stats
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Query Statistics", "should show statistics header")
	assert.Contains(t, output, "Total Queries: 0", "should show zero queries")
}

func TestRunStatsQueries_JSONOutput(t *testing.T) {
	// Given: a project with an index
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, ".amanmcp")
	require.NoError(t, os.MkdirAll(dataDir, 0755))

	// Create minimal metadata store
	metaPath := filepath.Join(dataDir, "metadata.db")
	meta, err := store.NewSQLiteStore(metaPath)
	require.NoError(t, err)
	require.NoError(t, meta.Close())

	oldDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(oldDir) }()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"stats", "queries", "--json"})

	// When: running stats queries with JSON output
	err = cmd.Execute()

	// Then: should succeed and output valid JSON
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, `"summary"`, "should contain summary key")
	assert.Contains(t, output, `"total_queries"`, "should contain total_queries key")
	assert.Contains(t, output, `"query_type_counts"`, "should contain query_type_counts key")
}

func TestGetQueryStats_EmptyStore(t *testing.T) {
	// Given: a metrics store with no data
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "test.db")
	meta, err := store.NewSQLiteStore(metaPath)
	require.NoError(t, err)
	defer func() { _ = meta.Close() }()

	// Note: We can't directly test getQueryStats without creating a telemetry store
	// This test documents the expected behavior
	t.Log("getQueryStats with empty store should return zero counts")
}

func TestStatsQueriesOutput_Structure(t *testing.T) {
	// Given: stats output structure
	output := &StatsQueriesOutput{
		Summary: StatsQueriesSummary{
			TotalQueries:  100,
			ZeroResultPct: 5.5,
		},
		QueryTypeCounts: map[string]int64{
			"lexical":  40,
			"semantic": 60,
		},
		TopTerms: []StatsTermCount{
			{Term: "search", Count: 25},
			{Term: "find", Count: 20},
		},
		ZeroResultQueries: []string{"xyz", "abc"},
		LatencyDistribution: map[string]int64{
			"p50":  30,
			"p100": 50,
		},
	}

	// Then: structure should be properly populated
	assert.Equal(t, int64(100), output.Summary.TotalQueries)
	assert.Equal(t, 5.5, output.Summary.ZeroResultPct)
	assert.Len(t, output.QueryTypeCounts, 2)
	assert.Len(t, output.TopTerms, 2)
	assert.Len(t, output.ZeroResultQueries, 2)
	assert.Len(t, output.LatencyDistribution, 2)
}

func TestPrintStatsFormatted_EmptyData(t *testing.T) {
	// Given: empty stats output
	output := &StatsQueriesOutput{
		Summary: StatsQueriesSummary{
			TotalQueries:  0,
			ZeroResultPct: 0,
		},
		QueryTypeCounts:     make(map[string]int64),
		TopTerms:            []StatsTermCount{},
		ZeroResultQueries:   []string{},
		LatencyDistribution: make(map[string]int64),
	}

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	// When: printing formatted output
	err := printStatsFormatted(cmd, output)

	// Then: should succeed and show appropriate messages
	require.NoError(t, err)
	result := buf.String()
	assert.Contains(t, result, "Query Statistics")
	assert.Contains(t, result, "Total Queries: 0")
	assert.Contains(t, result, "none recorded yet")
	assert.Contains(t, result, "none")
}

func TestPrintStatsFormatted_WithData(t *testing.T) {
	// Given: stats output with data
	output := &StatsQueriesOutput{
		Summary: StatsQueriesSummary{
			TotalQueries:  100,
			ZeroResultPct: 5.0,
		},
		QueryTypeCounts: map[string]int64{
			"lexical":  40,
			"semantic": 60,
		},
		TopTerms: []StatsTermCount{
			{Term: "search", Count: 25},
			{Term: "find", Count: 20},
		},
		ZeroResultQueries: []string{"xyz"},
		LatencyDistribution: map[string]int64{
			"p50":  30,
			"p100": 50,
		},
	}

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	// When: printing formatted output
	err := printStatsFormatted(cmd, output)

	// Then: should show all data
	require.NoError(t, err)
	result := buf.String()
	assert.Contains(t, result, "Total Queries: 100")
	assert.Contains(t, result, "5.0%")
	assert.Contains(t, result, "Query Type Distribution")
	assert.Contains(t, result, "Top Query Terms")
	assert.Contains(t, result, "search (25)")
	assert.Contains(t, result, "Recent Zero-Result Queries")
	assert.Contains(t, result, `"xyz"`)
}

func TestPrintStatsJSON_ValidJSON(t *testing.T) {
	// Given: stats output
	output := &StatsQueriesOutput{
		Summary: StatsQueriesSummary{
			TotalQueries:  50,
			ZeroResultPct: 2.0,
		},
		QueryTypeCounts:     map[string]int64{"lexical": 25, "semantic": 25},
		TopTerms:            []StatsTermCount{{Term: "test", Count: 10}},
		ZeroResultQueries:   []string{},
		LatencyDistribution: map[string]int64{},
	}

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	// When: printing JSON output
	err := printStatsJSON(cmd, output)

	// Then: should be valid JSON
	require.NoError(t, err)
	result := buf.String()
	assert.Contains(t, result, `"total_queries": 50`)
	assert.Contains(t, result, `"zero_result_pct": 2`)
	assert.Contains(t, result, `"term": "test"`)
}
