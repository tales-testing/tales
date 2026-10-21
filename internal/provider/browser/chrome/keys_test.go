package chrome

import (
	"errors"
	"testing"
	"unicode/utf8"

	"github.com/chromedp/chromedp/kb"
	"github.com/tales-testing/tales/internal/model"
)

// Every accepted key name must reach chromedp as the single rune its kb
// table encodes as that key. chromedp.KeyEvent sends one key event per
// rune, so a name passed through unchanged is typed letter by letter
// (issue #80: "Enter" typed E, n, t, e, r into the field).
func TestKeyEventTextMapsEveryKeyName(t *testing.T) {
	t.Parallel()

	for _, name := range model.BrowserKeyNames() {
		text, err := keyEventText(name)
		if err != nil {
			t.Errorf("keyEventText(%q) returned %v", name, err)

			continue
		}

		if utf8.RuneCountInString(text) != 1 {
			t.Errorf("keyEventText(%q) = %q, want a single rune so chromedp sends one key", name, text)

			continue
		}

		r, _ := utf8.DecodeRuneInString(text)

		key, ok := kb.Keys[r]
		if !ok {
			t.Errorf("keyEventText(%q) = %q, which chromedp's kb table does not know", name, text)

			continue
		}

		// The DOM `key` chromedp sends must be the W3C name itself, except
		// for Space, whose W3C key value is a literal space.
		want := name
		if name == "Space" {
			want = " "
		}

		if key.Key != want {
			t.Errorf("keyEventText(%q) presses DOM key %q, want %q", name, key.Key, want)
		}
	}
}

func TestKeyEventTextSpotChecks(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"Enter":      kb.Enter,
		"Tab":        kb.Tab,
		"Escape":     kb.Escape,
		"Backspace":  kb.Backspace,
		"Delete":     kb.Delete,
		"ArrowUp":    kb.ArrowUp,
		"ArrowRight": kb.ArrowRight,
		"PageDown":   kb.PageDown,
		"F12":        kb.F12,
		"Space":      " ",
	}

	for name, want := range cases {
		got, err := keyEventText(name)
		if err != nil {
			t.Fatalf("keyEventText(%q) returned %v", name, err)
		}

		if got != want {
			t.Errorf("keyEventText(%q) = %q, want %q", name, got, want)
		}
	}
}

// A single character keeps today's behavior: it is sent as is, raw
// control characters included.
func TestKeyEventTextKeepsSingleCharacters(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"a", "Z", "1", " ", "\r", "\t", "é"} {
		got, err := keyEventText(key)
		if err != nil {
			t.Fatalf("keyEventText(%q) returned %v", key, err)
		}

		if got != key {
			t.Errorf("keyEventText(%q) = %q, want it unchanged", key, got)
		}
	}
}

func TestKeyEventTextRejectsUnknownNames(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"Entr", "enter", "abc", ""} {
		got, err := keyEventText(key)
		if err == nil {
			t.Fatalf("keyEventText(%q) = %q, want an error rather than typing it", key, got)
		}

		if !errors.Is(err, model.ErrUnknownBrowserKey) {
			t.Errorf("keyEventText(%q) error %v must wrap model.ErrUnknownBrowserKey", key, err)
		}
	}
}
