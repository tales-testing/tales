// Package chrome holds the production Chrome / Chromium binding used by
// the Tales browser provider. It glues together the abstract Driver
// interface from internal/provider/browser/driver and chromedp.
package chrome

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Locate returns the path to a Chrome / Chromium executable. Resolution
// order:
//  1. override (the target's driver.executable, when set)
//  2. CHROME_PATH environment variable
//  3. PATH lookup of well-known names (google-chrome, chromium, …)
//  4. OS-specific install locations
//
// The returned path is verified with os.Stat to fail fast on a stale
// reference. A missing binary surfaces as a clear error mentioning
// CHROME_PATH and `driver.executable` so the user knows how to fix it.
func Locate(override string) (string, error) {
	if override != "" {
		if err := verifyExecutable(override); err != nil {
			return "", fmt.Errorf("driver.executable: %w", err)
		}

		return override, nil
	}

	if env := os.Getenv("CHROME_PATH"); env != "" {
		if err := verifyExecutable(env); err != nil {
			return "", fmt.Errorf("CHROME_PATH: %w", err)
		}

		return env, nil
	}

	for _, name := range pathCandidates() {
		if p, ok := lookPath(name); ok {
			return p, nil
		}
	}

	for _, p := range osCandidatesFn() {
		if err := verifyExecutable(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("chrome executable not found; install Chrome/Chromium or set CHROME_PATH")
}

func pathCandidates() []string {
	return []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
		"chrome",
	}
}

// osCandidatesFn lets tests stub the OS install-path probe so they pass
// deterministically on machines where Chrome is installed.
var osCandidatesFn = osCandidates

func osCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
		}
	case "linux":
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
		}
	case "windows":
		return []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
		}
	}

	return nil
}

func verifyExecutable(p string) error {
	//nolint:gosec // G703: probing user-supplied Chrome paths is the whole point of this function; we only read metadata.
	info, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("stat %q: %w", p, err)
	}

	if info.IsDir() {
		return fmt.Errorf("%q is a directory", p)
	}

	return nil
}

// lookPath is a tiny exec.LookPath wrapper that returns (path, true) when
// the binary exists. Kept as a method so tests can stub it in the future
// without pulling exec into the unit-test path.
func lookPath(name string) (string, bool) {
	if strings.ContainsAny(name, string(os.PathSeparator)) {
		if err := verifyExecutable(name); err != nil {
			return "", false
		}

		return name, true
	}

	path, err := execLookPath(name)
	if err != nil {
		return "", false
	}

	return path, true
}
