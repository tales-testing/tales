package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/parser"
	"github.com/tales-testing/tales/internal/provider"
	browserprovider "github.com/tales-testing/tales/internal/provider/browser"
	chromebrowser "github.com/tales-testing/tales/internal/provider/browser/chrome"
	execprovider "github.com/tales-testing/tales/internal/provider/exec"
	fileprovider "github.com/tales-testing/tales/internal/provider/file"
	httpprovider "github.com/tales-testing/tales/internal/provider/http"
	keywordprovider "github.com/tales-testing/tales/internal/provider/keyword"
	loadprovider "github.com/tales-testing/tales/internal/provider/load"
	mailprovider "github.com/tales-testing/tales/internal/provider/mail"
	mobileprovider "github.com/tales-testing/tales/internal/provider/mobile"
	androidmobile "github.com/tales-testing/tales/internal/provider/mobile/android"
	applemobile "github.com/tales-testing/tales/internal/provider/mobile/apple"
	rpcprovider "github.com/tales-testing/tales/internal/provider/rpc"
	sqlprovider "github.com/tales-testing/tales/internal/provider/sql"
	webhookprovider "github.com/tales-testing/tales/internal/provider/webhook"
	"github.com/tales-testing/tales/internal/report"
	"github.com/tales-testing/tales/internal/report/visual"
	talesruntime "github.com/tales-testing/tales/internal/runtime"
	"github.com/urfave/cli/v3"
)

const pathArgsUsage = "<path>"

// heartbeatTickEvery is the cadence at which the --verbose ticker lists
// scenarios still in flight. 30s strikes a balance: long enough that fast
// suites never trigger it, short enough that a hang surfaces within one or
// two ticks in CI.
const heartbeatTickEvery = 30 * time.Second

// defaultTeardownGrace is the wall-clock budget granted to teardown steps on
// a context detached from the run's. It exists so an exhausted --timeout stops
// the run without also killing the cleanup that the aborted run made
// necessary. 30s is generous enough for a handful of DELETE calls or a SQL
// purge, and short enough that a wedged provider cannot hang CI for long.
const defaultTeardownGrace = 30 * time.Second

// NewTestCommand returns test command.
func NewTestCommand() *cli.Command {
	return &cli.Command{
		Name:      "test",
		Usage:     "Execute .tales scenarios",
		ArgsUsage: pathArgsUsage,
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
			&cli.DurationFlag{Name: "timeout", Usage: "Global wall-clock budget for the whole run (e.g. 30s, 5m). 0 disables (default)."},
			&cli.DurationFlag{Name: "teardown-grace", Value: defaultTeardownGrace, Usage: "Wall-clock budget granted to teardown steps on a context detached from --timeout. 0 makes teardown inherit the (possibly cancelled) run context."},
			&cli.BoolFlag{Name: "verbose", Usage: "Emit a heartbeat every 30s listing scenarios still running"},
			&cli.BoolFlag{Name: "allow-exec", Usage: "Allow exec steps to run external programs (disabled by default)"},
		},
		Action: runTest,
	}
}

// printTimeoutDiagnostic surfaces the list of scenarios still in-flight when
// the global --timeout fired. The list is non-empty only when the runner's
// deadline-watcher snapshotted active work at the moment DeadlineExceeded
// was observed, so this print itself is the user-facing signal that the
// budget ran out. A nil/empty list means either the run finished cleanly
// or it was cancelled some other way (e.g. Ctrl-C).
func printTimeoutDiagnostic(out io.Writer, budget time.Duration, stalled []string) {
	if len(stalled) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "tales: global --timeout (%s) exceeded\n", budget)
	_, _ = fmt.Fprintln(out, "  scenarios still running when timeout hit:")

	for _, name := range stalled {
		_, _ = fmt.Fprintf(out, "    - %q\n", name)
	}
}

// printPreflight summarizes what was loaded and whether the global wall-clock
// budget is set, before any scenario runs. The "timeout=disabled" branch is
// the load-bearing one: a silent run with no --timeout is exactly the failure
// mode users hit when a provider stalls — surfacing it up front turns an
// invisible config gap into an obvious one.
func printPreflight(out io.Writer, suite *model.Suite, budget time.Duration) {
	scenarioWord := pluralize("scenario", len(suite.Scenarios))
	fileWord := pluralize("file", len(suite.Files))

	timeoutNote := "disabled (use --timeout=<dur> to bound the run)"
	if budget > 0 {
		timeoutNote = budget.String()
	}

	_, _ = fmt.Fprintf(out, "tales: loaded %d %s from %d %s; timeout=%s\n",
		len(suite.Scenarios), scenarioWord, len(suite.Files), fileWord, timeoutNote)
}

func pluralize(word string, count int) string {
	if count == 1 {
		return word
	}

	return word + "s"
}

