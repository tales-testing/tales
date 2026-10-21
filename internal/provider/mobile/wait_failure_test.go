package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/mobile/driver"
	"github.com/tales-testing/tales/internal/provider/mobile/tree"
	"github.com/zclconf/go-cty/cty"
)

// The tests in this file cover issue #73: a wait that ran out of time used
// to report the hierarchy fetch its own deadline had cancelled, which reads
// as a driver that stopped answering when the element simply never
// appeared. Only a request that failed on its own is a driver problem.

// lastFetchNote matches the note a wait failure carries when the driver
// did answer during the wait.
var lastFetchNote = regexp.MustCompile(`\(last hierarchy fetched \d+\.\ds ago, \d+ elements?\)`)

// stalledHierarchyDriver answers the first `answers` hierarchy fetches at
// once, then holds every later one until its context is done, the way a
// fetch still in flight when the wait's deadline expires looks from the
// provider's side.
type stalledHierarchyDriver struct {
	driver.NoopDriver

	screen  *tree.ViewNode
	answers int32
	calls   atomic.Int32

	// scrollAnswers is the same budget for /scrollTo: that many dispatches
	// answer the drivers' own "not reachable yet" 404, later ones stall.
	scrollAnswers int32
	scrollCalls   atomic.Int32
}

func (d *stalledHierarchyDriver) Hierarchy(ctx context.Context, bundleID string) (*tree.ViewNode, error) {
	if d.calls.Add(1) <= d.answers {
		return d.screen, nil
	}

	<-ctx.Done()

	return nil, fmt.Errorf("call GET /hierarchy?bundleId=%s: %w", bundleID, ctx.Err())
}

func (d *stalledHierarchyDriver) ScrollTo(ctx context.Context, _ string, _ driver.Locator) error {
	if d.scrollCalls.Add(1) <= d.scrollAnswers {
		return errors.New("driver /scrollTo returned 404: element not found")
	}

	<-ctx.Done()

	return fmt.Errorf("call POST /scrollTo: %w", ctx.Err())
}

func waitSession(drv driver.Driver) *Session {
	return &Session{Target: sampleProviderTarget(), Driver: drv}
}

// assertNoCancelledFetch fails when msg blames the request the wait's own
// deadline cancelled.
func assertNoCancelledFetch(t *testing.T, msg string) {
	t.Helper()

	for _, blamed := range []string{"context deadline exceeded", "fetch hierarchy", "poll timed out"} {
		if strings.Contains(msg, blamed) {
			t.Fatalf("the failure blames the cancelled poll (%q): %s", blamed, msg)
		}
	}
}

// TestWaitTimeoutDoesNotReportTheFetchItsDeadlineCancelled reproduces the
// issue end to end with the real HTTP client: the driver answers, the
// element is not there, and the deadline lands while a fetch is in flight.
func TestWaitTimeoutDoesNotReportTheFetchItsDeadlineCancelled(t *testing.T) {
	t.Parallel()

	screen, err := json.Marshal(&tree.ViewNode{ID: "root", Visible: true, Children: []*tree.ViewNode{{ID: "welcome.title", Visible: true}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var calls atomic.Int32

	// The second fetch is the one in flight when the wait runs out. It
	// answers once the client has gone, like a healthy driver whose
	// response simply arrived too late. Every other fetch, the artifact
	// capture's included, answers at once.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 2 {
			<-r.Context().Done()

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(screen)
	}))
	t.Cleanup(srv.Close)

	builder := SessionBuilderFunc(func(_ context.Context, target Target) (*Session, error) {
		return &Session{Target: target, Driver: driver.New(srv.URL), Lifecycle: &fakeLifecycle{}}, nil
	})
	p := New(WithSessionBuilder(builder), WithArtifactsBase(t.TempDir()))

	_, err = p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     newStep("wait"),
		Config:   sampleConfigCty(),
		Mobile: &provider.MobileExecution{
			Platform:   "ios",
			TargetName: "iphone",
			Actions: []provider.MobileActionExec{
				{Kind: model.MobileActionWaitVisible, ID: "main.tab_bar", Timeout: 200 * time.Millisecond, Interval: 10 * time.Millisecond},
			},
		},
	})
	if err == nil {
		t.Fatal("expected the wait to fail")
	}

	msg := err.Error()

	if !strings.Contains(msg, `element id="main.tab_bar" not found after 200ms`) {
		t.Fatalf("expected the wait failure, got: %s", msg)
	}

	if !lastFetchNote.MatchString(msg) || !strings.Contains(msg, "2 elements") {
		t.Fatalf("expected when the last hierarchy came back and its size, got: %s", msg)
	}

	assertNoCancelledFetch(t, msg)
}

