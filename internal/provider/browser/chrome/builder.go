package chrome

import (
	"context"
	"fmt"

	"github.com/chromedp/chromedp"
	"github.com/tales-testing/tales/internal/provider/browser"
)

// DefaultBuilder returns a SessionBuilder that drives Chrome via chromedp.
// One Chrome subprocess is started per target; each scenario gets a fresh
// chromedp.Context off that allocator for incognito-style isolation.
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

func newScenarioContext(_ context.Context, sess *browser.Session, _ string) (*browser.ScenarioBrowserCtx, error) {
	execPath, err := Locate(sess.Target.Driver.Executable)
	if err != nil {
		return nil, fmt.Errorf("locate chrome: %w", err)
	}

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(execPath),
		chromedp.Flag("headless", sess.Target.Driver.Headless),
		chromedp.WindowSize(sess.Target.Driver.Viewport.Width, sess.Target.Driver.Viewport.Height),
	)

	for _, arg := range sess.Target.Driver.Args {
		opts = append(opts, chromedp.Flag(trimDash(arg), true))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)

	cleanup := func() {
		cancel()
		allocCancel()
	}

	if err := chromedp.Run(ctx); err != nil {
		cleanup()

		return nil, fmt.Errorf("start chrome: %w", err)
	}

	return &browser.ScenarioBrowserCtx{
		Driver: NewDriver(ctx, cleanup),
		Cancel: cleanup,
	}, nil
}

// trimDash strips leading "--" from a chromedp flag string. chromedp.Flag
// expects bare names ("disable-gpu"), but users typically write
// "--disable-gpu" in their config because that is what Chrome itself
// accepts.
func trimDash(s string) string {
	for len(s) > 0 && s[0] == '-' {
		s = s[1:]
	}

	return s
}
