package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
	"github.com/zclconf/go-cty/cty"
)

// diagnosticsFake is a FakeDriver that recorded one console message, one
// exception and one request, and knows its page state.
func diagnosticsFake() *driver.FakeDriver {
	fake := driver.NewFakeDriver()
	fake.ScreenshotPNG = capturePNG
	fake.OuterHTMLValue = "<html><body>page</body></html>"
	fake.ConsoleEntries = []driver.ConsoleEntry{
		{Time: "2026-10-21T09:00:00.001Z", Type: driver.ConsoleTypeConsole, Level: "log", Text: "diagnostics page ready"},
		{Time: "2026-10-21T09:00:00.002Z", Type: driver.ConsoleTypeException, Level: "error", Text: "Uncaught Error: diagnostics boom", Line: 3},
	}
	fake.NetworkEntries = []driver.NetworkEntry{
		{Time: "2026-10-21T09:00:00.003Z", Type: driver.NetworkTypeRequest, RequestID: "1", Method: "GET", URL: "http://127.0.0.1/web/diagnostics/ping", ResourceType: "Fetch"},
	}
	fake.PageStateValue = &driver.PageState{
		URL:           "http://127.0.0.1/web/diagnostics",
		ReadyState:    "complete",
		ActiveElement: &driver.ElementDescription{Selector: `input[data-testid="diagnostics.query"]`, Tag: "input", TestID: "diagnostics.query"},
		Viewport:      driver.Viewport{Width: 1280, Height: 720, DevicePixelRatio: 1},
		Scroll:        driver.ScrollOffsets{Y: 40},
	}

	return fake
}

func runDiagnosticsStep(t *testing.T, mode provider.CaptureMode, drv driver.Driver, exec *provider.BrowserExecution) (*Provider, *provider.Output, error) {
	t.Helper()

	p := buildCaptureProvider(t, mode, drv)

	exec.TargetName = "chrome"

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "diagnose", File: "demo.tales"},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser:  exec,
	})

	return p, out, err
}

func failingWait() *provider.BrowserExecution {
	return &provider.BrowserExecution{Actions: []provider.BrowserActionExec{
		{Kind: model.BrowserActionGoto, URL: "http://127.0.0.1/web/diagnostics"},
		{Kind: model.BrowserActionWaitVisible, Selector: "#never", Timeout: time.Second},
	}}
}

func passingGoto() *provider.BrowserExecution {
	return &provider.BrowserExecution{Actions: []provider.BrowserActionExec{
		{Kind: model.BrowserActionGoto, URL: "http://127.0.0.1/web/diagnostics"},
	}}
}

// artifactsByType reads the artifact list of an output into type -> path.
func artifactsByType(t *testing.T, out *provider.Output) map[string]string {
	t.Helper()

	got := map[string]string{}

	list, ok := out.Response["artifacts"]
	if !ok || list.IsNull() {
		return got
	}

	for it := list.ElementIterator(); it.Next(); {
		_, item := it.Element()
		got[item.GetAttr(artifactType).AsString()] = item.GetAttr(artifactPath).AsString()
	}

	return got
}

func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}

	defer func() { _ = file.Close() }()

	var lines []map[string]any

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("%s: line %q is not JSON: %v", path, scanner.Text(), err)
		}

		lines = append(lines, line)
	}

	return lines
}

