package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Aman-CERP/amanmcp/internal/config"
	"github.com/Aman-CERP/amanmcp/internal/eval"
	"github.com/spf13/cobra"
)

type evalSearchRunner interface {
	Run(context.Context, eval.Options) (*eval.Report, error)
}

var newEvalSearchRunner = func(projectRoot string) evalSearchRunner {
	return eval.NewRunner(&lazyValidationSearcher{projectRoot: projectRoot})
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

func runEvalSearch(cmd *cobra.Command, opts eval.Options) error {
	root, err := config.FindProjectRoot(".")
	if err != nil {
		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to determine project root: %w", err)
		}
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

func regressedDimensions(regressions []eval.DimensionRegression) []eval.DimensionRegression {
	out := make([]eval.DimensionRegression, 0, len(regressions))
	for _, regression := range regressions {
		if regression.Regressed {
			out = append(out, regression)
		}
	}
	return out
}
