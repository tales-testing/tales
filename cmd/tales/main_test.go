package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hyperxlab/tales/internal/version"
)

func TestPrintVersion(t *testing.T) {
	t.Parallel()

	info := &version.Info{
		Version:      "0.1.0",
		GitCommit:    "abc1234",
		GitTreeState: "clean",
		BuildDate:    "2026-05-17T10:00:00Z",
		GoVersion:    "go1.26.0",
		Compiler:     "gc",
		Platform:     "linux/amd64",
	}

	var buf bytes.Buffer
	printVersion(&buf, "tales", info)

	out := buf.String()

	wantSubstrings := []string{
		"tales version: 0.1.0",
		"(build: 2026-05-17T10:00:00Z)",
		"commit: abc1234",
		"Go runtime version: go1.26.0",
		"Platform: linux/amd64",
	}

	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Errorf("printVersion output missing %q\nfull output:\n%s", want, out)
		}
	}
}
