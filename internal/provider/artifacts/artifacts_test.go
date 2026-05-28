package artifacts

import (
	"strings"
	"testing"
)

func TestSafePathSegment(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                 "unnamed",
		"  ":               "unnamed",
		"hello":            "hello",
		"Hello World!":     "Hello_World",
		"path/with/slash":  "path_with_slash",
		"!!!":              "unnamed",
		"a.b-c_d":          "a.b-c_d",
		"trailing___":      "trailing",
		"keep_chars_-.OK":  "keep_chars_-.OK",
	}

	for in, want := range cases {
		if got := SafePathSegment(in); got != want {
			t.Errorf("SafePathSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHashDeterministic(t *testing.T) {
	t.Parallel()

	a := Hash("file.tales", "scenario A")
	b := Hash("file.tales", "scenario A")
	c := Hash("file.tales", "scenario B")

	if a != b {
		t.Fatalf("hash should be deterministic: %q vs %q", a, b)
	}

	if a == c {
		t.Fatalf("hash collision between distinct inputs: %q == %q", a, c)
	}

	if len(a) != 8 {
		t.Fatalf("hash should be 8 chars, got %d", len(a))
	}
}

func TestDirContainsExpectedSegments(t *testing.T) {
	t.Parallel()

	got := Dir("build/artifacts", "browser", "/abs/file.tales", "Login flow", "open", "step", 1)
	if !strings.Contains(got, "browser") {
		t.Fatalf("expected provider segment, got %q", got)
	}

	if !strings.HasSuffix(got, "attempt-1") {
		t.Fatalf("expected attempt-N suffix, got %q", got)
	}

	if !strings.Contains(got, "Login_flow") {
		t.Fatalf("expected sanitized scenario segment, got %q", got)
	}

	// Defaults applied.
	gotDefault := Dir("", "browser", "f", "s", "step", "", 0)
	if !strings.HasPrefix(gotDefault, "build/artifacts/browser") {
		t.Fatalf("expected default base + provider, got %q", gotDefault)
	}

	if !strings.HasSuffix(gotDefault, "attempt-1") {
		t.Fatalf("expected default attempt-1, got %q", gotDefault)
	}
}
