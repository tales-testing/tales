package main

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/version"
	urfavecli "github.com/urfave/cli/v3"
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
// --tag filter. urfave/cli/v3 implements its own argv parser that
// supports interspersed flags and positionals out of the box, so we
// assert here that the StringSliceFlag value reaches the action
// whether --tag is placed before or after the positional path.
func TestTagFilterParsedRegardlessOfFlagPosition(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			var capturedTags []string

			var capturedArgs []string

			app := &urfavecli.Command{
				Name: "tales",
				Commands: []*urfavecli.Command{{
					Name: "test",
					Flags: []urfavecli.Flag{
						&urfavecli.StringSliceFlag{Name: "tag"},
						&urfavecli.BoolFlag{Name: "no-progress"},
						&urfavecli.Int64Flag{Name: "seed"},
					},
					Action: func(_ context.Context, cmd *urfavecli.Command) error {
						capturedTags = cmd.StringSlice("tag")
						capturedArgs = cmd.Args().Slice()

						return nil
					},
				}},
			}

			if err := app.Run(context.Background(), tc.args); err != nil {
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
