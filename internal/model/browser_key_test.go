package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestBrowserKeyNamesCoverTheDocumentedKeys(t *testing.T) {
	t.Parallel()

	want := []string{
		"Enter", "Tab", "Escape", "Backspace", "Delete", "Insert",
		"ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight",
		"Home", "End", "PageUp", "PageDown", "Space",
	}

	for i := 1; i <= 12; i++ {
		want = append(want, fmt.Sprintf("F%d", i))
	}

	names := BrowserKeyNames()

	for _, name := range want {
		if !slices.Contains(names, name) {
			t.Errorf("BrowserKeyNames() is missing %q", name)
		}
	}

	if len(names) != len(want) {
		t.Errorf("BrowserKeyNames() = %v, want exactly %v", names, want)
	}
}

func TestBrowserKeyNamesReturnsACopy(t *testing.T) {
	t.Parallel()

	names := BrowserKeyNames()
	names[0] = "Mutated"

	if BrowserKeyNames()[0] == "Mutated" {
		t.Fatal("BrowserKeyNames() must not expose the package-level slice")
	}
}

func TestValidateBrowserKeyAccepts(t *testing.T) {
	t.Parallel()

	keys := append([]string{"a", "Z", "1", " ", "\r", "\t", "é", "日"}, BrowserKeyNames()...)

	for _, key := range keys {
		if err := ValidateBrowserKey(key); err != nil {
			t.Errorf("ValidateBrowserKey(%q) = %v, want nil", key, err)
		}
	}
}

func TestValidateBrowserKeyRejects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		key  string
		hint string
	}{
		{name: "typo", key: "Entr"},
		{name: "plain text", key: "abc"},
		{name: "empty", key: ""},
		{name: "unsupported function key", key: "F13"},
		{name: "non-W3C alias", key: "Return"},
		{name: "two runes forming one glyph", key: "é"},
		{name: "wrong case", key: "enter", hint: `did you mean "Enter"?`},
		{name: "wrong case arrow", key: "arrowup", hint: `did you mean "ArrowUp"?`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateBrowserKey(tc.key)
			if err == nil {
				t.Fatalf("ValidateBrowserKey(%q) = nil, want an error", tc.key)
			}

			if !errors.Is(err, ErrUnknownBrowserKey) {
				t.Errorf("error %v must wrap ErrUnknownBrowserKey", err)
			}

			msg := err.Error()

			if !strings.Contains(msg, fmt.Sprintf("%q", tc.key)) {
				t.Errorf("error %q must quote the rejected key %q", msg, tc.key)
			}

			// The message lists every accepted name so the fix is in front
			// of the user, whichever name they were after.
			for _, name := range BrowserKeyNames() {
				if !strings.Contains(msg, name) {
					t.Errorf("error %q must list the accepted name %q", msg, name)
				}
			}

			if tc.hint != "" && !strings.Contains(msg, tc.hint) {
				t.Errorf("error %q must carry the hint %q", msg, tc.hint)
			}

			if tc.hint == "" && strings.Contains(msg, "did you mean") {
				t.Errorf("error %q must not suggest a name for %q", msg, tc.key)
			}
		})
	}
}
