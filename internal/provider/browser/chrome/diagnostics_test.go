package chrome

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

func monotonicAt(t time.Time) *cdp.MonotonicTime {
	m := cdp.MonotonicTime(t)

	return &m
}

func startedRecorder() *driver.Recorder {
	r := driver.NewRecorder(driver.WithRecorderClock(func() time.Time { return time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC) }))
	r.Start()

	return r
}

func TestRenderRemoteObject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   *runtime.RemoteObject
		want string
	}{
		{name: "nil", in: nil, want: ""},
		{name: "string unquoted", in: &runtime.RemoteObject{Type: runtime.TypeString, Value: []byte(`"ready \"now\""`)}, want: `ready "now"`},
		{name: "number literal", in: &runtime.RemoteObject{Type: runtime.TypeNumber, Value: []byte(`42.5`)}, want: "42.5"},
		{name: "boolean literal", in: &runtime.RemoteObject{Type: runtime.TypeBoolean, Value: []byte(`true`)}, want: "true"},
		{name: "null", in: &runtime.RemoteObject{Type: runtime.TypeObject, Subtype: runtime.SubtypeNull, Value: []byte(`null`)}, want: "null"},
		{name: "undefined", in: &runtime.RemoteObject{Type: runtime.TypeUndefined}, want: "undefined"},
		{name: "unserializable number", in: &runtime.RemoteObject{Type: runtime.TypeNumber, UnserializableValue: "NaN"}, want: "NaN"},
		{
			name: "error keeps its message line only",
			in: &runtime.RemoteObject{
				Type: runtime.TypeObject, Subtype: runtime.SubtypeError,
				Description: "Error: boom\n    at http://localhost:1337/app.js?token=s3cr3t:3:9",
			},
			want: "Error: boom",
		},
		{
			name: "object preview",
			in: &runtime.RemoteObject{Type: runtime.TypeObject, Description: "Object", Preview: &runtime.ObjectPreview{
				Type: runtime.TypeObject,
				Properties: []*runtime.PropertyPreview{
					{Name: "step", Type: runtime.TypeNumber, Value: "1"},
					{Name: "name", Type: runtime.TypeString, Value: "ping"},
				},
				Overflow: true,
			}},
			want: `{step: 1, name: "ping", ...}`,
		},
		{
			name: "array preview",
			in: &runtime.RemoteObject{Type: runtime.TypeObject, Subtype: runtime.SubtypeArray, Description: "Array(2)", Preview: &runtime.ObjectPreview{
				Type: runtime.TypeObject, Subtype: runtime.SubtypeArray,
				Properties: []*runtime.PropertyPreview{{Name: "0", Type: runtime.TypeNumber, Value: "1"}, {Name: "1", Type: runtime.TypeNumber, Value: "2"}},
			}},
			want: "[1, 2]",
		},
		{name: "node keeps its description", in: &runtime.RemoteObject{Type: runtime.TypeObject, Subtype: runtime.SubtypeNode, Description: "button#submit", Preview: &runtime.ObjectPreview{}}, want: "button#submit"},
		{name: "function description", in: &runtime.RemoteObject{Type: runtime.TypeFunction, Description: "function f() {}"}, want: "function f() {}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := renderRemoteObject(tt.in); got != tt.want {
				t.Errorf("renderRemoteObject() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRecordEventConsoleCall(t *testing.T) {
	t.Parallel()

	r := startedRecorder()

	recordEvent(r, &runtime.EventConsoleAPICalled{
		Type: runtime.APITypeWarning,
		Args: []*runtime.RemoteObject{
			{Type: runtime.TypeString, Value: []byte(`"slow response"`)},
			{Type: runtime.TypeNumber, Value: []byte(`1200`)},
		},
		StackTrace: &runtime.StackTrace{CallFrames: []*runtime.CallFrame{
			{FunctionName: "report", URL: "http://localhost:1337/web/diagnostics?token=s3cr3t", LineNumber: 11, ColumnNumber: 4},
			{FunctionName: "outer", URL: "http://localhost:1337/web/diagnostics", LineNumber: 20},
		}},
	})

	got := r.Snapshot().Console
	if len(got) != 1 {
		t.Fatalf("console entries = %d, want 1", len(got))
	}

	want := driver.ConsoleEntry{
		Time:   "2026-10-21T09:00:00.000Z",
		Type:   driver.ConsoleTypeConsole,
		Level:  "warning",
		Text:   "slow response 1200",
		URL:    "http://localhost:1337/web/diagnostics",
		Line:   12,
		Column: 5,
	}

	if e := got[0]; e.Time != want.Time || e.Type != want.Type || e.Level != want.Level || e.Text != want.Text || e.URL != want.URL || e.Line != want.Line || e.Column != want.Column || e.Stack != nil {
		t.Errorf("entry = %+v, want %+v", e, want)
	}
}

func TestRecordEventUncaughtException(t *testing.T) {
	t.Parallel()

	r := startedRecorder()

	recordEvent(r, &runtime.EventExceptionThrown{ExceptionDetails: &runtime.ExceptionDetails{
		Text:         "Uncaught",
		LineNumber:   2,
		ColumnNumber: 8,
		URL:          "http://localhost:1337/web/diagnostics?token=s3cr3t",
		Exception: &runtime.RemoteObject{
			Type: runtime.TypeObject, Subtype: runtime.SubtypeError,
			Description: "Error: diagnostics boom\n    at explode (http://localhost:1337/web/diagnostics?token=s3cr3t:3:9)",
		},
		StackTrace: &runtime.StackTrace{CallFrames: []*runtime.CallFrame{
			{FunctionName: "explode", URL: "http://localhost:1337/web/diagnostics?token=s3cr3t", LineNumber: 2, ColumnNumber: 8},
		}},
	}})
	// A thrown string has no description, only a value.
	recordEvent(r, &runtime.EventExceptionThrown{ExceptionDetails: &runtime.ExceptionDetails{
		Text:       "Uncaught",
		Exception:  &runtime.RemoteObject{Type: runtime.TypeString, Value: []byte(`"plain"`)},
		StackTrace: &runtime.StackTrace{CallFrames: []*runtime.CallFrame{{URL: "http://localhost:1337/x.js", LineNumber: 0, ColumnNumber: 0}}},
	}})

	got := r.Snapshot().Console
	if len(got) != 2 {
		t.Fatalf("console entries = %d, want 2", len(got))
	}

	boom := got[0]
	if boom.Type != driver.ConsoleTypeException || boom.Level != "error" || boom.Text != "Uncaught Error: diagnostics boom" {
		t.Errorf("exception entry = %+v", boom)
	}

	if boom.URL != "http://localhost:1337/web/diagnostics" || boom.Line != 3 || boom.Column != 9 {
		t.Errorf("exception location = %s:%d:%d", boom.URL, boom.Line, boom.Column)
	}

	if len(boom.Stack) != 1 || boom.Stack[0].Function != "explode" || strings.Contains(boom.Stack[0].URL, "?") {
		t.Errorf("exception stack = %+v", boom.Stack)
	}

	plain := got[1]
	if plain.Text != "Uncaught plain" || plain.URL != "http://localhost:1337/x.js" || plain.Line != 1 || plain.Column != 1 {
		t.Errorf("thrown string entry = %+v, want its location from the stack", plain)
	}
}

func TestRecordEventNetworkLifecycle(t *testing.T) {
	t.Parallel()

	r := startedRecorder()
	sent := time.Unix(5000, 0)

	recordEvent(r, &network.EventRequestWillBeSent{
		RequestID: "42",
		Request: &network.Request{
			URL:         "http://localhost:1337/web/diagnostics/ping?token=s3cr3t-query",
			Method:      "GET",
			Headers:     network.Headers{"Authorization": "Bearer s3cr3t-header"},
			HasPostData: false,
		},
		Type:      network.ResourceTypeFetch,
		Timestamp: monotonicAt(sent),
	})
	recordEvent(r, &network.EventResponseReceived{
		RequestID: "42",
		Type:      network.ResourceTypeFetch,
		Timestamp: monotonicAt(sent.Add(8 * time.Millisecond)),
		Response: &network.Response{
			URL:        "http://localhost:1337/web/diagnostics/ping?token=s3cr3t-query",
			Status:     200,
			StatusText: "OK",
			MimeType:   "application/json",
			Headers:    network.Headers{"Set-Cookie": "session=s3cr3t-cookie"},
		},
	})
	recordEvent(r, &network.EventLoadingFinished{RequestID: "42", EncodedDataLength: 321, Timestamp: monotonicAt(sent.Add(9 * time.Millisecond))})
	recordEvent(r, &network.EventRequestWillBeSent{
		RequestID: "43",
		Request:   &network.Request{URL: "http://localhost:1337/missing", Method: "POST"},
		Type:      network.ResourceTypeXHR,
		Timestamp: monotonicAt(sent),
	})
	recordEvent(r, &network.EventLoadingFailed{
		RequestID:       "43",
		Type:            network.ResourceTypeXHR,
		ErrorText:       "net::ERR_FAILED",
		Canceled:        true,
		BlockedReason:   network.BlockedReasonInspector,
		CorsErrorStatus: &network.CorsErrorStatus{CorsError: network.CorsErrorMissingAllowOriginHeader},
		Timestamp:       monotonicAt(sent.Add(3 * time.Millisecond)),
	})

	got := r.Snapshot().Network
	if len(got) != 5 {
		t.Fatalf("network entries = %d, want 5", len(got))
	}

	for i, e := range got {
		dump := strings.Join([]string{e.URL, e.Method, e.StatusText, e.MimeType, e.ErrorText}, " ")
		if strings.Contains(dump, "s3cr3t") {
			t.Errorf("entry %d leaked a secret: %+v", i, e)
		}
	}

	request, response, finished, failedRequest, failed := got[0], got[1], got[2], got[3], got[4]

	if request.Type != driver.NetworkTypeRequest || request.Method != "GET" || request.URL != "http://localhost:1337/web/diagnostics/ping" || request.ResourceType != "Fetch" {
		t.Errorf("request entry = %+v", request)
	}

	if response.Status != 200 || response.StatusText != "OK" || response.MimeType != "application/json" || response.Method != "GET" || response.ElapsedMS == nil || *response.ElapsedMS != 8 {
		t.Errorf("response entry = %+v", response)
	}

	if finished.EncodedBytes != 321 || finished.URL != request.URL || finished.ElapsedMS == nil || *finished.ElapsedMS != 9 {
		t.Errorf("finished entry = %+v", finished)
	}

	if failedRequest.Method != "POST" {
		t.Errorf("second request entry = %+v", failedRequest)
	}

	if failed.ErrorText != "net::ERR_FAILED" || !failed.Canceled || failed.BlockedReason != "inspector" || failed.CORSError != "MissingAllowOriginHeader" || failed.Method != "POST" || failed.URL != "http://localhost:1337/missing" {
		t.Errorf("failed entry = %+v", failed)
	}
}

func TestRecordEventRedirect(t *testing.T) {
	t.Parallel()

	r := startedRecorder()
	sent := time.Unix(5000, 0)

	recordEvent(r, &network.EventRequestWillBeSent{
		RequestID: "9",
		Request:   &network.Request{URL: "http://localhost:1337/web/bounce?to=s3cr3t", Method: "GET"},
		Type:      network.ResourceTypeDocument,
		Timestamp: monotonicAt(sent),
	})
	recordEvent(r, &network.EventRequestWillBeSent{
		RequestID:        "9",
		Request:          &network.Request{URL: "http://localhost:1337/web/login", Method: "GET"},
		RedirectResponse: &network.Response{URL: "http://localhost:1337/web/bounce?to=s3cr3t", Status: 302, StatusText: "Found"},
		Type:             network.ResourceTypeDocument,
		Timestamp:        monotonicAt(sent.Add(4 * time.Millisecond)),
	})

	got := r.Snapshot().Network
	if len(got) != 3 {
		t.Fatalf("network entries = %d, want request, redirect response, request", len(got))
	}

	redirect := got[1]
	if redirect.Type != driver.NetworkTypeResponse || !redirect.Redirect || redirect.Status != 302 || redirect.URL != "http://localhost:1337/web/bounce" || redirect.ElapsedMS == nil || *redirect.ElapsedMS != 4 {
		t.Errorf("redirect entry = %+v", redirect)
	}

	if next := got[2]; next.Type != driver.NetworkTypeRequest || next.URL != "http://localhost:1337/web/login" {
		t.Errorf("redirected request entry = %+v", next)
	}
}

func TestRecordEventIgnoresOtherEvents(t *testing.T) {
	t.Parallel()

	r := startedRecorder()

	recordEvent(r, &page.EventFrameNavigated{Frame: &cdp.Frame{ID: "main"}})
	recordEvent(r, &network.EventDataReceived{RequestID: "1"})
	recordEvent(r, &runtime.EventExceptionThrown{})

	got := r.Snapshot()
	if len(got.Network) != 0 {
		t.Errorf("unrelated events recorded: %+v", got.Network)
	}

	// An exception event without details still records that one happened.
	if len(got.Console) != 1 || got.Console[0].Type != driver.ConsoleTypeException {
		t.Errorf("console = %+v", got.Console)
	}
}

func TestSanitizePageState(t *testing.T) {
	t.Parallel()

	got := sanitizePageState(driver.PageState{
		URL:           "http://localhost:1337/web/diagnostics?token=s3cr3t#done",
		ActiveElement: &driver.ElementDescription{Tag: "input", TestID: "diagnostics.query", Name: "q"},
	})

	if got.URL != "http://localhost:1337/web/diagnostics" {
		t.Errorf("URL = %q, want the query and fragment stripped", got.URL)
	}

	if got.ActiveElement == nil || got.ActiveElement.Selector != `input[data-testid="diagnostics.query"][name="q"]` {
		t.Errorf("active element = %+v", got.ActiveElement)
	}

	if none := sanitizePageState(driver.PageState{}); none.ActiveElement != nil {
		t.Errorf("no focus became %+v", none.ActiveElement)
	}
}