// TestWaitTimeoutBetweenPollsReportsTheMissingElement is the other half of
// the issue: the same cause used to read "poll timed out: context deadline
// exceeded" whenever the deadline fell between two fetches.
func TestWaitTimeoutBetweenPollsReportsTheMissingElement(t *testing.T) {
	t.Parallel()

	drv := &fakeDriverAll{hierarchies: []*tree.ViewNode{{ID: "root", Visible: true}}}
	p := New()

	err := p.handleAction(context.Background(), waitSession(drv), provider.MobileActionExec{
		Kind: model.MobileActionWaitVisible, ID: "main.tab_bar", Timeout: 50 * time.Millisecond, Interval: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected the wait to fail")
	}

	msg := err.Error()

	if !strings.HasPrefix(msg, `element id="main.tab_bar" not found after 50ms (last hierarchy fetched `) {
		t.Fatalf("expected the wait failure with its last fetch, got: %s", msg)
	}

	if !strings.Contains(msg, "1 element)") {
		t.Fatalf("expected the size of the last hierarchy, got: %s", msg)
	}

	assertNoCancelledFetch(t, msg)
}

// TestWaitTimeoutCoversEveryPollingPath pins the rule on every wait that
// shares the poll: element waits before actions, the wait_* actions and
// the expectations.
func TestWaitTimeoutCoversEveryPollingPath(t *testing.T) {
	t.Parallel()

	visible := newButtonNode()
	empty := &tree.ViewNode{ID: "root", Visible: true}

	cases := map[string]struct {
		screen *tree.ViewNode
		run    func(p *Provider, s *Session) error
		want   string
	}{
		"tap waits for its element": {
			screen: empty,
			run: func(p *Provider, s *Session) error {
				return p.handleAction(context.Background(), s, provider.MobileActionExec{Kind: model.MobileActionTap, ID: "welcome.register", Timeout: 80 * time.Millisecond, Interval: 5 * time.Millisecond})
			},
			want: `element id="welcome.register" was not visible after 80ms (last hierarchy fetched `,
		},
		"wait_visible": {
			screen: empty,
			run: func(p *Provider, s *Session) error {
				return p.handleAction(context.Background(), s, provider.MobileActionExec{Kind: model.MobileActionWaitVisible, ID: "welcome.register", Timeout: 80 * time.Millisecond, Interval: 5 * time.Millisecond})
			},
			want: `element id="welcome.register" not found after 80ms (last hierarchy fetched `,
		},
		"wait_not_visible": {
			screen: visible,
			run: func(p *Provider, s *Session) error {
				return p.handleAction(context.Background(), s, provider.MobileActionExec{Kind: model.MobileActionWaitNotVisible, ID: "welcome.register", Timeout: 80 * time.Millisecond, Interval: 5 * time.Millisecond})
			},
			want: `element id="welcome.register" was still visible after 80ms (last hierarchy fetched `,
		},
		"wait_enabled": {
			screen: empty,
			run: func(p *Provider, s *Session) error {
				return p.handleAction(context.Background(), s, provider.MobileActionExec{Kind: model.MobileActionWaitEnabled, ID: "welcome.register", Timeout: 80 * time.Millisecond, Interval: 5 * time.Millisecond})
			},
			want: `element id="welcome.register" not found after 80ms (last hierarchy fetched `,
		},
		"expect visible": {
			screen: empty,
			run: func(p *Provider, s *Session) error {
				return p.handleExpect(context.Background(), s, provider.MobileExpectExec{Visible: []provider.MobileVisibilityExec{{ID: "welcome.register", Timeout: 80 * time.Millisecond, Interval: 5 * time.Millisecond}}})
			},
			want: `element id="welcome.register" not found after 80ms (last hierarchy fetched `,
		},
		"expect text": {
			screen: empty,
			run: func(p *Provider, s *Session) error {
				return p.handleExpect(context.Background(), s, provider.MobileExpectExec{Text: []provider.MobileValueExpectationExec{{ID: "welcome.register", Expected: cty.StringVal("Welcome"), Timeout: 80 * time.Millisecond, Interval: 5 * time.Millisecond}}})
			},
			want: `element id="welcome.register" not found after 80ms (last hierarchy fetched `,
		},
		"expect disabled": {
			screen: empty,
			run: func(p *Provider, s *Session) error {
				return p.handleExpect(context.Background(), s, provider.MobileExpectExec{Disabled: []provider.MobileStateExpectationExec{{ID: "welcome.register", Timeout: 80 * time.Millisecond, Interval: 5 * time.Millisecond}}})
			},
			want: `element id="welcome.register" not found after 80ms (last hierarchy fetched `,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// One answer, then a fetch the deadline cuts off mid-flight.
			drv := &stalledHierarchyDriver{screen: tc.screen, answers: 1}

			err := tc.run(New(), waitSession(drv))
			if err == nil {
				t.Fatal("expected the wait to fail")
			}

			msg := err.Error()

			if !strings.Contains(msg, tc.want) {
				t.Fatalf("expected %q, got: %s", tc.want, msg)
			}

			assertNoCancelledFetch(t, msg)
		})
	}
}

