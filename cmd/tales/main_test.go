package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperxlab/tales/internal/version"
	urfavecli "github.com/urfave/cli/v2"
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

// TestTagFilterParsedRegardlessOfFlagPosition is the regression test
// for the bug where `tales test <path> --tag X` silently dropped the
// --tag filter because urfave/cli/v2 stops parsing flags at the first
// positional argument. The fix is the reorderArgs pre-parser plumbed
// in main(); this test reproduces the same plumbing at a smaller scale
// and asserts that the StringSliceFlag value is received whether the
// flag is placed before or after the positional path.
func TestTagFilterParsedRegardlessOfFlagPosition(t *testing.T) {
	// Intentionally not parallel: urfave/cli/v2's StringSliceFlag stores
	// its value on the flag struct itself, which would race across
	// parallel sub-tests sharing the same App definition.
	cases := []struct {
		name string
		args []string
	}{
		{
			name: "flag before path",
			args: []string{"tales", "test", "--tag", "demo", "./e2e/pass"},
		},
		{
			name: "flag after path",
			args: []string{"tales", "test", "./e2e/pass", "--tag", "demo"},
		},
		{
			name: "multiple flags interleaved with path",
			args: []string{"tales", "test", "./e2e/pass", "--tag", "demo", "--no-progress", "--seed", "42"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedTags []string

			var capturedArgs []string

			app := &urfavecli.App{
				Commands: []*urfavecli.Command{{
					Name: "test",
					Flags: []urfavecli.Flag{
						&urfavecli.StringSliceFlag{Name: "tag"},
						&urfavecli.BoolFlag{Name: "no-progress"},
						&urfavecli.Int64Flag{Name: "seed"},
					},
					Action: func(c *urfavecli.Context) error {
						capturedTags = c.StringSlice("tag")
						capturedArgs = c.Args().Slice()

						return nil
					},
				}},
			}

			if err := app.Run(reorderArgs(tc.args, collectBoolFlags(app))); err != nil {
				t.Fatalf("app.Run: %v", err)
			}

			if !reflect.DeepEqual(capturedTags, []string{"demo"}) {
				t.Fatalf("got tags %v, want [demo]", capturedTags)
			}

			if !reflect.DeepEqual(capturedArgs, []string{"./e2e/pass"}) {
				t.Fatalf("got positional args %v, want [./e2e/pass]", capturedArgs)
			}
		})
	}
}
