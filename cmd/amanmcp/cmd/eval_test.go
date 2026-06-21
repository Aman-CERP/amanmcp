package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Aman-CERP/amanmcp/internal/eval"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCmd_HasEvalSearchCommand(t *testing.T) {
	cmd := NewRootCmd()

	evalCmd, _, err := cmd.Find([]string{"eval"})
	require.NoError(t, err)
	require.NotNil(t, evalCmd)
	assert.Equal(t, "eval", evalCmd.Name())

	searchCmd, _, err := cmd.Find([]string{"eval", "search"})
	require.NoError(t, err)
	require.NotNil(t, searchCmd)
	assert.Equal(t, "search", searchCmd.Name())
}

func TestRootCmd_HasEvalGraphCommand(t *testing.T) {
	cmd := NewRootCmd()

	graphCmd, _, err := cmd.Find([]string{"eval", "graph"})
	require.NoError(t, err)
	require.NotNil(t, graphCmd)
	assert.Equal(t, "graph", graphCmd.Name())
}

func TestEvalSearchCmd_FlagParsingAndReportPaths(t *testing.T) {
	corpusPath := writeEvalCmdCorpus(t, `
queries:
  - id: Q1
    name: search owner
    query: "search engine"
    tool: search
    class: exact_identifier
    job: code
    expected_results:
      - path: internal/search/engine.go
        grade: 3
        rationale: owner
    holdout: false
    source: manual
`)
	outDir := t.TempDir()
	restore := stubEvalRunner(t, evalRunnerFunc(func(ctx context.Context, opts eval.Options) (*eval.Report, error) {
		assert.Equal(t, corpusPath, opts.CorpusPath)
		assert.Equal(t, "class:exact_identifier", opts.Subset)
		assert.Equal(t, "markdown", opts.Output)
		assert.Equal(t, outDir, opts.OutDir)
		assert.Equal(t, filepath.Join(outDir, "baseline.json"), opts.BaselinePath)
		assert.True(t, opts.FailOnRegression)
		assert.True(t, opts.IncludeHoldout)
		return &eval.Report{
			OutputPaths: eval.OutputPaths{
				Markdown: filepath.Join(outDir, "latest.md"),
			},
		}, nil
	}))
	defer restore()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"eval", "search",
		"--corpus", corpusPath,
		"--subset", "class:exact_identifier",
		"--output", "markdown",
		"--out-dir", outDir,
		"--baseline", filepath.Join(outDir, "baseline.json"),
		"--fail-on-regression",
		"--include-holdout",
	})

	err := cmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "latest.md")
}

func TestEvalGraphCmd_FlagParsingAndReportPaths(t *testing.T) {
	corpusPath := writeEvalCmdCorpus(t, `
schema_version: 1
queries:
  - id: GRA-Q1
    name: graph query
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick, full, mode:find_references]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: direct graph evidence
`)
	outDir := t.TempDir()
	restore := stubEvalGraphRunner(t, evalGraphRunnerFunc(func(ctx context.Context, opts eval.GraphOptions) (*eval.DirectGraphEvalReport, error) {
		assert.Equal(t, corpusPath, opts.CorpusPath)
		assert.Equal(t, "mode:find_references", opts.Subset)
		assert.Equal(t, "json", opts.Output)
		assert.Equal(t, outDir, opts.OutDir)
		assert.True(t, opts.FailOnRegression)
		assert.Equal(t, 0.10, opts.BlockingDegradationThreshold)
		assert.Contains(t, opts.Command, "amanmcp eval graph")
		assert.Contains(t, opts.Command, "--corpus="+corpusPath)
		assert.Contains(t, opts.Command, "--fail-on-regression")
		assert.Contains(t, opts.Command, "--out-dir="+outDir)
		assert.Contains(t, opts.Command, "--output=json")
		assert.Contains(t, opts.Command, "--subset=mode:find_references")
		return &eval.DirectGraphEvalReport{
			Summary: eval.DirectGraphSummary{
				QueryCount:              1,
				PassRate:                1.0,
				DegradationBlockingRate: 0,
			},
			OutputPaths: eval.OutputPaths{
				JSON: filepath.Join(outDir, "latest.json"),
			},
		}, nil
	}))
	defer restore()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"eval", "graph",
		"--corpus", corpusPath,
		"--subset", "mode:find_references",
		"--output", "json",
		"--out-dir", outDir,
		"--fail-on-regression",
	})

	err := cmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "latest.json")
	assert.Contains(t, buf.String(), "Graph eval summary")
}

