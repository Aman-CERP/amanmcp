package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/eval"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type evalSearchRunner interface {
	Run(context.Context, eval.Options) (*eval.Report, error)
}

type evalGraphRunner interface {
	Run(context.Context, eval.GraphOptions) (*eval.DirectGraphEvalReport, error)
}

var newEvalSearchRunner = func(projectRoot string) evalSearchRunner {
	return eval.NewRunner(&lazyValidationSearcher{projectRoot: projectRoot})
}

var newEvalGraphRunner = func(projectRoot string) evalGraphRunner {
	dataDir := filepath.Join(projectRoot, ".amanmcp")
	return eval.NewDirectGraphRunner(eval.NewSQLiteDirectGraphClient(dataDir, hashString(projectRoot)))
}

type lazyValidationSearcher struct {
	projectRoot string
	once        sync.Once
	searcher    *eval.ValidationSearcher
	err         error
}

func (s *lazyValidationSearcher) Prepare(ctx context.Context) error {
	s.once.Do(func() {
		s.searcher, s.err = eval.NewValidationSearcher(ctx, s.projectRoot)
	})
	return s.err
}

func (s *lazyValidationSearcher) Search(ctx context.Context, query eval.Query) (eval.SearchResponse, error) {
	if err := s.Prepare(ctx); err != nil {
		return eval.SearchResponse{}, err
	}
	return s.searcher.Search(ctx, query)
}

func (s *lazyValidationSearcher) Close() error {
	if s.searcher == nil {
		return nil
	}
	return s.searcher.Close()
}

func newEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run evaluation harnesses",
	}
	cmd.AddCommand(newEvalSearchCmd())
	cmd.AddCommand(newEvalGraphCmd())
	return cmd
}

func newEvalSearchCmd() *cobra.Command {
	opts := eval.Options{
		CorpusPath: eval.DefaultCorpusPath,
		Subset:     "full",
		Output:     "both",
		OutDir:     eval.DefaultOutDir,
	}

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Run the search evaluation corpus",
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.Command = "amanmcp " + strings.Join(os.Args[1:], " ")
			return runEvalSearch(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.CorpusPath, "corpus", opts.CorpusPath, "Path to search eval corpus")
	cmd.Flags().StringVar(&opts.Subset, "subset", opts.Subset, "Subset: quick, graph, full, holdout, class:<name>, or job:<name>")
	cmd.Flags().StringVar(&opts.Output, "output", opts.Output, "Report output: json, markdown, or both")
	cmd.Flags().StringVar(&opts.OutDir, "out-dir", opts.OutDir, "Directory for latest.json/latest.md reports")
	cmd.Flags().StringVar(&opts.BaselinePath, "baseline", "", "Optional baseline JSON report path")
	cmd.Flags().StringVar(&opts.TokenBaselinePath, "token-baseline", "", "Optional tokens-baseline JSON path")
	cmd.Flags().BoolVar(&opts.FailOnRegression, "fail-on-regression", false, "Exit non-zero when baseline comparison regresses")
	cmd.Flags().BoolVar(&opts.IncludeHoldout, "include-holdout", false, "Include holdout queries in full/class/job subsets")
	cmd.Flags().BoolVar(&opts.SaveBaseline, "save-baseline", false, "Also write baseline.json/baseline.md and tokens-baseline artifacts")
	cmd.Flags().BoolVar(&opts.ForceOverwriteBaseline, "force-overwrite-baseline", false, "Allow --save-baseline to overwrite existing baseline artifacts")

	return cmd
}

func newEvalGraphCmd() *cobra.Command {
	opts := eval.GraphOptions{
		CorpusPath:                   eval.DefaultGraphCorpusPath,
		Subset:                       eval.GraphSubsetFull,
		Output:                       "both",
		OutDir:                       eval.DefaultGraphOutDir,
		BlockingDegradationThreshold: config.DefaultEvalGraphBlockingDegradationThreshold,
	}

	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Run the direct graph.query evaluation corpus",
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.Command = evalCommandInvocation(cmd)
			opts.BlockingDegradationThresholdConfigured = cmd.Flags().Changed("blocking-degradation-threshold")
			return runEvalGraph(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.CorpusPath, "corpus", opts.CorpusPath, "Path to graph eval corpus")
	cmd.Flags().StringVar(&opts.Subset, "subset", opts.Subset, "Subset: quick, full, or mode:<name>")
	cmd.Flags().StringVar(&opts.Output, "output", opts.Output, "Report output: json, markdown, or both")
	cmd.Flags().StringVar(&opts.OutDir, "out-dir", opts.OutDir, "Directory for latest.json/latest.md reports")
	cmd.Flags().BoolVar(&opts.FailOnRegression, "fail-on-regression", false, "Exit non-zero when direct graph eval degradation exceeds the regression gate")
	cmd.Flags().Float64Var(
		&opts.BlockingDegradationThreshold,
		"blocking-degradation-threshold",
		opts.BlockingDegradationThreshold,
		"Blocking graph status rate threshold for --fail-on-regression",
	)

	return cmd
}

