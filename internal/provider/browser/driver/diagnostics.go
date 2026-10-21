package driver

import (
	"math"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tales-testing/tales/internal/diagnostic"
)

// Recording bounds. A recording is evidence for a failure report: it must
// stay small whatever the page does, so each list keeps only its most
// recent entries and each text is cut. The entries closest to the failure
// are the ones worth keeping, hence most recent rather than first.
const (
	// MaxConsoleEntries caps the console messages and exceptions kept.
	MaxConsoleEntries = 1000
	// MaxNetworkEntries caps the network events kept (a request usually
	// yields three: request, response, finished).
	MaxNetworkEntries = 2000
	// MaxTextBytes caps the text of one console entry.
	MaxTextBytes = 4096
	// MaxURLBytes caps one recorded URL, after its query string is gone.
	MaxURLBytes = 2048
	// MaxStackFrames caps the frames kept from one exception stack.
	MaxStackFrames = 20
	// maxPendingRequests caps the requests remembered between their
	// request event and their last event, to name the method and URL of a
	// response whose request was dropped or sent before the step started.
	maxPendingRequests = 4096
)

// Console entry types.
const (
	ConsoleTypeConsole   = "console"
	ConsoleTypeException = "exception"
)

// Network entry types, one per CDP event recorded.
const (
	NetworkTypeRequest  = "request"
	NetworkTypeResponse = "response"
	NetworkTypeFinished = "finished"
	NetworkTypeFailed   = "failed"
)

// ConsoleEntry is one console message (console.log, console.error, ...) or
// one uncaught exception. It is written as one line of console.jsonl.
type ConsoleEntry struct {
	// Time is when the entry was recorded (diagnostic.TimestampLayout).
	Time string `json:"time"`
	// Type is ConsoleTypeConsole or ConsoleTypeException.
	Type string `json:"type"`
	// Level is the console method (log, info, warning, error, debug,
	// ...), or "error" for an exception.
	Level string `json:"level"`
	// Text is the message: the console arguments rendered and joined by
	// a space, or the exception message.
	Text string `json:"text"`
	// URL, Line and Column locate the call or the throw. Line and Column
	// are 1-based. The URL carries no query string.
	URL    string `json:"url,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	// Stack is the exception's call stack, innermost frame first.
	Stack []StackFrame `json:"stack,omitempty"`
	// Truncated reports that Text, URL or Stack was cut to its bound.
	Truncated bool `json:"truncated,omitempty"`
}

// StackFrame is one frame of an exception stack.
type StackFrame struct {
	Function string `json:"function,omitempty"`
	URL      string `json:"url,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
}