func TestEvalGraphCmd_PrintsMeasurementSummary(t *testing.T) {
	cases := []struct {
		name         string
		report       *eval.DirectGraphEvalReport
		wantContains []string
		wantAbsent   []string
	}{
		{
			name: "measured run surfaces measured tool and scope",
			report: &eval.DirectGraphEvalReport{
				MeasuredTool:      eval.DirectGraphMeasuredTool,
				EvaluationScope:   eval.DirectGraphEvaluationScope,
				GraphToolMeasured: true,
				Summary:           eval.DirectGraphSummary{QueryCount: 3, MeasuredQueryCount: 3, PassRate: 1.0},
			},
			wantContains: []string{
				"Measured tool: graph.query (scope direct_graph_query_modes)",
				"graph tool measured: true (3/3 measured)",
			},
			wantAbsent: []string{"Unmeasured reason"},
		},
		{
			name: "unmeasured run surfaces the reason",
			report: &eval.DirectGraphEvalReport{
				MeasuredTool:      eval.DirectGraphMeasuredTool,
				EvaluationScope:   eval.DirectGraphEvaluationScope,
				GraphToolMeasured: false,
				UnmeasuredReason:  "graph.query produced no servable output (tool not measured)",
				Summary:           eval.DirectGraphSummary{QueryCount: 3, MeasuredQueryCount: 0},
			},
			wantContains: []string{
				"graph tool measured: false (0/3 measured)",
				"Unmeasured reason: graph.query produced no servable output (tool not measured)",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := tc.report
			restore := stubEvalGraphRunner(t, evalGraphRunnerFunc(func(context.Context, eval.GraphOptions) (*eval.DirectGraphEvalReport, error) {
				return report, nil
			}))
			defer restore()

			cmd := NewRootCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs([]string{"eval", "graph", "--output", "json", "--out-dir", t.TempDir()})

			require.NoError(t, cmd.Execute())
			for _, want := range tc.wantContains {
				assert.Contains(t, buf.String(), want)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, buf.String(), absent)
			}
		})
	}
}

func TestEvalGraphCmd_UsesConfigThresholdUnlessFlagOverrides(t *testing.T) {
	projectDir := t.TempDir()
	t.Chdir(projectDir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".amanmcp.yaml"), []byte(`
version: 1
eval:
  graph:
    blocking_degradation_threshold: 0.35
`), 0o644))
	corpusPath := writeEvalCmdCorpus(t, `
schema_version: 1
queries:
  - id: GRA-Q1
    name: graph query
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: direct graph evidence
`)
	outDir := t.TempDir()
	seen := 0
	restore := stubEvalGraphRunner(t, evalGraphRunnerFunc(func(ctx context.Context, opts eval.GraphOptions) (*eval.DirectGraphEvalReport, error) {
		seen++
		if seen == 1 {
			assert.Equal(t, 0.35, opts.BlockingDegradationThreshold)
		} else {
			assert.Equal(t, 0.20, opts.BlockingDegradationThreshold)
		}
		return &eval.DirectGraphEvalReport{
			Summary: eval.DirectGraphSummary{
				QueryCount:              1,
				PassRate:                1.0,
				DegradationBlockingRate: 0,
			},
			OutputPaths: eval.OutputPaths{
				JSON: filepath.Join(outDir, "latest.json"),
			},
		}, nil
	}))
	defer restore()

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{
		"eval", "graph",
		"--corpus", corpusPath,
		"--output", "json",
		"--out-dir", outDir,
		"--fail-on-regression",
	})
	require.NoError(t, cmd.Execute())

	cmd = NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{
		"eval", "graph",
		"--corpus", corpusPath,
		"--output", "json",
		"--out-dir", outDir,
		"--fail-on-regression",
		"--blocking-degradation-threshold", "0.20",
	})
	require.NoError(t, cmd.Execute())
	assert.Equal(t, 2, seen)
}

func TestEvalGraphCmd_PrintsReportWhenRunnerReturnsReportAndError(t *testing.T) {
	outDir := t.TempDir()
	restore := stubEvalGraphRunner(t, evalGraphRunnerFunc(func(context.Context, eval.GraphOptions) (*eval.DirectGraphEvalReport, error) {
		return &eval.DirectGraphEvalReport{
			Summary: eval.DirectGraphSummary{
				QueryCount:              2,
				PassRate:                0.50,
				DegradationBlockingRate: 0.50,
			},
			OutputPaths: eval.OutputPaths{
				JSON: filepath.Join(outDir, "latest.json"),
			},
		}, errors.New("direct graph eval gate failed")
	}))
	defer restore()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"eval", "graph", "--out-dir", outDir, "--fail-on-regression"})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, buf.String(), "blocking degradation")
	assert.Contains(t, buf.String(), "latest.json")
}