// buildEventSink wires the streaming console reporter unless --no-progress
// is set. The sink writes to stderr so the final, machine-readable report on
// stdout is never interleaved. Color follows the same TTY heuristic used by
// PrintConsole; the stream itself runs even on CI because it is the only
// output produced while runner.Run is in flight.
func buildEventSink(noProgress, noColor bool) talesruntime.EventSink {
	if noProgress {
		return nil
	}

	streamOptions := report.DefaultConsoleOptions(os.Stderr)
	if noColor {
		streamOptions.Color = false
	}

	return report.NewStreamSink(os.Stderr, streamOptions.Color)
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

// mobileOptions assembles the mobile provider options: the shared capture
// mode plus one registration per compiled-in platform backend. A step's
// `platform` selects among them at runtime, so a binary built without a
// given backend reports that platform as unsupported rather than failing
// obscurely later.
func mobileOptions(captureMode mobileprovider.CaptureMode) []mobileprovider.Option {
	backends := make([]mobileprovider.Option, 0, 4)
	backends = append(backends, applemobile.Options()...)
	backends = append(backends, androidmobile.Options()...)

	opts := make([]mobileprovider.Option, 0, 1+len(backends))
	opts = append(opts, mobileprovider.WithCaptureMode(captureMode))
	opts = append(opts, backends...)

	return opts
}

func runTest(ctx context.Context, cmd *cli.Command) error {
	path := "."
	if cmd.NArg() > 0 {
		path = cmd.Args().First()
	}

	suite, diags := parser.LoadPath(path)
	if diags.HasErrors() {
		_, _ = fmt.Fprintf(os.Stderr, "%s\n", diags.Error())

		return cli.Exit("parse failed", 2)
	}

	printDeprecationWarnings(os.Stderr, diags)

	seed := cmd.Int64("seed")
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	htmlPath := cmd.String("report-html")

	captureMode, err := resolveCaptureMode(cmd.String("capture-screenshots"), htmlPath)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}

	budget := cmd.Duration("timeout")
	if budget > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}

	printPreflight(os.Stderr, suite, budget)

	runner := talesruntime.NewRunner(provider.NewRegistry(
		httpprovider.New(),
		keywordprovider.New(),
		mobileprovider.New(mobileOptions(captureMode)...),
		sqlprovider.New(),
		mailprovider.New(),
		chromebrowser.New(browserprovider.WithCaptureMode(captureMode)),
		loadprovider.New(),
		webhookprovider.New(),
		fileprovider.New(),
		execprovider.New(execprovider.WithAllowExec(cmd.Bool("allow-exec"))),
		rpcprovider.New(),
	))

	sink := buildEventSink(cmd.Bool("no-progress"), cmd.Bool("no-color"))

	heartbeatInterval := time.Duration(0)
	if cmd.Bool("verbose") {
		heartbeatInterval = heartbeatTickEvery
	}

	projectDir, _ := os.Getwd()

	result, err := runner.Run(ctx, suite, talesruntime.Options{
		Seed:              seed,
		Parallel:          cmd.Int("parallel"),
		Tags:              cmd.StringSlice("tag"),
		Scenario:          cmd.String("scenario"),
		ProjectDir:        projectDir,
		InputPath:         path,
		Events:            sink,
		HeartbeatInterval: heartbeatInterval,
		TeardownGrace:     cmd.Duration("teardown-grace"),
	})
	if err != nil && result == nil {
		_, _ = fmt.Fprintf(os.Stderr, "runtime failed: %v\n", err)

		return cli.Exit("runtime failed", 3)
	}

	if result != nil {
		printTimeoutDiagnostic(os.Stderr, budget, result.StalledScenarios)
	}

	if exitErr := emitReports(cmd, result, htmlPath); exitErr != nil {
		return exitErr
	}

	if result.Failed() {
		return cli.Exit("at least one scenario failed", 1)
	}

	return nil
}

// emitReports renders the console output and every requested report writer.
// Extracted from runTest to keep that function under the gocyclo budget; the
// flow is a straight sequence of "if requested, write — else skip", so the
// helper has no branches that matter beyond the writes themselves.
func emitReports(cmd *cli.Command, result *report.SuiteResult, htmlPath string) error {
	consoleOptions := report.DefaultConsoleOptions(os.Stdout)
	if cmd.Bool("no-color") {
		consoleOptions.Color = false
	}

	if cmd.Bool("no-progress") {
		consoleOptions.Progress = false
	}

	if printErr := report.PrintConsoleWithOptions(os.Stdout, result, consoleOptions); printErr != nil {
		return cli.Exit(printErr.Error(), 3)
	}

	if junitPath := cmd.String("report-junit"); junitPath != "" {
		if writeErr := report.WriteJUnit(junitPath, result); writeErr != nil {
			return cli.Exit(fmt.Sprintf("write junit failed: %v", writeErr), 3)
		}
	}

	if jsonlPath := cmd.String("report-jsonl"); jsonlPath != "" {
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

	return nil
}
