package chrome

import (
	"fmt"
	"unicode/utf8"

	"github.com/chromedp/chromedp/kb"
	"github.com/tales-testing/tales/internal/model"
)

// namedKeys maps each name of model.BrowserKeyNames onto the rune chromedp's
// kb table encodes as that key. chromedp.KeyEvent sends one key event per
// rune of its argument, so handing it a name as is types the name letter by
// letter instead of pressing the key.
var namedKeys = map[string]string{
	"Enter":      kb.Enter,
	"Tab":        kb.Tab,
	"Escape":     kb.Escape,
	"Backspace":  kb.Backspace,
	"Delete":     kb.Delete,
	"Insert":     kb.Insert,
	"ArrowUp":    kb.ArrowUp,
	"ArrowDown":  kb.ArrowDown,
	"ArrowLeft":  kb.ArrowLeft,
	"ArrowRight": kb.ArrowRight,
	"Home":       kb.Home,
	"End":        kb.End,
	"PageUp":     kb.PageUp,
	"PageDown":   kb.PageDown,
	"Space":      " ",
	"F1":         kb.F1,
	"F2":         kb.F2,
	"F3":         kb.F3,
	"F4":         kb.F4,
	"F5":         kb.F5,
	"F6":         kb.F6,
	"F7":         kb.F7,
	"F8":         kb.F8,
	"F9":         kb.F9,
	"F10":        kb.F10,
	"F11":        kb.F11,
	"F12":        kb.F12,
}

// keyEventText returns the argument chromedp.KeyEvent needs to press key: the
// kb rune for a key name, the key itself for a single character. Any other
// string is an error, never text to type.
func keyEventText(key string) (string, error) {
	if text, ok := namedKeys[key]; ok {
		return text, nil
	}

	if utf8.RuneCountInString(key) == 1 {
		return key, nil
	}

	if err := model.ValidateBrowserKey(key); err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}

	// Only reachable if model.BrowserKeyNames gains a name this table lacks;
	// TestKeyEventTextMapsEveryKeyName keeps the two in step.
	return "", fmt.Errorf("key %q has no chromedp mapping", key)
}
