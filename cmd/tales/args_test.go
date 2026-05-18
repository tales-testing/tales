package main

import (
	"reflect"
	"testing"
)

func TestReorderArgs(t *testing.T) {
	t.Parallel()

	boolFlags := map[string]map[string]struct{}{
		"test": {
			"no-color":    struct{}{},
			"no-progress": struct{}{},
		},
		"doctor": {
			"json": struct{}{},
		},
		"validate": {},
	}

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "flags already before path are untouched",
			in:   []string{"tales", "test", "--tag", "demo", "./e2e/pass"},
			want: []string{"tales", "test", "--tag", "demo", "./e2e/pass"},
		},
		{
			name: "flag after path is hoisted with its value",
			in:   []string{"tales", "test", "./e2e/pass", "--tag", "demo"},
			want: []string{"tales", "test", "--tag", "demo", "./e2e/pass"},
		},
		{
			name: "multiple value flags after path are hoisted in order",
			in:   []string{"tales", "test", "./e2e/pass", "--tag", "demo", "--seed", "42"},
			want: []string{"tales", "test", "--tag", "demo", "--seed", "42", "./e2e/pass"},
		},
		{
			name: "bool flag after path does not consume next positional",
			in:   []string{"tales", "test", "./e2e/pass", "--no-progress"},
			want: []string{"tales", "test", "--no-progress", "./e2e/pass"},
		},
		{
			name: "bool flag interleaved with positional",
			in:   []string{"tales", "test", "./e2e/pass", "--no-progress", "./e2e/fail"},
			want: []string{"tales", "test", "--no-progress", "./e2e/pass", "./e2e/fail"},
		},
		{
			name: "flag using --name=value syntax does not consume next token",
			in:   []string{"tales", "test", "./e2e/pass", "--tag=demo", "extra"},
			want: []string{"tales", "test", "--tag=demo", "./e2e/pass", "extra"},
		},
		{
			name: "double dash terminator preserves trailing positionals verbatim",
			in:   []string{"tales", "test", "./e2e/pass", "--", "--tag", "demo"},
			want: []string{"tales", "test", "./e2e/pass", "--", "--tag", "demo"},
		},
		{
			name: "repeated string-slice flag after path is hoisted twice",
			in:   []string{"tales", "test", "./e2e/pass", "--tag", "demo", "--tag", "smoke"},
			want: []string{"tales", "test", "--tag", "demo", "--tag", "smoke", "./e2e/pass"},
		},
		{
			name: "unknown subcommand passes through untouched",
			in:   []string{"tales", "unknown", "./e2e/pass", "--tag", "demo"},
			want: []string{"tales", "unknown", "./e2e/pass", "--tag", "demo"},
		},
		{
			name: "no arguments returns input untouched",
			in:   []string{"tales"},
			want: []string{"tales"},
		},
		{
			name: "global flag before subcommand is preserved",
			in:   []string{"tales", "--help"},
			want: []string{"tales", "--help"},
		},
		{
			name: "doctor bool flag after no positional",
			in:   []string{"tales", "doctor", "--json"},
			want: []string{"tales", "doctor", "--json"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := reorderArgs(tc.in, boolFlags)

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("reorderArgs(%v)\n  got  %v\n  want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFlagName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"--tag", "tag"},
		{"-t", "t"},
		{"--tag=demo", "tag"},
		{"--no-progress", "no-progress"},
		{"-x=y", "x"},
	}

	for _, tc := range cases {
		if got := flagName(tc.in); got != tc.want {
			t.Errorf("flagName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