func TestFailedStepWritesDiagnosticsNextToTheDOM(t *testing.T) {
	t.Parallel()

	fake := diagnosticsFake()
	fake.FailOnSelector = map[string]error{"#never": errors.New("selector #never not visible")}

	_, out, err := runDiagnosticsStep(t, provider.CaptureFailures, fake, failingWait())
	if err == nil {
		t.Fatal("wait_visible was programmed to fail")
	}

	artifacts := artifactsByType(t, out)

	for _, kind := range []string{artifactKindScreenshot, artifactKindDOM, artifactKindConsole, artifactKindNetwork, artifactKindState} {
		if artifacts[kind] == "" {
			t.Fatalf("artifact %q missing from %v", kind, artifacts)
		}
	}

	stepDir := filepath.Dir(artifacts[artifactKindDOM])

	for kind, name := range map[string]string{artifactKindConsole: consoleFileName, artifactKindNetwork: networkFileName, artifactKindState: stateFileName} {
		if want := filepath.Join(stepDir, name); artifacts[kind] != want {
			t.Errorf("%s written at %s, want next to dom.html at %s", kind, artifacts[kind], want)
		}
	}

	console := readJSONLines(t, artifacts[artifactKindConsole])
	if len(console) != 2 || console[0]["text"] != "diagnostics page ready" || console[1]["type"] != "exception" || console[1]["text"] != "Uncaught Error: diagnostics boom" {
		t.Errorf("console.jsonl = %v", console)
	}

	network := readJSONLines(t, artifacts[artifactKindNetwork])
	if len(network) != 1 || network[0]["type"] != "request" || network[0]["url"] != "http://127.0.0.1/web/diagnostics/ping" || network[0]["method"] != "GET" {
		t.Errorf("network.jsonl = %v", network)
	}

	body, err := os.ReadFile(artifacts[artifactKindState])
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}

	var state struct {
		CapturedAt    string `json:"captured_at"`
		URL           string `json:"url"`
		ActiveElement struct {
			Selector string `json:"selector"`
			TestID   string `json:"data_testid"`
		} `json:"active_element"`
		Viewport struct {
			Width float64 `json:"width"`
		} `json:"viewport"`
		Scroll struct {
			Y float64 `json:"y"`
		} `json:"scroll"`
	}

	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("state.json is not JSON: %v\n%s", err, body)
	}

	if state.URL != "http://127.0.0.1/web/diagnostics" || state.ActiveElement.TestID != "diagnostics.query" || state.Viewport.Width != 1280 || state.Scroll.Y != 40 {
		t.Errorf("state.json = %s", body)
	}

	if _, err := time.Parse("2006-01-02T15:04:05.000Z07:00", state.CapturedAt); err != nil {
		t.Errorf("captured_at %q: %v", state.CapturedAt, err)
	}
}

func TestFailedExpectationWritesDiagnostics(t *testing.T) {
	t.Parallel()

	fake := diagnosticsFake()
	fake.Texts["#title"] = "Login"

	exec := passingGoto()
	exec.Expect = provider.BrowserExpectExec{Text: []provider.BrowserValueExpectationExec{
		{Selector: "#title", Expected: cty.StringVal("Dashboard"), Timeout: 50 * time.Millisecond},
	}}

	_, out, err := runDiagnosticsStep(t, provider.CaptureFailures, fake, exec)
	if err == nil {
		t.Fatal("the text expectation was programmed to fail")
	}

	artifacts := artifactsByType(t, out)
	if artifacts[artifactKindConsole] == "" || artifacts[artifactKindNetwork] == "" || artifacts[artifactKindState] == "" {
		t.Fatalf("a failed expectation must write the diagnostics, got %v", artifacts)
	}
}

func TestRecordingStartsBeforeTheFirstAction(t *testing.T) {
	t.Parallel()

	fake := diagnosticsFake()

	if _, _, err := runDiagnosticsStep(t, provider.CaptureFailures, fake, passingGoto()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	methods := fake.MethodsCalled()

	start, goTo := slices.Index(methods, "StartDiagnostics"), slices.Index(methods, "Goto")
	if start < 0 || goTo < 0 || start > goTo {
		t.Fatalf("StartDiagnostics must run before the first action, calls: %v", methods)
	}
}

func TestPassingStepWritesNoDiagnosticsOutsideActionsMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []provider.CaptureMode{provider.CaptureFailures, provider.CaptureSteps} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			fake := diagnosticsFake()

			p, out, err := runDiagnosticsStep(t, mode, fake, passingGoto())
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}

			artifacts := artifactsByType(t, out)
			for _, kind := range []string{artifactKindConsole, artifactKindNetwork, artifactKindState} {
				if artifacts[kind] != "" {
					t.Errorf("a passing step in %s mode wrote %s: %s", mode, kind, artifacts[kind])
				}
			}

			if slices.Contains(fake.MethodsCalled(), "PageState") {
				t.Errorf("a passing step in %s mode read the page state", mode)
			}

			assertNoDiagnosticFiles(t, p.artifactsBase)
		})
	}
}

func TestPassingStepWritesDiagnosticsInActionsMode(t *testing.T) {
	t.Parallel()

	fake := diagnosticsFake()

	_, out, err := runDiagnosticsStep(t, provider.CaptureActions, fake, passingGoto())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	artifacts := artifactsByType(t, out)
	for _, kind := range []string{artifactKindConsole, artifactKindNetwork, artifactKindState} {
		if artifacts[kind] == "" {
			t.Errorf("actions mode must write %s for a passing step, got %v", kind, artifacts)
		}
	}

	if dir := filepath.Base(filepath.Dir(artifacts[artifactKindConsole])); dir != "step" {
		t.Errorf("console.jsonl written under %q, want the step/ directory", dir)
	}
}

