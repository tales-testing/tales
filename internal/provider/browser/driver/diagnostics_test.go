package driver

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

var recorderEpoch = time.Date(2026, 10, 21, 9, 54, 34, 0, time.UTC)

// fixedClock returns a clock that advances by one millisecond per call, so
// entries get distinct, predictable times.
func fixedClock() func() time.Time {
	var (
		mu sync.Mutex
		n  int
	)

	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()

		n++

		return recorderEpoch.Add(time.Duration(n) * time.Millisecond)
	}
}

func TestStripURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "plain URL unchanged", in: "http://localhost:1337/web/login", want: "http://localhost:1337/web/login"},
		{name: "query dropped", in: "http://localhost:1337/api/ping?token=s3cr3t&x=1", want: "http://localhost:1337/api/ping"},
		{name: "fragment dropped", in: "https://example.com/app#/settings?tab=keys", want: "https://example.com/app"},
		{name: "empty query marker dropped", in: "https://example.com/search?", want: "https://example.com/search"},
		{name: "credentials dropped", in: "https://alice:hunter2@example.com/private?k=v", want: "https://example.com/private"},
		{name: "data URL keeps its media type only", in: "data:text/plain;base64,c2VjcmV0", want: "data:text/plain;base64"},
		{name: "data URL scheme in capitals", in: "DATA:image/png,abc", want: "DATA:image/png"},
		{name: "blob URL cut at the query", in: "blob:http://localhost:1337/0b1c?x=1", want: "blob:http://localhost:1337/0b1c"},
		{name: "unparsable URL cut at the query", in: "http://exa mple.com/p?token=s3cr3t", want: "http://exa mple.com/p"},
		{name: "unparsable URL loses its credentials", in: "http://bob:pa ss@host/p?q=1", want: "http://host/p"},
		{name: "relative path", in: "/web/ping?token=s3cr3t", want: "/web/ping"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := StripURL(tt.in); got != tt.want {
				t.Errorf("StripURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDescribeElement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   ElementDescription
		want string
	}{
		{name: "tag only", in: ElementDescription{Tag: "body"}, want: "body"},
		{
			name: "every field",
			in:   ElementDescription{Tag: "input", ID: "email", TestID: "login.email", Name: "email", Type: "email", Classes: []string{"wide", "primary"}},
			want: `input#email.wide.primary[data-testid="login.email"][name="email"][type="email"]`,
		},
		{name: "id needing escapes becomes an attribute", in: ElementDescription{Tag: "div", ID: "1st:item"}, want: `div[id="1st:item"]`},
		{name: "class needing escapes is left out", in: ElementDescription{Tag: "div", Classes: []string{"md:flex", "card"}}, want: "div.card"},
		{name: "quotes escaped", in: ElementDescription{Tag: "button", TestID: `say "hi"`}, want: `button[data-testid="say \"hi\""]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := DescribeElement(tt.in)
			if got.Selector != tt.want {
				t.Errorf("Selector = %q, want %q", got.Selector, tt.want)
			}

			if got.Tag != tt.in.Tag || got.ID != tt.in.ID {
				t.Errorf("DescribeElement changed the described fields: %+v", got)
			}
		})
	}
}

func TestRecorderRecordsNothingBeforeStart(t *testing.T) {
	t.Parallel()

	r := NewRecorder(WithRecorderClock(fixedClock()))
	r.RecordConsole(ConsoleEntry{Type: ConsoleTypeConsole, Level: "log", Text: "early"})
	r.RecordNetwork(NetworkEntry{Type: NetworkTypeRequest, RequestID: "1", Method: "GET", URL: "http://h/p"})

	if got := r.Snapshot(); got != nil {
		t.Fatalf("Snapshot before Start = %+v, want nil", got)
	}

	r.Start()

	got := r.Snapshot()
	if got == nil || len(got.Console) != 0 || len(got.Network) != 0 {
		t.Fatalf("Snapshot after Start = %+v, want an empty recording", got)
	}
}

func TestRecorderConsoleSanitizesAndStamps(t *testing.T) {
	t.Parallel()

	r := NewRecorder(WithRecorderClock(fixedClock()))
	r.Start()

	long := strings.Repeat("é", MaxTextBytes) // two bytes per rune

	r.RecordConsole(ConsoleEntry{
		Type:  ConsoleTypeException,
		Level: "error",
		Text:  long,
		URL:   "http://localhost:1337/app.js?v=s3cr3t",
		Line:  3,
		Stack: []StackFrame{{Function: "boom", URL: "http://localhost:1337/app.js?v=s3cr3t#x", Line: 3, Column: 9}},
	})
	r.RecordConsole(ConsoleEntry{Time: "2000-01-01T00:00:00.000Z", Type: ConsoleTypeConsole, Level: "log", Text: "kept time"})

	got := r.Snapshot()
	if len(got.Console) != 2 {
		t.Fatalf("console entries = %d, want 2", len(got.Console))
	}

	first := got.Console[0]
	if first.Time != "2026-10-21T09:54:34.001Z" {
		t.Errorf("Time = %q, want the recorder clock", first.Time)
	}

	if len(first.Text) > MaxTextBytes || !strings.HasPrefix(long, first.Text) {
		t.Errorf("Text not cut to %d bytes on a rune boundary: %d bytes", MaxTextBytes, len(first.Text))
	}

	if !first.Truncated {
		t.Error("Truncated = false for a cut text")
	}

	if first.URL != "http://localhost:1337/app.js" || first.Stack[0].URL != "http://localhost:1337/app.js" {
		t.Errorf("URLs not stripped: url=%q stack=%q", first.URL, first.Stack[0].URL)
	}

	if got.Console[1].Time != "2000-01-01T00:00:00.000Z" || got.Console[1].Truncated {
		t.Errorf("second entry altered: %+v", got.Console[1])
	}
}

func TestRecorderConsoleCapsStack(t *testing.T) {
	t.Parallel()

	r := NewRecorder(WithRecorderClock(fixedClock()))
	r.Start()

	frames := make([]StackFrame, MaxStackFrames+5)
	r.RecordConsole(ConsoleEntry{Type: ConsoleTypeException, Stack: frames})

	got := r.Snapshot().Console[0]
	if len(got.Stack) != MaxStackFrames || !got.Truncated {
		t.Errorf("stack = %d frames, truncated=%v; want %d frames, truncated", len(got.Stack), got.Truncated, MaxStackFrames)
	}
}

func TestRecorderNetworkCompletesFromRequest(t *testing.T) {
	t.Parallel()

	r := NewRecorder(WithRecorderClock(fixedClock()))
	r.Start()

	sent := time.Unix(1000, 0)

	r.RecordNetwork(NetworkEntry{Type: NetworkTypeRequest, RequestID: "7", Method: "POST", URL: "http://localhost:1337/api/login?token=s3cr3t", ResourceType: "Fetch", BrowserTime: sent})
	r.RecordNetwork(NetworkEntry{Type: NetworkTypeResponse, RequestID: "7", URL: "http://localhost:1337/api/login?token=s3cr3t", Status: 401, BrowserTime: sent.Add(12500 * time.Microsecond)})
	r.RecordNetwork(NetworkEntry{Type: NetworkTypeFinished, RequestID: "7", EncodedBytes: 120, BrowserTime: sent.Add(20 * time.Millisecond)})

	got := r.Snapshot().Network
	if len(got) != 3 {
		t.Fatalf("network entries = %d, want 3", len(got))
	}

	for i, e := range got {
		if strings.Contains(e.URL, "s3cr3t") || strings.Contains(e.URL, "?") {
			t.Errorf("entry %d kept the query string: %q", i, e.URL)
		}
	}

	if got[0].ElapsedMS != nil {
		t.Errorf("request entry has elapsed_ms %v, want none", *got[0].ElapsedMS)
	}

	if got[1].Method != "POST" || got[1].ResourceType != "Fetch" || got[1].ElapsedMS == nil || *got[1].ElapsedMS != 12.5 {
		t.Errorf("response not completed from its request: %+v", got[1])
	}

	finished := got[2]
	if finished.Method != "POST" || finished.URL != "http://localhost:1337/api/login" || finished.ElapsedMS == nil || *finished.ElapsedMS != 20 {
		t.Errorf("finished not completed from its request: %+v (elapsed %v)", finished, finished.ElapsedMS)
	}

	// The request is forgotten once finished: a late event for it is
	// recorded as is.
	r.RecordNetwork(NetworkEntry{Type: NetworkTypeFailed, RequestID: "7"})

	if late := r.Snapshot().Network[3]; late.Method != "" || late.ElapsedMS != nil {
		t.Errorf("event after finished completed from a forgotten request: %+v", late)
	}
}

func TestRecorderKeepsInFlightRequestsAcrossStart(t *testing.T) {
	t.Parallel()

	r := NewRecorder(WithRecorderClock(fixedClock()))
	r.Start()
	r.RecordNetwork(NetworkEntry{Type: NetworkTypeRequest, RequestID: "1", Method: "GET", URL: "http://h/slow"})

	r.Start()
	r.RecordNetwork(NetworkEntry{Type: NetworkTypeResponse, RequestID: "1", Status: 200})

	got := r.Snapshot().Network
	if len(got) != 1 {
		t.Fatalf("Start kept %d old entries, want only the response", len(got))
	}

	if got[0].Method != "GET" || got[0].URL != "http://h/slow" {
		t.Errorf("response of a request sent before Start not completed: %+v", got[0])
	}
}

func TestRecorderKeepsTheMostRecentEntries(t *testing.T) {
	t.Parallel()

	r := NewRecorder(WithRecorderClock(fixedClock()), WithRecorderLimits(3, 2))
	r.Start()

	for i := range 5 {
		r.RecordConsole(ConsoleEntry{Text: fmt.Sprintf("m%d", i)})
		r.RecordNetwork(NetworkEntry{Type: NetworkTypeRequest, RequestID: fmt.Sprint(i)})
	}

	got := r.Snapshot()

	var texts []string
	for _, e := range got.Console {
		texts = append(texts, e.Text)
	}

	if strings.Join(texts, ",") != "m2,m3,m4" || got.ConsoleDropped != 2 {
		t.Errorf("console = %v dropped=%d, want m2,m3,m4 dropped=2", texts, got.ConsoleDropped)
	}

	if len(got.Network) != 2 || got.Network[0].RequestID != "3" || got.Network[1].RequestID != "4" || got.NetworkDropped != 3 {
		t.Errorf("network = %+v dropped=%d, want requests 3,4 dropped=3", got.Network, got.NetworkDropped)
	}

	r.Start()

	if again := r.Snapshot(); len(again.Console) != 0 || again.ConsoleDropped != 0 || again.NetworkDropped != 0 {
		t.Errorf("Start did not reset the buffers: %+v", again)
	}
}

func TestRecorderBoundsPendingRequests(t *testing.T) {
	t.Parallel()

	r := NewRecorder(WithRecorderClock(fixedClock()))
	r.Start()

	for i := range maxPendingRequests + 10 {
		r.RecordNetwork(NetworkEntry{Type: NetworkTypeRequest, RequestID: fmt.Sprint(i), Method: "GET"})
	}

	r.mu.Lock()
	pending := len(r.pending)
	r.mu.Unlock()

	if pending != maxPendingRequests {
		t.Errorf("pending requests = %d, want the cap %d", pending, maxPendingRequests)
	}
}

// TestRecorderConcurrentUse records from several goroutines while another
// snapshots and restarts, the way chromedp's event loop and the provider
// share a recorder. Run under -race.
func TestRecorderConcurrentUse(t *testing.T) {
	t.Parallel()

	r := NewRecorder(WithRecorderLimits(50, 50))
	r.Start()

	var wg sync.WaitGroup

	for w := range 4 {
		wg.Go(func() {
			for i := range 200 {
				id := fmt.Sprintf("%d-%d", w, i)
				r.RecordConsole(ConsoleEntry{Type: ConsoleTypeConsole, Text: id})
				r.RecordNetwork(NetworkEntry{Type: NetworkTypeRequest, RequestID: id, URL: "http://h/p?x=1"})
				r.RecordNetwork(NetworkEntry{Type: NetworkTypeFinished, RequestID: id})
			}
		})
	}

	wg.Go(func() {
		for i := range 100 {
			snap := r.Snapshot()
			if snap == nil {
				t.Error("Snapshot returned nil while recording")

				return
			}

			if i%10 == 0 {
				r.Start()
			}
		}
	})

	wg.Wait()

	snap := r.Snapshot()
	if len(snap.Console) > 50 || len(snap.Network) > 50 {
		t.Errorf("bounds exceeded: console=%d network=%d", len(snap.Console), len(snap.Network))
	}
}