// NetworkEntry is one network event of one request. It is written as one
// line of network.jsonl. Header values, cookies, bodies and query strings
// are never part of it.
type NetworkEntry struct {
	// Time is when the event was recorded (diagnostic.TimestampLayout).
	Time string `json:"time"`
	// Type is one of the NetworkType constants.
	Type string `json:"type"`
	// RequestID links the events of one request.
	RequestID string `json:"request_id"`
	Method    string `json:"method,omitempty"`
	// URL is the request URL without its query string, fragment or
	// credentials.
	URL string `json:"url,omitempty"`
	// ResourceType is the browser's classification: Document, Fetch,
	// XHR, Script, Image, ...
	ResourceType string `json:"resource_type,omitempty"`
	// Status, StatusText and MimeType describe a response.
	Status     int    `json:"status,omitempty"`
	StatusText string `json:"status_text,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	// FromCache reports a response served from the disk cache, the
	// prefetch cache or a service worker.
	FromCache bool `json:"from_cache,omitempty"`
	// Redirect marks the response of a request that was redirected; a
	// request event for the new URL follows with the same RequestID.
	Redirect bool `json:"redirect,omitempty"`
	// ErrorText, Canceled, BlockedReason and CORSError describe a
	// request that failed.
	ErrorText     string `json:"error_text,omitempty"`
	Canceled      bool   `json:"canceled,omitempty"`
	BlockedReason string `json:"blocked_reason,omitempty"`
	CORSError     string `json:"cors_error,omitempty"`
	// EncodedBytes is the number of bytes the request received over the
	// network, set on its finished event.
	EncodedBytes int64 `json:"encoded_bytes,omitempty"`
	// ElapsedMS is the time since the request was sent, by the browser's
	// clock. Absent on the request event itself, and when the request was
	// sent before the recording could see it.
	ElapsedMS *float64 `json:"elapsed_ms,omitempty"`
	// Truncated reports that URL was cut to MaxURLBytes.
	Truncated bool `json:"truncated,omitempty"`

	// BrowserTime is the browser's monotonic timestamp of the event, the
	// clock ElapsedMS is measured with. Not written out.
	BrowserTime time.Time `json:"-"`
}

// Diagnostics is what a driver recorded about the page since its last
// StartDiagnostics, oldest entry first.
type Diagnostics struct {
	Console []ConsoleEntry
	// ConsoleDropped counts the oldest console entries dropped to stay
	// within MaxConsoleEntries.
	ConsoleDropped int
	Network        []NetworkEntry
	// NetworkDropped counts the oldest network entries dropped to stay
	// within MaxNetworkEntries.
	NetworkDropped int
}

// PageState describes the page at one moment: what has the focus, how it is
// scrolled, how large the viewport is. It is written as state.json.
type PageState struct {
	// URL is the document URL without its query string, fragment or
	// credentials.
	URL        string `json:"url"`
	Title      string `json:"title"`
	ReadyState string `json:"ready_state"`
	// ActiveElement is document.activeElement, followed into open shadow
	// roots. Nil when nothing has the focus.
	ActiveElement *ElementDescription `json:"active_element"`
	Viewport      Viewport            `json:"viewport"`
	Scroll        ScrollOffsets       `json:"scroll"`
	// Document is the scrollable size of the document.
	Document Size `json:"document"`
}

// ElementDescription names an element without its content: never its value
// or its text, which may be a password.
type ElementDescription struct {
	// Selector is a CSS-selector-like summary of the fields below, e.g.
	// `input#email.wide[data-testid="login.email"][name="email"]`. It is
	// meant to be read, not guaranteed to match one element.
	Selector string   `json:"selector"`
	Tag      string   `json:"tag"`
	ID       string   `json:"id,omitempty"`
	TestID   string   `json:"data_testid,omitempty"`
	Name     string   `json:"name,omitempty"`
	Type     string   `json:"type,omitempty"`
	Classes  []string `json:"classes,omitempty"`
}

// Viewport is the layout viewport in CSS pixels.
type Viewport struct {
	Width            float64 `json:"width"`
	Height           float64 `json:"height"`
	DevicePixelRatio float64 `json:"device_pixel_ratio"`
}

// ScrollOffsets is how far the window is scrolled, in CSS pixels.
type ScrollOffsets struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Size is a width and a height in CSS pixels.
type Size struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// DescribeElement fills in d.Selector from the other fields and returns d.
func DescribeElement(d ElementDescription) ElementDescription {
	var b strings.Builder

	b.WriteString(d.Tag)

	if d.ID != "" {
		if isPlainIdent(d.ID) {
			b.WriteString("#" + d.ID)
		} else {
			b.WriteString(attributeSelector("id", d.ID))
		}
	}

	for _, class := range d.Classes {
		if isPlainIdent(class) {
			b.WriteString("." + class)
		}
	}

	if d.TestID != "" {
		b.WriteString(attributeSelector("data-testid", d.TestID))
	}

	if d.Name != "" {
		b.WriteString(attributeSelector("name", d.Name))
	}

	if d.Type != "" {
		b.WriteString(attributeSelector("type", d.Type))
	}

	d.Selector = b.String()

	return d
}

func attributeSelector(name, value string) string {
	return "[" + name + `="` + strings.ReplaceAll(value, `"`, `\"`) + `"]`
}