func TestCaptureNoneRecordsAndWritesNothing(t *testing.T) {
	t.Parallel()

	fake := diagnosticsFake()
	fake.FailOnSelector = map[string]error{"#never": errors.New("selector #never not visible")}

	p, out, err := runDiagnosticsStep(t, provider.CaptureNone, fake, failingWait())
	if err == nil {
		t.Fatal("wait_visible was programmed to fail")
	}

	if methods := fake.MethodsCalled(); slices.Contains(methods, "StartDiagnostics") || slices.Contains(methods, "PageState") {
		t.Errorf("capture mode none must not record, calls: %v", methods)
	}

	if artifacts := artifactsByType(t, out); len(artifacts) != 0 {
		t.Errorf("capture mode none wrote %v", artifacts)
	}

	assertNoDiagnosticFiles(t, p.artifactsBase)
}

func TestRecordingThatCannotStartLeavesTheStepAlone(t *testing.T) {
	t.Parallel()

	fake := diagnosticsFake()
	fake.StartDiagnosticsErr = errors.New("listener refused")
	fake.FailOnSelector = map[string]error{"#never": errors.New("selector #never not visible")}

	_, out, err := runDiagnosticsStep(t, provider.CaptureFailures, fake, failingWait())
	if err == nil || !strings.Contains(err.Error(), "#never") {
		t.Fatalf("the step must fail on its own action, got %v", err)
	}

	artifacts := artifactsByType(t, out)
	if artifacts[artifactKindConsole] != "" || artifacts[artifactKindNetwork] != "" {
		t.Errorf("no recording ran, yet console/network were written: %v", artifacts)
	}

	if artifacts[artifactKindState] == "" || artifacts[artifactKindDOM] == "" {
		t.Errorf("the page state and DOM do not depend on the recording, got %v", artifacts)
	}
}

// hangingStateDriver never answers PageState, the way Chrome does not
// answer a call made while a navigation replaces the page.
type hangingStateDriver struct {
	*driver.FakeDriver
}

func (d hangingStateDriver) PageState(ctx context.Context) (*driver.PageState, error) {
	<-ctx.Done()

	return nil, ctx.Err()
}

func TestPageStateReadIsBounded(t *testing.T) {
	t.Parallel()

	fake := diagnosticsFake()
	fake.FailOnSelector = map[string]error{"#never": errors.New("selector #never not visible")}

	started := time.Now()

	_, out, err := runDiagnosticsStep(t, provider.CaptureFailures, hangingStateDriver{FakeDriver: fake}, failingWait())
	if err == nil {
		t.Fatal("wait_visible was programmed to fail")
	}

	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Execute took %s: the page state read was not bounded", elapsed)
	}

	artifacts := artifactsByType(t, out)
	if artifacts[artifactKindState] != "" {
		t.Errorf("state.json written for a page that never answered: %v", artifacts)
	}

	if artifacts[artifactKindConsole] == "" || artifacts[artifactKindNetwork] == "" {
		t.Errorf("console and network come from memory and must be written anyway, got %v", artifacts)
	}
}

func TestWriteJSONLReportsDroppedEntriesFirst(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	a, err := writeConsole(dir, &driver.Diagnostics{
		Console:        []driver.ConsoleEntry{{Time: "t", Type: driver.ConsoleTypeConsole, Level: "log", Text: "<b>kept & readable</b>"}},
		ConsoleDropped: 12,
	})
	if err != nil {
		t.Fatalf("writeConsole: %v", err)
	}

	lines := readJSONLines(t, a.Path)
	if len(lines) != 2 {
		t.Fatalf("lines = %v, want the dropped line then the entry", lines)
	}

	if lines[0]["type"] != "dropped" || lines[0]["count"] != float64(12) || lines[0]["limit"] != float64(driver.MaxConsoleEntries) {
		t.Errorf("dropped line = %v", lines[0])
	}

	raw, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !strings.Contains(string(raw), "<b>kept & readable</b>") {
		t.Errorf("console text was HTML-escaped: %s", raw)
	}
}

func TestWriteJSONLWritesAnEmptyFileForAnEmptyRecording(t *testing.T) {
	t.Parallel()

	a, err := writeNetwork(t.TempDir(), &driver.Diagnostics{})
	if err != nil {
		t.Fatalf("writeNetwork: %v", err)
	}

	info, err := os.Stat(a.Path)
	if err != nil {
		t.Fatalf("an empty recording must still write its file: %v", err)
	}

	if info.Size() != 0 {
		t.Errorf("empty recording wrote %d bytes", info.Size())
	}
}

func assertNoDiagnosticFiles(t *testing.T, base string) {
	t.Helper()

	err := filepath.WalkDir(base, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		switch filepath.Base(path) {
		case consoleFileName, networkFileName, stateFileName:
			t.Errorf("unexpected diagnostics file %s", path)
		}

		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("walk %s: %v", base, err)
	}
}
