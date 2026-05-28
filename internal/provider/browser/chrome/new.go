package chrome

import (
	"github.com/tales-testing/tales/internal/provider/browser"
)

// New returns a browser.Provider wired to drive Chrome via chromedp. It is
// the production entry point used by the CLI; tests use browser.New with a
// custom SessionBuilder.
func New(opts ...browser.Option) *browser.Provider {
	return browser.New(append([]browser.Option{browser.WithSessionBuilder(DefaultBuilder())}, opts...)...)
}