func evalCommandInvocation(cmd *cobra.Command) string {
	parts := []string{cmd.CommandPath()}
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		if flag.Value.Type() == "bool" {
			if flag.Value.String() == "true" {
				parts = append(parts, "--"+flag.Name)
				return
			}
			parts = append(parts, "--"+flag.Name+"=false")
			return
		}
		parts = append(parts, "--"+flag.Name+"="+flag.Value.String())
	})
	return strings.Join(parts, " ")
}

func runEvalSearch(cmd *cobra.Command, opts eval.Options) error {
	root, err := findEvalProjectRoot()
	if err != nil {
		return err
	}

	report, runErr := newEvalSearchRunner(root).Run(cmd.Context(), opts)
	if report != nil {
		writeEvalSearchSummary(cmd, report)
	}
	if runErr != nil {
		return runErr
	}
	return nil
}

func runEvalGraph(cmd *cobra.Command, opts eval.GraphOptions) error {
	root, err := findEvalProjectRoot()
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return fmt.Errorf("failed to load eval graph configuration: %w", err)
	}
	if !opts.BlockingDegradationThresholdConfigured {
		opts.BlockingDegradationThreshold = cfg.Eval.Graph.BlockingDegradationThreshold
		opts.BlockingDegradationThresholdConfigured = true
	}
	opts.ModeThresholds = cfg.Eval.Graph.Modes

	report, runErr := newEvalGraphRunner(root).Run(cmd.Context(), opts)
	if report != nil {
		writeEvalGraphSummary(cmd, report)
	}
	if runErr != nil {
		return runErr
	}
	return nil
}

func findEvalProjectRoot() (string, error) {
	root, err := config.FindProjectRoot(".")
	if err == nil {
		return root, nil
	}
	root, err = os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to determine project root: %w", err)
	}
	return root, nil
}

func writeEvalSearchSummary(cmd *cobra.Command, report *eval.Report) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Eval summary: %d queries, pass rate %.2f, regressed: %t\n",
		report.Summary.QueryCount,
		report.Summary.PassRate,
		report.BaselineComparison.Regressed,
	)
	if regressions := regressedDimensions(report.DimensionRegressions); len(regressions) > 0 {
		fmt.Fprintln(out, "Dimension regressions:")
		for _, regression := range regressions {
			fmt.Fprintf(out, "- %s/%s %s delta %.2f (baseline %.2f, current %.2f)\n",
				regression.Dimension,
				regression.Group,
				regression.Metric,
				regression.Delta,
				regression.BaselineValue,
				regression.CurrentValue,
			)
		}
	}
	if report.OutputPaths.JSON != "" {
		fmt.Fprintf(out, "JSON report: %s\n", report.OutputPaths.JSON)
	}
	if report.OutputPaths.Markdown != "" {
		fmt.Fprintf(out, "Markdown report: %s\n", report.OutputPaths.Markdown)
	}
}

func writeEvalGraphSummary(cmd *cobra.Command, report *eval.DirectGraphEvalReport) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Graph eval summary: %d queries, pass rate %.2f, blocking degradation %.2f\n",
		report.Summary.QueryCount,
		report.Summary.PassRate,
		report.Summary.DegradationBlockingRate,
	)
	fmt.Fprintf(out, "Measured tool: %s (scope %s), graph tool measured: %t (%d/%d measured)\n",
		report.MeasuredTool,
		report.EvaluationScope,
		report.GraphToolMeasured,
		report.Summary.MeasuredQueryCount,
		report.Summary.QueryCount,
	)
	if report.UnmeasuredReason != "" {
		fmt.Fprintf(out, "Unmeasured reason: %s\n", report.UnmeasuredReason)
	}
	if report.OutputPaths.JSON != "" {
		fmt.Fprintf(out, "JSON report: %s\n", report.OutputPaths.JSON)
	}
	if report.OutputPaths.Markdown != "" {
		fmt.Fprintf(out, "Markdown report: %s\n", report.OutputPaths.Markdown)
	}
}

func regressedDimensions(regressions []eval.DimensionRegression) []eval.DimensionRegression {
	out := make([]eval.DimensionRegression, 0, len(regressions))
	for _, regression := range regressions {
		if regression.Regressed {
			out = append(out, regression)
		}
	}
	return out
}
