package chrome

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// StartDiagnostics implements driver.Driver. The first call installs one
// event listener on the browsing context, which lives as long as the
// context; every call then restarts the recording.
//
// No CDP domain is enabled here: chromedp enables Runtime, Network and Log
// when it attaches to the page (chromedp.Context.attachTarget), so the
// events already flow and listening to them changes nothing in the page.
// The listener runs on chromedp's event loop and must never block it: it
// only maps the event and appends it to the recorder under a mutex.
func (d *chromedpDriver) StartDiagnostics(_ context.Context) error {
	d.diagMu.Lock()
	defer d.diagMu.Unlock()

	if d.recorder == nil {
		recorder := driver.NewRecorder()

		chromedp.ListenTarget(d.ctx, func(ev any) { recordEvent(recorder, ev) })

		d.recorder = recorder
	}

	d.recorder.Start()

	return nil
}

// Diagnostics implements driver.Driver.
func (d *chromedpDriver) Diagnostics() *driver.Diagnostics {
	d.diagMu.Lock()
	recorder := d.recorder
	d.diagMu.Unlock()

	if recorder == nil {
		return nil
	}

	return recorder.Snapshot()
}

// recordEvent maps the CDP events a recording keeps onto recorder entries
// and ignores every other event.
func recordEvent(recorder *driver.Recorder, ev any) {
	switch e := ev.(type) {
	case *runtime.EventConsoleAPICalled:
		recorder.RecordConsole(consoleEntry(e))
	case *runtime.EventExceptionThrown:
		recorder.RecordConsole(exceptionEntry(e))
	case *network.EventRequestWillBeSent:
		for _, entry := range requestEntries(e) {
			recorder.RecordNetwork(entry)
		}
	case *network.EventResponseReceived:
		recorder.RecordNetwork(responseEntry(e))
	case *network.EventLoadingFinished:
		recorder.RecordNetwork(finishedEntry(e))
	case *network.EventLoadingFailed:
		recorder.RecordNetwork(failedEntry(e))
	}
}

// consoleEntry maps a console.* call. Its location is the top frame of the
// call's stack; the rest of the stack is left out, a console call is not an
// error.
func consoleEntry(e *runtime.EventConsoleAPICalled) driver.ConsoleEntry {
	args := make([]string, 0, len(e.Args))
	for _, arg := range e.Args {
		args = append(args, renderRemoteObject(arg))
	}

	entry := driver.ConsoleEntry{
		Type:  driver.ConsoleTypeConsole,
		Level: string(e.Type),
		Text:  strings.Join(args, " "),
	}

	if frames := stackFrames(e.StackTrace); len(frames) > 0 {
		entry.URL, entry.Line, entry.Column = frames[0].URL, frames[0].Line, frames[0].Column
	}

	return entry
}

// exceptionEntry maps an uncaught exception, worded as the DevTools console
// words it: "Uncaught Error: boom".
func exceptionEntry(e *runtime.EventExceptionThrown) driver.ConsoleEntry {
	entry := driver.ConsoleEntry{Type: driver.ConsoleTypeException, Level: "error"}

	details := e.ExceptionDetails
	if details == nil {
		return entry
	}

	entry.Text = details.Text

	if details.Exception != nil {
		if message := renderRemoteObject(details.Exception); message != "" {
			entry.Text = strings.TrimSpace(details.Text + " " + message)
		}
	}

	entry.Stack = stackFrames(details.StackTrace)
	entry.URL = details.URL
	entry.Line = int(details.LineNumber) + 1
	entry.Column = int(details.ColumnNumber) + 1

	if entry.URL == "" && len(entry.Stack) > 0 {
		entry.URL, entry.Line, entry.Column = entry.Stack[0].URL, entry.Stack[0].Line, entry.Stack[0].Column
	}

	return entry
}

// stackFrames converts a CDP stack (0-based positions) to 1-based frames.
// URLs are stripped by the recorder.
func stackFrames(trace *runtime.StackTrace) []driver.StackFrame {
	if trace == nil {
		return nil
	}

	frames := make([]driver.StackFrame, 0, len(trace.CallFrames))

	for _, frame := range trace.CallFrames {
		if frame == nil {
			continue
		}

		frames = append(frames, driver.StackFrame{
			Function: frame.FunctionName,
			URL:      frame.URL,
			Line:     int(frame.LineNumber) + 1,
			Column:   int(frame.ColumnNumber) + 1,
		})
	}

	return frames
}

// renderRemoteObject renders one console argument the way a reader expects
// it: a string as itself, a number or a boolean as its literal, an object
// as its preview or its description. An Error keeps its first line, the
// message: the rest is a stack whose URLs could carry query strings, and the
// entry's location already says where the call came from.
func renderRemoteObject(o *runtime.RemoteObject) string {
	if o == nil {
		return ""
	}

	if len(o.Value) > 0 {
		if o.Type == runtime.TypeString {
			var s string
			if err := json.Unmarshal(o.Value, &s); err == nil {
				return s
			}
		}

		return string(o.Value)
	}

	if o.UnserializableValue != "" {
		return string(o.UnserializableValue)
	}

	if o.Subtype == runtime.SubtypeError {
		message, _, _ := strings.Cut(o.Description, "\n")

		return message
	}

	if o.Preview != nil && o.Type == runtime.TypeObject && (o.Subtype == "" || o.Subtype == runtime.SubtypeArray) {
		return renderPreview(o.Preview)
	}

	if o.Description != "" {
		return o.Description
	}

	return string(o.Type)
}

