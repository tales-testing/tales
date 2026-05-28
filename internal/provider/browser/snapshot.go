package browser

// Snapshot is the post-step DOM snapshot the provider records so the
// runtime's capture helpers (text, attribute, browser.url, browser.title)
// can read consistent state without re-issuing CDP queries from the
// runtime layer.
type Snapshot struct {
	URL   string
	Title string
	DOM   string
}
