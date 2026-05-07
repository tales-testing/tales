package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/hyperxlab/tales/internal/parser"
	"github.com/hyperxlab/tales/internal/provider"
	httpprovider "github.com/hyperxlab/tales/internal/provider/http"
	keywordprovider "github.com/hyperxlab/tales/internal/provider/keyword"
	"github.com/hyperxlab/tales/internal/report"
	talesruntime "github.com/hyperxlab/tales/internal/runtime"
	"github.com/urfave/cli/v2"
)

// NewTestCommand returns test command.
func NewTestCommand() *cli.Command {
	return &cli.Command{
		Name:      "test",
		Usage:     "Execute .tales scenarios",
		ArgsUsage: "<path>",
		Flags: []cli.Flag{
			&cli.Int64Flag{Name: "seed", Value: 0, Usage: "Deterministic seed"},
			&cli.IntFlag{Name: "parallel", Value: runtime.NumCPU(), Usage: "Scenario parallelism"},
			&cli.StringSliceFlag{Name: "tag", Usage: "Filter scenario by tag"},
			&cli.StringFlag{Name: "scenario", Usage: "Run only one scenario by exact name"},
			&cli.StringFlag{Name: "report-junit", Usage: "Write JUnit XML report"},
			&cli.StringFlag{Name: "report-jsonl", Usage: "Write JSONL report"},
		},
		Action: runTest,
	}
}

func runTest(c *cli.Context) error {
	path := "."
	if c.NArg() > 0 {
		path = c.Args().First()
	}

	suite, diags := parser.LoadPath(path)
	if diags.HasErrors() {
		_, _ = fmt.Fprintf(os.Stderr, "%s\n", diags.Error())
		return cli.Exit("parse failed", 2)
	}

	seed := c.Int64("seed")
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	runner := talesruntime.NewRunner(provider.NewRegistry(
		httpprovider.New(),
		keywordprovider.New(),
	))

	result, err := runner.Run(context.Background(), suite, talesruntime.Options{
		Seed:     seed,
		Parallel: c.Int("parallel"),
		Tags:     c.StringSlice("tag"),
		Scenario: c.String("scenario"),
	})
	if err != nil && result == nil {
		_, _ = fmt.Fprintf(os.Stderr, "runtime failed: %v\n", err)
		return cli.Exit("runtime failed", 3)
	}

	if printErr := report.PrintConsole(os.Stdout, result); printErr != nil {
		return cli.Exit(printErr.Error(), 3)
	}

	if junitPath := c.String("report-junit"); junitPath != "" {
		if writeErr := report.WriteJUnit(junitPath, result); writeErr != nil {
			return cli.Exit(fmt.Sprintf("write junit failed: %v", writeErr), 3)
		}
	}
	if jsonlPath := c.String("report-jsonl"); jsonlPath != "" {
		if writeErr := report.WriteJSONL(jsonlPath, result); writeErr != nil {
			return cli.Exit(fmt.Sprintf("write jsonl failed: %v", writeErr), 3)
		}
	}

	if result.Failed() {
		return cli.Exit("at least one scenario failed", 1)
	}
	return nil
}