// isPlainIdent reports whether s can follow "#" or "." in a CSS selector
// without escaping.
func isPlainIdent(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}

	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}

	return true
}

// StripURL keeps the scheme, host and path of raw and drops the parts that
// may carry secrets: the query string, the fragment and any user:password.
// A data: URL keeps its scheme and media type, never its payload. Text that
// does not parse as a URL is cut at its first '?' or '#'.
func StripURL(raw string) string {
	if raw == "" {
		return ""
	}

	if len(raw) >= len("data:") && strings.EqualFold(raw[:len("data:")], "data:") {
		head, _, _ := strings.Cut(raw, ",")

		return head
	}

	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" {
		return stripUnparsedURL(raw)
	}

	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""

	return u.String()
}

// stripUnparsedURL is StripURL for text url.Parse refuses (or reads as an
// opaque URL such as blob:): it cuts at the first '?' or '#' and drops a
// userinfo found between "://" and the next '/'.
func stripUnparsedURL(raw string) string {
	if cut := strings.IndexAny(raw, "?#"); cut >= 0 {
		raw = raw[:cut]
	}

	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return raw
	}

	authority := rest
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		authority = rest[:slash]
	}

	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		rest = rest[at+1:]
	}

	return scheme + "://" + rest
}

// truncateUTF8 cuts s to at most limit bytes without splitting a rune, and
// reports whether it cut anything.
func truncateUTF8(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}

	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut], true
}

// Recorder keeps the console and network entries of one browsing context.
// A driver feeds it from its event listener, which must never block, so
// every Record call only takes a mutex, sanitizes the entry and stores it in
// a bounded buffer. It is safe for concurrent use.
type Recorder struct {
	mu      sync.Mutex
	now     func() time.Time
	started bool
	console ring[ConsoleEntry]
	network ring[NetworkEntry]
	pending map[string]pendingRequest
}

// pendingRequest is what the recorder remembers of a request until its
// finished or failed event, to complete the request's later entries.
type pendingRequest struct {
	method       string
	url          string
	resourceType string
	sentAt       time.Time
}

// RecorderOption configures a Recorder.
type RecorderOption func(*Recorder)

// WithRecorderClock replaces the clock that stamps each entry's Time.
func WithRecorderClock(now func() time.Time) RecorderOption {
	return func(r *Recorder) { r.now = now }
}

// WithRecorderLimits replaces MaxConsoleEntries and MaxNetworkEntries.
// Values below 1 keep the default.
func WithRecorderLimits(console, network int) RecorderOption {
	return func(r *Recorder) {
		if console > 0 {
			r.console.limit = console
		}

		if network > 0 {
			r.network.limit = network
		}
	}
}

// NewRecorder builds a recorder that records nothing until Start.
func NewRecorder(opts ...RecorderOption) *Recorder {
	r := &Recorder{
		now:     time.Now,
		console: ring[ConsoleEntry]{limit: MaxConsoleEntries},
		network: ring[NetworkEntry]{limit: MaxNetworkEntries},
		pending: map[string]pendingRequest{},
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Start discards every entry recorded so far and records from now on. The
// requests still in flight are remembered, so a response that arrives
// after Start still names its method and URL.
func (r *Recorder) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.started = true
	r.console.reset()
	r.network.reset()
}

// Snapshot returns a copy of the entries recorded since Start, or nil when
// Start was never called.
func (r *Recorder) Snapshot() *Diagnostics {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return nil
	}

	return &Diagnostics{
		Console:        r.console.snapshot(),
		ConsoleDropped: r.console.dropped,
		Network:        r.network.snapshot(),
		NetworkDropped: r.network.dropped,
	}
}

