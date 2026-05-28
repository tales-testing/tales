package chrome

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/tales-testing/tales/internal/provider/browser"
)

func chromeDebugf(format string, args ...any) {
	if os.Getenv("TALES_BROWSER_DEBUG") != "1" {
		return
	}

	_, _ = fmt.Fprintf(os.Stderr, "[browser:chrome %s] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}

// DefaultBuilder returns a SessionBuilder that drives Chrome via chromedp.
// One Chrome subprocess is started per target; each scenario gets a fresh
// chromedp.Context off that allocator for incognito-style isolation.
//
// Isolation guarantees (intentional and load-bearing — the user's
// regular Chrome must never be touched by `tales test`):
//
//   - Per-scenario subprocess. Each scenario spawns its own Chrome
//     via chromedp.NewExecAllocator. cleanup() cancels ONLY that
//     allocator context. chromedp translates the cancellation into a
//     Browser.close CDP command followed by SIGTERM (then SIGKILL on
//     grace timeout) to that subprocess's exact PID. Other Chrome
//     instances on the host are not affected.
//   - Isolated profile. Every subprocess gets an explicit
//     --user-data-dir under $TMPDIR/tales-chrome-<random>/, so it
//     shares nothing with the user's normal Chrome profile (cookies,
//     history, extensions, sessions, keychain, …). The temp dir is
//     wiped on cleanup.
//   - No name-based termination. The tales binary never invokes
//     pkill, killall, or any process matcher by name. Every kill is
//     PID-scoped through context cancellation. A unit test
//     (no_pkill_test.go) pins this property.
func DefaultBuilder() browser.SessionBuilder {
	return browser.SessionBuilderFunc{
		BuildFn:    build,
		ScenarioFn: newScenarioContext,
	}
}

// build returns a Session whose only role is to carry the Target around so
// NewScenarioContext can spawn one Chrome subprocess per scenario for full
// cookie / storage isolation. Spec calls out isolation as the priority
// over speed in V1, so we accept the per-scenario startup cost.
func build(_ context.Context, target browser.Target) (*browser.Session, error) {
	return &browser.Session{
		TargetName: target.Name,
		Target:     target,
	}, nil
}

func newScenarioContext(_ context.Context, sess *browser.Session, scenario string) (*browser.ScenarioBrowserCtx, error) {
	execPath, err := Locate(sess.Target.Driver.Executable)
	if err != nil {
		return nil, fmt.Errorf("locate chrome: %w", err)
	}

	// Force an isolated per-allocator user-data-dir under $TMPDIR. chromedp
	// already defaults to a temp dir when UserDataDir is unset, but
	// setting it explicitly:
	//   - makes the path visible in debug logs (auditable)
	//   - guarantees no shared state with the user's regular Chrome
	//     profile, regardless of any host-level Chrome flags users might
	//     have picked up from ~/.zshrc / ~/.bashrc.
	userDataDir, err := os.MkdirTemp("", "tales-chrome-")
	if err != nil {
		return nil, fmt.Errorf("create user data dir: %w", err)
	}

	chromeDebugf("spawning chrome target=%q scenario=%q exec=%q headless=%v viewport=%dx%d user_data_dir=%q args=%v",
		sess.Target.Name, scenario, execPath, sess.Target.Driver.Headless,
		sess.Target.Driver.Viewport.Width, sess.Target.Driver.Viewport.Height,
		userDataDir, sess.Target.Driver.Args)

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(execPath),
		chromedp.UserDataDir(userDataDir),
		chromedp.Flag("headless", sess.Target.Driver.Headless),
		chromedp.WindowSize(sess.Target.Driver.Viewport.Width, sess.Target.Driver.Viewport.Height),
	)

	if sess.Target.Driver.Headless {
		// Headless defaults that match what Puppeteer / Playwright ship
		// for CI environments. Without --no-sandbox, Chrome hangs on
		// startup inside GitHub Actions / most Docker images because
		// the setuid sandbox cannot be created without CAP_SYS_ADMIN.
		// --disable-dev-shm-usage avoids crashes on runners where /dev/shm
		// is tiny (the default 64 MiB on many containers). --disable-gpu
		// removes a warning + saves a few hundred ms of startup time in
		// headless mode where GPU acceleration is irrelevant.
		opts = append(opts,
			chromedp.NoSandbox,
			chromedp.Flag("disable-dev-shm-usage", true),
			chromedp.Flag("disable-gpu", true),
		)
	}

	for _, arg := range sess.Target.Driver.Args {
		name, value := parseFlag(arg)
		opts = append(opts, chromedp.Flag(name, value))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)

	cleanup := func() {
		pid := browserPID(ctx)
		chromeDebugf("cleanup target=%q scenario=%q pid=%d user_data_dir=%q",
			sess.Target.Name, scenario, pid, userDataDir)
		// Cancel both contexts. chromedp owns the SIGTERM → grace → SIGKILL
		// dance, scoped to the subprocess we spawned (PID %d above). It
		// does NOT touch any other Chrome process on the host.
		cancel()
		allocCancel()
		// Wipe the per-scenario user data dir. Best-effort: never blocks
		// scenario teardown on a stale directory.
		if rmErr := os.RemoveAll(userDataDir); rmErr != nil {
			chromeDebugf("user data dir cleanup failed dir=%q err=%v",
				filepath.Clean(userDataDir), rmErr)
		}
	}

	chromeDebugf("calling chromedp.Run target=%q scenario=%q", sess.Target.Name, scenario)

	if err := chromedp.Run(ctx); err != nil {
		cleanup()

		return nil, fmt.Errorf("start chrome: %w", err)
	}

	chromeDebugf("chrome ready target=%q scenario=%q pid=%d",
		sess.Target.Name, scenario, browserPID(ctx))

	return &browser.ScenarioBrowserCtx{
		Driver: NewDriver(ctx, cleanup),
		Cancel: cleanup,
	}, nil
}

// browserPID returns the OS PID of the Chrome subprocess chromedp spawned
// for the given browser context, or 0 when the PID is unavailable (e.g.,
// chrome hasn't started yet, or the browser was started by another
// allocator). The PID is the same one chromedp signals during cleanup —
// surfacing it in debug logs makes the "we only touch our own subprocess"
// guarantee auditable.
func browserPID(ctx context.Context) int {
	c := chromedp.FromContext(ctx)
	if c == nil || c.Browser == nil || c.Browser.Process() == nil {
		return 0
	}

	return c.Browser.Process().Pid
}

// parseFlag turns a user-supplied Chrome command-line flag string into
// the (name, value) pair chromedp.Flag wants. Users typically write the
// full `--name` or `--name=value` form they would pass to Chrome
// directly; chromedp expects bare names and a value (bool for boolean
// flags, string for valued flags). Examples:
//
//	"--disable-gpu"            → ("disable-gpu", true)
//	"--proxy-server=http://p"  → ("proxy-server", "http://p")
//	"user-agent=Tales/0.1"     → ("user-agent", "Tales/0.1")
//	""                         → ("", true) — caller should filter empty entries
func parseFlag(s string) (string, any) {
	for len(s) > 0 && s[0] == '-' {
		s = s[1:]
	}

	if name, value, ok := strings.Cut(s, "="); ok {
		return name, value
	}

	return s, true
}
