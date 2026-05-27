package report

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// StreamSink writes runtime progress events to a Writer as they happen.
// It satisfies the runtime.EventSink shape via duck typing — the runtime
// package imports report, not the other way around, so we describe the
// surface in the runtime package and rely on the concrete *StreamSink
// satisfying it.
//
// Output is intentionally minimal and line-oriented so it stays readable
// when several scenarios stream in parallel: one start line, one end line,
// no buffering. Color is applied only when the underlying writer is a TTY
// (i.e. ConsoleOptions.Color is true).
type StreamSink struct {
	mu      sync.Mutex
	out     io.Writer
	painter colorPainter
	started time.Time
}

// NewStreamSink builds a sink that writes to out. Pass color=true to enable
// ANSI escapes (caller is responsible for the TTY check — DefaultConsoleOptions
// does it).
func NewStreamSink(out io.Writer, color bool) *StreamSink {
	return &StreamSink{out: out, painter: newColorPainter(color)}
}

// SuiteStarted prints the suite header once.
func (s *StreamSink) SuiteStarted(totalScenarios, parallel int, seed int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.started = time.Now()

	label := s.painter.paint(ansiBlue, "Suite")

	_, _ = fmt.Fprintf(s.out, "%s: running %d scenario(s), parallel=%d, seed=%d\n",
		label, totalScenarios, parallel, seed)
}

// ScenarioStarted prints a "starting" line when a scenario goroutine begins
// real work. With parallel > 1 several start lines may appear before the
// first end line — that is intentional and reflects the actual execution.
func (s *StreamSink) ScenarioStarted(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	arrow := s.painter.paint(ansiBlue, "▶")
	timestamp := s.painter.paint(ansiGray, time.Now().Format("15:04:05"))

	_, _ = fmt.Fprintf(s.out, "%s %s scenario %q starting\n", timestamp, arrow, name)
}

// ScenarioEnded prints the terminal status for one scenario. Failure messages
// are truncated to the first line — the full failure context is rendered by
// PrintConsole at the end of the run.
func (s *StreamSink) ScenarioEnded(name string, status Status, duration time.Duration, failureMessage string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	timestamp := s.painter.paint(ansiGray, time.Now().Format("15:04:05"))

	switch status {
	case StatusPass:
		_, _ = fmt.Fprintf(s.out, "%s %s scenario %q PASS in %s\n",
			timestamp, s.painter.paint(ansiGreen, "✓"), name, duration.Round(time.Millisecond))
	case StatusFail:
		summary := firstLine(failureMessage)
		if summary == "" {
			_, _ = fmt.Fprintf(s.out, "%s %s scenario %q FAIL in %s\n",
				timestamp, s.painter.paint(ansiRed, "✗"), name, duration.Round(time.Millisecond))

			return
		}

		_, _ = fmt.Fprintf(s.out, "%s %s scenario %q FAIL in %s: %s\n",
			timestamp, s.painter.paint(ansiRed, "✗"), name, duration.Round(time.Millisecond), summary)
	case StatusSkip:
		_, _ = fmt.Fprintf(s.out, "%s %s scenario %q SKIP\n",
			timestamp, s.painter.paint(ansiYellow, "○"), name)
	case StatusUnknown:
		fallthrough
	default:
		_, _ = fmt.Fprintf(s.out, "%s %s scenario %q %s in %s\n",
			timestamp, s.painter.paint(ansiGray, "?"), name, status, duration.Round(time.Millisecond))
	}
}

// Heartbeat prints the list of scenarios still in flight along with each
// one's elapsed time. The runtime only invokes this when verbose mode is
// enabled, so a print on every tick is the desired behavior.
func (s *StreamSink) Heartbeat(active []ActiveScenario) {
	if len(active) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	timestamp := s.painter.paint(ansiGray, time.Now().Format("15:04:05"))
	label := s.painter.paint(ansiCyan, "…")

	_, _ = fmt.Fprintf(s.out, "%s %s heartbeat: %d scenario(s) still running\n",
		timestamp, label, len(active))

	for _, scenario := range active {
		_, _ = fmt.Fprintf(s.out, "    - %q for %s\n",
			scenario.Name, scenario.Elapsed.Round(time.Second))
	}
}

// SuiteEnded prints a terminal line once every scenario has finished. The
// final, detailed report is still rendered separately by PrintConsole.
func (s *StreamSink) SuiteEnded(duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	label := s.painter.paint(ansiBlue, "Suite")
	_, _ = fmt.Fprintf(s.out, "%s: finished in %s\n", label, duration.Round(time.Millisecond))
}

// firstLine returns the first non-empty line of s, or s itself if it has no
// newline. Used to keep streamed failure summaries to one line.
func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}

	return s
}