// RecordConsole stores one console entry: it stamps the time when the entry
// carries none, strips the query string from every URL and cuts the text
// and the stack to their bounds.
func (r *Recorder) RecordConsole(e ConsoleEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return
	}

	if e.Time == "" {
		e.Time = diagnostic.FormatTimestamp(r.now())
	}

	var cut bool

	e.Text, cut = truncateUTF8(e.Text, MaxTextBytes)
	e.Truncated = e.Truncated || cut

	e.URL, cut = sanitizeURL(e.URL)
	e.Truncated = e.Truncated || cut

	e.Stack, cut = sanitizeStack(e.Stack)
	e.Truncated = e.Truncated || cut

	r.console.push(e)
}

// sanitizeStack copies at most MaxStackFrames frames with their URLs
// stripped, and reports whether anything was cut. The copy keeps the
// caller's slice untouched.
func sanitizeStack(in []StackFrame) ([]StackFrame, bool) {
	if len(in) == 0 {
		return nil, false
	}

	cut := len(in) > MaxStackFrames
	out := make([]StackFrame, 0, min(len(in), MaxStackFrames))

	for _, frame := range in[:min(len(in), MaxStackFrames)] {
		var urlCut bool

		frame.URL, urlCut = sanitizeURL(frame.URL)
		out = append(out, frame)
		cut = cut || urlCut
	}

	return out, cut
}

// RecordNetwork stores one network entry. It strips the URL, and completes
// a response, finished or failed entry from its request: method, URL,
// resource type when missing, and the time elapsed since it was sent.
func (r *Recorder) RecordNetwork(e NetworkEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return
	}

	if e.Time == "" {
		e.Time = diagnostic.FormatTimestamp(r.now())
	}

	var cut bool

	e.URL, cut = sanitizeURL(e.URL)
	e.Truncated = e.Truncated || cut

	if e.Type == NetworkTypeRequest {
		r.remember(e)
		e.ElapsedMS = nil
	} else {
		r.complete(&e)
	}

	r.network.push(e)
}

// remember keeps a request until its last event. A request id seen again
// is a redirect: the new URL and send time replace the old ones.
func (r *Recorder) remember(e NetworkEntry) {
	if _, known := r.pending[e.RequestID]; !known && len(r.pending) >= maxPendingRequests {
		return
	}

	r.pending[e.RequestID] = pendingRequest{
		method:       e.Method,
		url:          e.URL,
		resourceType: e.ResourceType,
		sentAt:       e.BrowserTime,
	}
}

// complete fills a later event of a request in from what remember kept,
// and forgets the request once it finished or failed.
func (r *Recorder) complete(e *NetworkEntry) {
	req, ok := r.pending[e.RequestID]
	if !ok {
		return
	}

	if e.Method == "" {
		e.Method = req.method
	}

	if e.URL == "" {
		e.URL = req.url
	}

	if e.ResourceType == "" {
		e.ResourceType = req.resourceType
	}

	if !req.sentAt.IsZero() && !e.BrowserTime.IsZero() {
		elapsed := math.Round(float64(e.BrowserTime.Sub(req.sentAt).Microseconds())/100) / 10
		e.ElapsedMS = &elapsed
	}

	if e.Type == NetworkTypeFinished || e.Type == NetworkTypeFailed {
		delete(r.pending, e.RequestID)
	}
}

// sanitizeURL strips raw and cuts it to MaxURLBytes, reporting a cut.
func sanitizeURL(raw string) (string, bool) {
	return truncateUTF8(StripURL(raw), MaxURLBytes)
}

// ring is a bounded FIFO that drops its oldest item when full.
type ring[T any] struct {
	items   []T
	start   int
	limit   int
	dropped int
}

func (r *ring[T]) push(v T) {
	if len(r.items) < r.limit {
		r.items = append(r.items, v)

		return
	}

	r.items[r.start] = v
	r.start = (r.start + 1) % r.limit
	r.dropped++
}

// snapshot copies the items, oldest first.
func (r *ring[T]) snapshot() []T {
	out := make([]T, 0, len(r.items))
	out = append(out, r.items[r.start:]...)

	return append(out, r.items[:r.start]...)
}

func (r *ring[T]) reset() {
	clear(r.items)
	r.items = r.items[:0]
	r.start = 0
	r.dropped = 0
}
