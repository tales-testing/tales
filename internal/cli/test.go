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
	mobileprovider "github.com/hyperxlab/tales/internal/provider/mobile"
	"github.com/hyperxlab/tales/internal/report"
	"github.com/hyperxlab/tales/internal/report/visual"
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
			&cli.BoolFlag{Name: "no-color", Usage: "Disable colorized console output"},
			&cli.BoolFlag{Name: "no-progress", Usage: "Disable progress counters in console output"},
			&cli.StringFlag{Name: "report-junit", Usage: "Write JUnit XML report"},
			&cli.StringFlag{Name: "report-jsonl", Usage: "Write JSONL report"},
			&cli.StringFlag{Name: "report-html", Usage: "Write single-file visual HTML report"},
			&cli.StringFlag{Name: "capture-screenshots", Usage: "Mobile screenshot capture mode (none|failures|steps|actions)"},
		},
		Action: runTest,
	}
}

// resolveCaptureMode picks the effective mobile capture mode from the CLI
// flags. With no explicit flag it defaults to actions when --report-html is
// set (so the visual replay has frames to show) and failures otherwise (the
// historical behavior). An invalid explicit value produces a typed CLI error.
func resolveCaptureMode(raw, htmlPath string) (mobileprovider.CaptureMode, error) {
	if raw == "" {
		if htmlPath != "" {
			return mobileprovider.CaptureActions, nil
		}

		return mobileprovider.CaptureFailures, nil
	}

	mode, err := mobileprovider.ParseCaptureMode(raw)
	if err != nil {
		return "", fmt.Errorf("--capture-screenshots: %w", err)
	}

	return mode, nil
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

	htmlPath := c.String("report-html")

	captureMode, err := resolveCaptureMode(c.String("capture-screenshots"), htmlPath)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}

	runner := talesruntime.NewRunner(provider.NewRegistry(
		httpprovider.New(),
		keywordprovider.New(),
		mobileprovider.NewApple(mobileprovider.WithCaptureMode(captureMode)),
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

	consoleOptions := report.DefaultConsoleOptions(os.Stdout)
	if c.Bool("no-color") {
		consoleOptions.Color = false
	}

	if c.Bool("no-progress") {
		consoleOptions.Progress = false
	}

	if printErr := report.PrintConsoleWithOptions(os.Stdout, result, consoleOptions); printErr != nil {
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

	if htmlPath != "" {
		if writeErr := visual.Write(htmlPath, result); writeErr != nil {
			return cli.Exit(fmt.Sprintf("write html failed: %v", writeErr), 3)
		}

		_, _ = fmt.Fprintf(os.Stdout, "HTML report: %s\n", htmlPath)
	}

	if result.Failed() {
		return cli.Exit("at least one scenario failed", 1)
	}

	return nil
}
