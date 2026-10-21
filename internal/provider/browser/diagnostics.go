package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// Artifact types of the step diagnostics (issue #83), written next to the
// step's dom.html.
const (
	artifactKindConsole = "console"
	artifactKindNetwork = "network"
	artifactKindState   = "state"

	consoleFileName = "console.jsonl"
	networkFileName = "network.jsonl"
	stateFileName   = "state.json"
)

// startDiagnostics starts recording the page's console and network for the
// step about to run, so what led to a failure can be written next to its
// screenshot. Nothing is recorded in CaptureNone, where nothing would be
// written either. A recording that cannot start costs the step nothing: the
// files are then left out.
func (p *Provider) startDiagnostics(ctx context.Context, drv driver.Driver) {
	if p.captureMode == provider.CaptureNone {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, p.captureAttemptTimeout)
	defer cancel()

	if err := drv.StartDiagnostics(ctx); err != nil {
		debugf("diagnostics", "recording not started: %v", err)
	}
}

// captureDiagnostics writes state.json, console.jsonl and network.jsonl in
// the step's step/ directory, next to its dom.html. It runs for a failed
// step in every capture mode but none, and for every step in
// CaptureActions, so passing runs in the other modes write nothing new.
//
// The page state is read first, under the capture bounds, because it needs
// the page to answer. The console and network recording is read last, from
// memory: it holds everything up to the moment the files are written, and
// is written even when the page has stopped answering.
func (p *Provider) captureDiagnostics(ctx context.Context, drv driver.Driver, stepDir string, forFailure bool) []Artifact {
	if p.captureMode == provider.CaptureNone || stepDir == "" {
		return nil
	}

	if !forFailure && p.captureMode != provider.CaptureActions {
		return nil
	}

	dir := stepLevelArtifactDir(stepDir)
	out := make([]Artifact, 0, 3)

	if state := p.readPageState(ctx, drv); state != nil {
		if a, err := writeState(dir, state, time.Now()); err == nil {
			out = append(out, a)
		} else {
			debugf("diagnostics", "state.json not written: %v", err)
		}
	}

	diag := drv.Diagnostics()
	if diag == nil {
		return out
	}

	if a, err := writeConsole(dir, diag); err == nil {
		out = append(out, a)
	} else {
		debugf("diagnostics", "console.jsonl not written: %v", err)
	}

	if a, err := writeNetwork(dir, diag); err == nil {
		out = append(out, a)
	} else {
		debugf("diagnostics", "network.jsonl not written: %v", err)
	}

	return out
}

// readPageState reads the page state under the capture budget, retried
// like a screenshot when a navigation interrupts it. Nil when the page did
// not answer or the driver cannot tell.
func (p *Provider) readPageState(ctx context.Context, drv driver.Driver) *driver.PageState {
	ctx, cancel := context.WithTimeout(ctx, p.captureBudget)
	defer cancel()

	state, err := retryCapture(ctx, p.captureAttemptTimeout, drv.PageState)
	if err != nil {
		debugf("diagnostics", "page state skipped: %v", err)

		return nil
	}

	return state
}

// stateFile is the content of state.json: the page state plus when it was
// read, in the layout every report timestamp uses.
type stateFile struct {
	CapturedAt string `json:"captured_at"`
	*driver.PageState
}

func writeState(dir string, state *driver.PageState, now time.Time) (Artifact, error) {
	body, err := json.MarshalIndent(stateFile{CapturedAt: diagnostic.FormatTimestamp(now), PageState: state}, "", "  ")
	if err != nil {
		return Artifact{}, fmt.Errorf("encode page state: %w", err)
	}

	return writeArtifactFile(dir, stateFileName, artifactKindState, append(body, '\n'))
}

// droppedLine is the first line of a JSONL file whose recording hit its
// bound: it says how many of the oldest entries are missing.
type droppedLine struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
	Limit int    `json:"limit"`
}

func writeConsole(dir string, diag *driver.Diagnostics) (Artifact, error) {
	return writeJSONL(dir, consoleFileName, artifactKindConsole, diag.Console, diag.ConsoleDropped, driver.MaxConsoleEntries)
}

func writeNetwork(dir string, diag *driver.Diagnostics) (Artifact, error) {
	return writeJSONL(dir, networkFileName, artifactKindNetwork, diag.Network, diag.NetworkDropped, driver.MaxNetworkEntries)
}

// writeJSONL writes one entry per line, oldest first, after a "dropped"
// line when the recording lost its oldest entries. A recording with no
// entry still writes its (empty) file: it says the recording ran and saw
// nothing, which a missing file could not tell apart from no recording.
func writeJSONL[T any](dir, name, kind string, entries []T, dropped, limit int) (Artifact, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("create artifact dir: %w", err)
	}

	path := filepath.Join(dir, name)

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return Artifact{}, fmt.Errorf("create %s: %w", name, err)
	}

	buf := bufio.NewWriter(file)
	encoder := json.NewEncoder(buf)
	// Console text and URLs stay readable: nothing here is rendered as
	// HTML.
	encoder.SetEscapeHTML(false)

	encodeErr := encodeJSONLines(encoder, entries, dropped, limit)
	flushErr := buf.Flush()
	closeErr := file.Close()

	for _, err := range []error{encodeErr, flushErr, closeErr} {
		if err != nil {
			return Artifact{}, fmt.Errorf("write %s: %w", name, err)
		}
	}

	return Artifact{Type: kind, Path: path}, nil
}

func encodeJSONLines[T any](encoder *json.Encoder, entries []T, dropped, limit int) error {
	if dropped > 0 {
		if err := encoder.Encode(droppedLine{Type: "dropped", Count: dropped, Limit: limit}); err != nil {
			return fmt.Errorf("encode dropped line: %w", err)
		}
	}

	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			return fmt.Errorf("encode entry: %w", err)
		}
	}

	return nil
}

// writeArtifactFile writes body to <dir>/<name> and returns it as an
// artifact of the given kind.
func writeArtifactFile(dir, name, kind string, body []byte) (Artifact, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("create artifact dir: %w", err)
	}

	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, body, 0o600); err != nil {
		return Artifact{}, fmt.Errorf("write %s: %w", name, err)
	}

	return Artifact{Type: kind, Path: path}, nil
}
