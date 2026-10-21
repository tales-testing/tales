package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrUnknownBrowserKey reports a browser `press` key that is neither a
// single character nor one of BrowserKeyNames.
var ErrUnknownBrowserKey = errors.New("not a single character or a key name")

// browserKeyNames is the closed set of named keys a browser `press` action
// accepts on top of a single character. Each name is spelled exactly as the
// W3C UI Events `KeyboardEvent.key` value, and matching is case-sensitive
// like the W3C names themselves. "Space" is the one exception: its W3C key
// value is a literal " ", which is accepted as a single character too.
var browserKeyNames = []string{
	"Enter", "Tab", "Escape", "Backspace", "Delete", "Insert",
	"ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight",
	"Home", "End", "PageUp", "PageDown", "Space",
	"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12",
}

// BrowserKeyNames returns the named keys a browser `press` action accepts,
// in documentation order. The caller owns the returned slice.
func BrowserKeyNames() []string {
	return slices.Clone(browserKeyNames)
}

// ValidateBrowserKey checks a browser `press` key. A single character (one
// rune, raw control characters such as "\r" included) and the names of
// BrowserKeyNames are valid. Anything else is rejected rather than typed:
// a driver sends one key event per rune, so a misspelled name would land in
// the focused field as text while the action reported a success.
func ValidateBrowserKey(key string) error {
	if utf8.RuneCountInString(key) == 1 || slices.Contains(browserKeyNames, key) {
		return nil
	}

	hint := ""

	for _, name := range browserKeyNames {
		if strings.EqualFold(name, key) {
			hint = fmt.Sprintf(" (did you mean %q?)", name)

			break
		}
	}

	return fmt.Errorf(
		"%q is %w%s; use one of %s (key names are case-sensitive), or fill to type text",
		key, ErrUnknownBrowserKey, hint, strings.Join(browserKeyNames, ", "),
	)
}