func TestEvalGraphCmd_MalformedCorpusFailure(t *testing.T) {
	corpusPath := writeEvalCmdCorpus(t, `
schema_version: 1
queries:
  - id: GRA-BAD
    name: bad graph query
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - rationale: rationale alone is not a matcher
`)
	outDir := t.TempDir()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"eval", "graph",
		"--corpus", corpusPath,
		"--out-dir", outDir,
	})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected evidence without a matcher")
}

func TestEvalGraphCmd_InvalidFlagsFailBeforeOpeningGraphDB(t *testing.T) {
	corpusPath := writeEvalCmdCorpus(t, `
schema_version: 1
queries:
  - id: GRA-Q1
    name: graph query
    mode: find_references
    query: internal/graph/query.go
    subsets: [quick]
    holdout: false
    source: manual
    expected:
      - source_path: internal/graph/query.go
        rationale: direct graph evidence
`)
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "unsupported output",
			args:    []string{"eval", "graph", "--output", "xml"},
			wantErr: `unsupported output "xml"`,
		},
		{
			name:    "unsupported subset",
			args:    []string{"eval", "graph", "--corpus", corpusPath, "--subset", "holdout"},
			wantErr: `unsupported graph subset "holdout"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRootCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestMakefile_HasDirectGraphEvalTargets(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "Makefile"))
	require.NoError(t, err)
	makefile := string(data)

	assert.Contains(t, makefile, "eval-graph-quick:")
	assert.Contains(t, makefile, "eval-graph-full:")
	assert.Contains(t, makefile, "amanmcp eval graph --subset quick")
	assert.Contains(t, makefile, "amanmcp eval graph --subset full")
	assert.Contains(t, makefile, "--fail-on-regression")
	assert.Contains(t, makefile, "--blocking-degradation-threshold")
}

func TestEvalSearchCmd_PrintsDimensionRegressionsWhenRunnerReturnsReportAndError(t *testing.T) {
	outDir := t.TempDir()
	restore := stubEvalRunner(t, evalRunnerFunc(func(context.Context, eval.Options) (*eval.Report, error) {
		return &eval.Report{
			Summary: eval.Summary{QueryCount: 2, PassRate: 0.50},
			BaselineComparison: eval.BaselineComparison{
				Regressed: true,
			},
			DimensionRegressions: []eval.DimensionRegression{{
				Dimension:     "profile",
				Group:         "code",
				Metric:        "pass_rate",
				BaselineValue: 1.00,
				CurrentValue:  0.00,
				Delta:         -1.00,
				Tolerance:     -0.0001,
				Regressed:     true,
			}},
			OutputPaths: eval.OutputPaths{
				JSON: filepath.Join(outDir, "latest.json"),
			},
		}, errors.New("eval regression detected")
	}))
	defer restore()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"eval", "search", "--out-dir", outDir, "--fail-on-regression"})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, buf.String(), "Dimension regressions")
	assert.Contains(t, buf.String(), "profile/code pass_rate")
	assert.Contains(t, buf.String(), "latest.json")
}

func TestEvalSearchCmd_MalformedCorpusFailure(t *testing.T) {
	corpusPath := writeEvalCmdCorpus(t, `
queries:
  - id: Q1
    name: bad query
    query: "search"
    tool: search
    class: exact_identifier
    job: code
    holdout: false
    source: manual
`)
	outDir := t.TempDir()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"eval", "search",
		"--corpus", corpusPath,
		"--out-dir", outDir,
	})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected evidence")
}

type evalRunnerFunc func(context.Context, eval.Options) (*eval.Report, error)

func (f evalRunnerFunc) Run(ctx context.Context, opts eval.Options) (*eval.Report, error) {
	return f(ctx, opts)
}

func stubEvalRunner(t *testing.T, runner evalSearchRunner) func() {
	t.Helper()
	old := newEvalSearchRunner
	newEvalSearchRunner = func(string) evalSearchRunner {
		return runner
	}
	return func() {
		newEvalSearchRunner = old
	}
}

type evalGraphRunnerFunc func(context.Context, eval.GraphOptions) (*eval.DirectGraphEvalReport, error)

func (f evalGraphRunnerFunc) Run(ctx context.Context, opts eval.GraphOptions) (*eval.DirectGraphEvalReport, error) {
	return f(ctx, opts)
}

func stubEvalGraphRunner(t *testing.T, runner evalGraphRunner) func() {
	t.Helper()
	old := newEvalGraphRunner
	newEvalGraphRunner = func(string) evalGraphRunner {
		return runner
	}
	return func() {
		newEvalGraphRunner = old
	}
}

func writeEvalCmdCorpus(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "queries.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}
