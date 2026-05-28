package browser

import (
	"context"
	"net/url"
	"strings"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// captureForAction is a best-effort screenshot + DOM capture. Capture mode
// gates whether we capture at all on success; failures always capture when
// the mode is anything but None.
func (p *Provider) captureForAction(ctx context.Context, drv driver.Driver, stepDir string, result *provider.ActionResult, forFailure bool) {
	if p.captureMode == provider.CaptureNone {
		return
	}

	if !forFailure && p.captureMode != provider.CaptureActions {
		return
	}

	if stepDir == "" {
		return
	}

	dir := actionArtifactDir(stepDir, result.Index, result.Kind, result.SelectorID)

	if png, err := drv.Screenshot(ctx); err == nil && len(png) > 0 {
		if a, werr := writeScreenshot(dir, png); werr == nil {
			result.Screenshot = a.Path
		}
	}

	if dom, err := drv.OuterHTML(ctx, "html"); err == nil && dom != "" {
		if a, werr := writeDOM(dir, dom); werr == nil {
			result.Hierarchy = a.Path
		}
	}
}

// captureStepLevel writes the end-of-step screenshot + DOM under the step's
// step/ subdirectory. Returns an artifact-list ready for output.Response.
func (p *Provider) captureStepLevel(ctx context.Context, drv driver.Driver, stepDir string, forFailure bool) []Artifact {
	if p.captureMode == provider.CaptureNone {
		return nil
	}

	if !forFailure && p.captureMode == provider.CaptureFailures {
		return nil
	}

	if stepDir == "" {
		return nil
	}

	dir := stepLevelArtifactDir(stepDir)
	out := make([]Artifact, 0, 2)

	if png, err := drv.Screenshot(ctx); err == nil && len(png) > 0 {
		if a, werr := writeScreenshot(dir, png); werr == nil {
			out = append(out, a)
		}
	}

	if dom, err := drv.OuterHTML(ctx, "html"); err == nil && dom != "" {
		if a, werr := writeDOM(dir, dom); werr == nil {
			out = append(out, a)
		}
	}

	return out
}

// resolveURL joins a base URL with a possibly-relative target. Behavior:
//   - empty base → target verbatim
//   - empty target → "" (caller's responsibility to validate before reaching here)
//   - absolute target (has scheme) → target verbatim
//   - relative target → resolved against base via net/url
//
// A malformed base falls back to verbatim concatenation so we never silently
// crash inside an action handler.
func resolveURL(base, target string) string {
	if target == "" || base == "" {
		return target
	}

	if strings.Contains(target, "://") {
		return target
	}

	b, err := url.Parse(base)
	if err != nil {
		return base + target
	}

	t, err := url.Parse(target)
	if err != nil {
		return base + target
	}

	return b.ResolveReference(t).String()
}
