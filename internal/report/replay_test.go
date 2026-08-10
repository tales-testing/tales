package report

import (
	"bytes"
	"strings"
	"testing"
)

func failedSuite(inputPath string) *SuiteResult {
	return &SuiteResult{
		Seed:      1234,
		InputPath: inputPath,
		Scenarios: []*ScenarioResult{{
			File:    "e2e/pass/blog.tales",
			Name:    "Blog flow",
			Status:  StatusFail,
			Failure: &ErrorDetail{Kind: "assertion", Message: "boom"},
		}},
	}
}

func replayLine(t *testing.T, result *SuiteResult) string {
	t.Helper()

	var buf bytes.Buffer

	if err := PrintConsoleWithOptions(&buf, result, ConsoleOptions{}); err != nil {
		t.Fatalf("print console: %v", err)
	}

	for line := range strings.SplitSeq(buf.String(), "\n") {
		if strings.Contains(line, "replay:") {
			return strings.TrimSpace(line)
		}
	}

	t.Fatal("no replay line printed for a failed scenario")

	return ""
}

// The replay command must be copy-pasteable. Printing the scenario's own file
// drops the sibling _config.tales and keywords/ that a directory run loads, so
// the suggested command fails on paste.
func TestReplayCommandUsesTheCLIRoot(t *testing.T) {
	t.Parallel()

	got := replayLine(t, failedSuite("e2e/pass"))

	want := `replay: tales test --seed 1234 --scenario "Blog flow" e2e/pass`
	if got != want {
		t.Fatalf("replay line = %q, want %q", got, want)
	}
}

// Programmatic runs (tests, embedders) leave InputPath empty; the historical
// output is preserved for them.
func TestReplayCommandFallsBackToScenarioFile(t *testing.T) {
	t.Parallel()

	got := replayLine(t, failedSuite(""))

	want := `replay: tales test --seed 1234 --scenario "Blog flow" e2e/pass/blog.tales`
	if got != want {
		t.Fatalf("replay line = %q, want %q", got, want)
	}
}
