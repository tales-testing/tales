package chrome

import (
	"context"
	"errors"

	"github.com/chromedp/cdproto/page"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// listenFunc has the shape of chromedp.ListenTarget. It is a parameter of
// abortOnNavigation so the abort logic can be tested without a browser.
type listenFunc func(ctx context.Context, fn func(ev any))

// abortOnNavigation runs call with a context that is cancelled as soon as
// the top-level frame commits a navigation, and reports that case as
// driver.ErrNavigated. Some CDP commands are bound to the document they
// were sent to, and Chrome never answers them once a navigation replaces
// that document: Page.captureScreenshot does this when a page navigates by
// script right after `goto` (issue #79), the same class of hang that
// Reload / Back / Forward work around. Waiting for such a reply only ends
// at the caller's deadline; watching for the navigation lets the caller
// retry against the new document at once.
func abortOnNavigation(ctx context.Context, listen listenFunc, call func(ctx context.Context) error) error {
	callCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	listen(callCtx, func(ev any) {
		if isTopLevelNavigation(ev) {
			cancel(driver.ErrNavigated)
		}
	})

	err := call(callCtx)
	if err != nil && errors.Is(context.Cause(callCtx), driver.ErrNavigated) {
		return driver.ErrNavigated
	}

	return err
}

// isTopLevelNavigation reports whether ev is the commit of a navigation in
// the page's main frame. Sub-frame (iframe) navigations leave the document
// a screenshot is bound to in place, so they do not count.
func isTopLevelNavigation(ev any) bool {
	navigated, ok := ev.(*page.EventFrameNavigated)

	return ok && navigated.Frame != nil && navigated.Frame.ParentID == ""
}