// renderPreview renders the abbreviated properties Chrome sends with an
// object argument: {a: 1, b: "x"} or [1, 2, 3], with "..." when Chrome left
// properties out.
func renderPreview(p *runtime.ObjectPreview) string {
	isArray := p.Subtype == runtime.SubtypeArray
	parts := make([]string, 0, len(p.Properties)+1)

	for _, prop := range p.Properties {
		if prop == nil {
			continue
		}

		value := prop.Value
		if prop.Type == runtime.TypeString {
			value = strconv.Quote(value)
		}

		if isArray {
			parts = append(parts, value)
		} else {
			parts = append(parts, prop.Name+": "+value)
		}
	}

	if p.Overflow {
		parts = append(parts, "...")
	}

	if isArray {
		return "[" + strings.Join(parts, ", ") + "]"
	}

	return "{" + strings.Join(parts, ", ") + "}"
}

// requestEntries maps a request about to be sent. When it follows a
// redirect, the redirect's response comes first, so the file reads in the
// order things happened.
func requestEntries(e *network.EventRequestWillBeSent) []driver.NetworkEntry {
	at := monotonic(e.Timestamp)
	entries := make([]driver.NetworkEntry, 0, 2)

	if r := e.RedirectResponse; r != nil {
		entries = append(entries, driver.NetworkEntry{
			Type:        driver.NetworkTypeResponse,
			RequestID:   string(e.RequestID),
			URL:         r.URL,
			Status:      int(r.Status),
			StatusText:  r.StatusText,
			MimeType:    r.MimeType,
			FromCache:   fromCache(r),
			Redirect:    true,
			BrowserTime: at,
		})
	}

	entry := driver.NetworkEntry{
		Type:         driver.NetworkTypeRequest,
		RequestID:    string(e.RequestID),
		ResourceType: string(e.Type),
		BrowserTime:  at,
	}

	if e.Request != nil {
		entry.Method = e.Request.Method
		entry.URL = e.Request.URL
	}

	return append(entries, entry)
}

// responseEntry maps the response headers having arrived: status, MIME type,
// cache. The headers themselves are never read.
func responseEntry(e *network.EventResponseReceived) driver.NetworkEntry {
	entry := driver.NetworkEntry{
		Type:         driver.NetworkTypeResponse,
		RequestID:    string(e.RequestID),
		ResourceType: string(e.Type),
		BrowserTime:  monotonic(e.Timestamp),
	}

	if r := e.Response; r != nil {
		entry.URL = r.URL
		entry.Status = int(r.Status)
		entry.StatusText = r.StatusText
		entry.MimeType = r.MimeType
		entry.FromCache = fromCache(r)
	}

	return entry
}

func finishedEntry(e *network.EventLoadingFinished) driver.NetworkEntry {
	return driver.NetworkEntry{
		Type:         driver.NetworkTypeFinished,
		RequestID:    string(e.RequestID),
		EncodedBytes: int64(e.EncodedDataLength),
		BrowserTime:  monotonic(e.Timestamp),
	}
}

func failedEntry(e *network.EventLoadingFailed) driver.NetworkEntry {
	entry := driver.NetworkEntry{
		Type:          driver.NetworkTypeFailed,
		RequestID:     string(e.RequestID),
		ResourceType:  string(e.Type),
		ErrorText:     e.ErrorText,
		Canceled:      e.Canceled,
		BlockedReason: string(e.BlockedReason),
		BrowserTime:   monotonic(e.Timestamp),
	}

	if e.CorsErrorStatus != nil {
		entry.CORSError = string(e.CorsErrorStatus.CorsError)
	}

	return entry
}

func fromCache(r *network.Response) bool {
	return r.FromDiskCache || r.FromPrefetchCache || r.FromServiceWorker
}

// monotonic reads a CDP event timestamp, the zero time when it is absent.
func monotonic(t *cdp.MonotonicTime) time.Time {
	if t == nil {
		return time.Time{}
	}

	return t.Time()
}

// pageStateScript describes the page without reading any content: the
// focused element is named by its tag, id, data-testid, name, type and
// classes, never by its value or text.
const pageStateScript = `
(function(){
  var el = document.activeElement;
  while (el && el.shadowRoot && el.shadowRoot.activeElement) {
    el = el.shadowRoot.activeElement;
  }
  var active = null;
  if (el && el.tagName) {
    var attr = function (name) { return el.getAttribute(name) || ""; };
    active = {
      tag: el.tagName.toLowerCase(),
      id: el.id || "",
      data_testid: attr("data-testid"),
      name: attr("name"),
      type: attr("type"),
      classes: el.classList ? Array.prototype.slice.call(el.classList, 0, 10) : []
    };
  }
  var root = document.scrollingElement || document.documentElement;
  return {
    url: location.href,
    title: document.title,
    ready_state: document.readyState,
    active_element: active,
    viewport: { width: window.innerWidth, height: window.innerHeight, device_pixel_ratio: window.devicePixelRatio || 1 },
    scroll: { x: window.scrollX, y: window.scrollY },
    document: { width: root ? root.scrollWidth : 0, height: root ? root.scrollHeight : 0 }
  };
})()
`

// PageState implements driver.Driver.
func (d *chromedpDriver) PageState(ctx context.Context) (*driver.PageState, error) {
	var state driver.PageState

	if err := d.run(ctx, chromedp.Evaluate(pageStateScript, &state)); err != nil {
		return nil, fmt.Errorf("read page state: %w", err)
	}

	return sanitizePageState(state), nil
}

// sanitizePageState strips the document URL and names the focused element.
func sanitizePageState(state driver.PageState) *driver.PageState {
	state.URL = driver.StripURL(state.URL)

	if state.ActiveElement != nil {
		described := driver.DescribeElement(*state.ActiveElement)
		state.ActiveElement = &described
	}

	return &state
}
