package browser

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// debugEnabled toggles verbose stderr logging of the browser provider's
// internal lifecycle. Enabled when TALES_BROWSER_DEBUG=1.
var (
	debugEnabledOnce sync.Once
	debugEnabledVal  bool
)

func debugEnabled() bool {
	debugEnabledOnce.Do(func() {
		debugEnabledVal = os.Getenv("TALES_BROWSER_DEBUG") == "1"
	})

	return debugEnabledVal
}

// debugf prints a timestamped diagnostic to stderr when debug mode is on.
// The message is prefixed with the calling label so it is easy to grep.
func debugf(label, format string, args ...any) {
	if !debugEnabled() {
		return
	}

	_, _ = fmt.Fprintf(os.Stderr, "[browser:%s %s] %s\n", label, time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}