// TestWaitTimeoutKeepsTheErrorOfAFetchThatFailedOnItsOwn checks the other
// side of the rule: a driver that refuses connections is the story, and
// the failure says nothing ever came back.
func TestWaitTimeoutKeepsTheErrorOfAFetchThatFailedOnItsOwn(t *testing.T) {
	t.Parallel()

	refused := errors.New(`call GET /hierarchy?bundleId=com.example.MyApp: Get "http://127.0.0.1:9080/hierarchy": dial tcp 127.0.0.1:9080: connect: connection refused`)
	drv := &fakeDriverAll{hierarchyErr: refused}

	err := New().handleAction(context.Background(), waitSession(drv), provider.MobileActionExec{
		Kind: model.MobileActionWaitVisible, ID: "main.tab_bar", Timeout: 50 * time.Millisecond, Interval: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected the wait to fail")
	}

	msg := err.Error()

	want := `element id="main.tab_bar" not found after 50ms (no hierarchy fetched during the wait): fetch hierarchy: ` + refused.Error()
	if msg != want {
		t.Fatalf("unexpected failure\nwant: %s\ngot:  %s", want, msg)
	}

	if !looksLikeDriverDeath(err) {
		t.Fatalf("a refused connection must still read as a dead driver: %s", msg)
	}
}

// TestWaitTimeoutKeepsAPerRequestDriverTimeout guards the trap the issue
// warns about: driver.timeout also surfaces as context.DeadlineExceeded,
// and it is a driver problem whenever the wait still had time left.
func TestWaitTimeoutKeepsAPerRequestDriverTimeout(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	drv := driver.New(srv.URL, driver.WithTimeout(20*time.Millisecond))

	err := New().handleAction(context.Background(), waitSession(drv), provider.MobileActionExec{
		Kind: model.MobileActionWaitVisible, ID: "main.tab_bar", Timeout: 300 * time.Millisecond, Interval: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected the wait to fail")
	}

	msg := err.Error()

	for _, want := range []string{
		`element id="main.tab_bar" not found after 300ms (no hierarchy fetched during the wait): fetch hierarchy: `,
		"Client.Timeout exceeded",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected %q, got: %s", want, msg)
		}
	}
}

// TestWaitTimeoutForgetsAnErrorASuccessfulFetchSuperseded covers a
// transient refusal early in the wait: once the driver answered again,
// the refusal no longer explains anything.
func TestWaitTimeoutForgetsAnErrorASuccessfulFetchSuperseded(t *testing.T) {
	t.Parallel()

	drv := &flakyHierarchyDriver{refusals: 1}

	err := New().handleAction(context.Background(), waitSession(drv), provider.MobileActionExec{
		Kind: model.MobileActionWaitVisible, ID: "main.tab_bar", Timeout: 50 * time.Millisecond, Interval: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected the wait to fail")
	}

	msg := err.Error()

	if strings.Contains(msg, "503") {
		t.Fatalf("a refusal later fetches superseded must not explain the failure: %s", msg)
	}

	if !lastFetchNote.MatchString(msg) {
		t.Fatalf("expected the last fetch in the failure, got: %s", msg)
	}
}

// TestWaitWithoutAnyAnswerKeepsTheCancelledFetch: when not a single fetch
// came back during the wait, the one the deadline cut off is the only
// lead, and is exactly what the failure must show.
func TestWaitWithoutAnyAnswerKeepsTheCancelledFetch(t *testing.T) {
	t.Parallel()

	drv := &stalledHierarchyDriver{screen: newButtonNode()}

	err := New().handleAction(context.Background(), waitSession(drv), provider.MobileActionExec{
		Kind: model.MobileActionWaitVisible, ID: "main.tab_bar", Timeout: 50 * time.Millisecond, Interval: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected the wait to fail")
	}

	want := `element id="main.tab_bar" not found after 50ms (no hierarchy fetched during the wait): fetch hierarchy: call GET /hierarchy?bundleId=com.example.MyApp: context deadline exceeded`
	if got := err.Error(); got != want {
		t.Fatalf("unexpected failure\nwant: %s\ngot:  %s", want, got)
	}
}

// TestWaitInterruptedByTheRunSaysSo keeps a cancelled run (suite
// --timeout) distinguishable from a wait that ran its course.
func TestWaitInterruptedByTheRunSaysSo(t *testing.T) {
	t.Parallel()

	drv := &stalledHierarchyDriver{screen: &tree.ViewNode{ID: "root", Visible: true}, answers: 1}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	err := New().handleAction(ctx, waitSession(drv), provider.MobileActionExec{
		Kind: model.MobileActionWaitVisible, ID: "main.tab_bar", Timeout: 10 * time.Second, Interval: 5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected the wait to fail")
	}

	msg := err.Error()

	if strings.Contains(msg, "after 10s") {
		t.Fatalf("the wait did not run its 10s course and must not say so: %s", msg)
	}

	for _, want := range []string{
		`element id="main.tab_bar" not found after `,
		"wait interrupted before its 10s timeout: context deadline exceeded",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected %q, got: %s", want, msg)
		}
	}

	if !lastFetchNote.MatchString(msg) {
		t.Fatalf("expected the last fetch in the failure, got: %s", msg)
	}
}

// TestScrollToTimeoutKeepsTheDriversOwnReason: scroll_to shares the poll,
// and its deadline used to replace the driver's "not found" with the
// dispatch it cancelled.
func TestScrollToTimeoutKeepsTheDriversOwnReason(t *testing.T) {
	t.Parallel()

	drv := &stalledHierarchyDriver{screen: newButtonNode(), scrollAnswers: 1}

	err := New().handleAction(context.Background(), waitSession(drv), provider.MobileActionExec{
		Kind: model.MobileActionScrollTo, ID: "never.appears", Timeout: 50 * time.Millisecond, Interval: 5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected scroll_to to fail")
	}

	want := `id="never.appears": gave up after 50ms: scroll to: driver /scrollTo returned 404: element not found`
	if got := err.Error(); got != want {
		t.Fatalf("unexpected failure\nwant: %s\ngot:  %s", want, got)
	}
}

// TestWaitTimeoutLeavesMismatchesAlone: a wait that found its element and
// judged it keeps the matcher's own message.
func TestWaitTimeoutLeavesMismatchesAlone(t *testing.T) {
	t.Parallel()

	node := newButtonNode()
	node.Children[0].Text = "Bienvenue"

	drv := &stalledHierarchyDriver{screen: node, answers: 1}

	err := New().handleExpect(context.Background(), waitSession(drv), provider.MobileExpectExec{
		Text: []provider.MobileValueExpectationExec{{ID: "welcome.register", Expected: cty.StringVal("Welcome"), Timeout: 50 * time.Millisecond, Interval: 5 * time.Millisecond}},
	})
	if err == nil {
		t.Fatal("expected a mismatch")
	}

	msg := err.Error()

	if !strings.HasPrefix(msg, `text mismatch for id="welcome.register" after 50ms: want="Welcome" got="Bienvenue": `) {
		t.Fatalf("expected the mismatch to survive untouched, got: %s", msg)
	}

	assertNoCancelledFetch(t, msg)
}
