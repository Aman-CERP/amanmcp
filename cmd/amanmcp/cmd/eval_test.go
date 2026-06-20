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

func writeEvalCmdCorpus(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "queries.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}
