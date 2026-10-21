package chrome

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

func floatPtr(v float64) *float64 { return &v }

// scriptedProbe answers the given results in order, then repeats the last.
type scriptedProbe struct {
	answers        []probeResult
	errs           []error
	calls          int
	scrollAttempts []int
}

func (s *scriptedProbe) probe(_ context.Context, scrollAttempt int) (probeResult, error) {
	i := min(s.calls, len(s.answers)-1)
	s.calls++
	s.scrollAttempts = append(s.scrollAttempts, scrollAttempt)

	var err error
	if i < len(s.errs) {
		err = s.errs[i]
	}

	return s.answers[i], err
}

func TestAwaitActionableRetriesUntilReady(t *testing.T) {
	t.Parallel()

	probe := &scriptedProbe{answers: []probeResult{
		{Reason: reasonNotVisible},
		{Reason: reasonNotStable},
		{Reason: reasonCovered, Hit: `<div class="veil">`, X: floatPtr(10), Y: floatPtr(20)},
		{Ready: true, X: floatPtr(10), Y: floatPtr(20)},
	}}

	res, err := awaitActionable(context.Background(), time.Millisecond, probe.probe)
	if err != nil {
		t.Fatalf("awaitActionable: %v", err)
	}

	if !res.Ready || *res.point() != (driver.Point{X: 10, Y: 20}) {
		t.Fatalf("result = %+v, want ready at (10, 20)", res)
	}

	if probe.calls != 4 {
		t.Fatalf("probe calls = %d, want 4", probe.calls)
	}
}

func TestAwaitActionableKeepsTheLastReasonAtTheDeadline(t *testing.T) {
	t.Parallel()

	probe := &scriptedProbe{answers: []probeResult{
		{Reason: reasonCovered, Hit: `<div class="toast">`, X: floatPtr(912), Y: floatPtr(684)},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	res, err := awaitActionable(ctx, time.Millisecond, probe.probe)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}

	if got := res.describe(); got != `point (912, 684) is covered by <div class="toast">` {
		t.Fatalf("describe() = %q", got)
	}
}

func TestAwaitActionableIgnoresTheProbeTheDeadlineInterrupted(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	probe := func(_ context.Context, _ int) (probeResult, error) {
		calls++
		if calls == 1 {
			return probeResult{Reason: reasonDisabled}, nil
		}

		// The deadline fires while the second probe is in flight: its
		// error says nothing about the element.
		cancel()

		return probeResult{}, context.Canceled
	}

	res, err := awaitActionable(ctx, time.Millisecond, probe)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	if res.Reason != reasonDisabled {
		t.Fatalf("reason = %q, want the last real answer %q", res.Reason, reasonDisabled)
	}
}

func TestAwaitActionableRetriesAProbeThatFailed(t *testing.T) {
	t.Parallel()

	probe := &scriptedProbe{
		answers: []probeResult{{}, {Ready: true, X: floatPtr(1), Y: floatPtr(2)}},
		errs:    []error{errors.New("Execution context was destroyed")},
	}

	res, err := awaitActionable(context.Background(), time.Millisecond, probe.probe)
	if err != nil || !res.Ready {
		t.Fatalf("awaitActionable = %+v, %v; want ready after a retry", res, err)
	}
}

func TestAwaitActionableReportsAProbeThatNeverAnswers(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	probe := func(_ context.Context, _ int) (probeResult, error) {
		return probeResult{}, errors.New("Execution context was destroyed")
	}

	res, _ := awaitActionable(ctx, time.Millisecond, probe)

	want := "the page did not answer the actionability probe: Execution context was destroyed"
	if got := res.describe(); got != want {
		t.Fatalf("describe() = %q, want %q", got, want)
	}
}

func TestAwaitActionableStopsOnAFatalAnswer(t *testing.T) {
	t.Parallel()

	probe := &scriptedProbe{answers: []probeResult{{Fatal: "invalid selector: '[[' is not a valid selector."}}}

	res, err := awaitActionable(context.Background(), time.Millisecond, probe.probe)
	if err == nil || probe.calls != 1 {
		t.Fatalf("err = %v after %d calls, want an immediate failure", err, probe.calls)
	}

	got := actionabilityError(clickAction, "[[", res, err, time.Second).Error()
	if got != "click [[: invalid selector: '[[' is not a valid selector." {
		t.Fatalf("error = %q", got)
	}
}

func TestAwaitActionableCountsConsecutiveCoveredProbes(t *testing.T) {
	t.Parallel()

	covered := probeResult{Reason: reasonCovered, Hit: "<header>", X: floatPtr(1), Y: floatPtr(1)}
	probe := &scriptedProbe{answers: []probeResult{
		covered,
		covered,
		{Reason: reasonNotStable},
		covered,
		{Ready: true, X: floatPtr(1), Y: floatPtr(1)},
	}}

	if _, err := awaitActionable(context.Background(), time.Millisecond, probe.probe); err != nil {
		t.Fatalf("awaitActionable: %v", err)
	}

	// Each covered answer moves the next probe to another scroll
	// alignment; any other answer starts over from scroll-if-needed.
	want := []int{0, 1, 2, 0, 1}
	if len(probe.scrollAttempts) != len(want) {
		t.Fatalf("scroll attempts = %v, want %v", probe.scrollAttempts, want)
	}

	for i := range want {
		if probe.scrollAttempts[i] != want[i] {
			t.Fatalf("scroll attempts = %v, want %v", probe.scrollAttempts, want)
		}
	}
}

func TestProbeResultDescribesEveryReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		res  probeResult
		want string
	}{
		{probeResult{Reason: reasonNotFound}, "element not found"},
		{probeResult{Reason: reasonNotVisible}, "element is not visible"},
		{probeResult{Reason: reasonDisabled}, "element is disabled"},
		{probeResult{Reason: reasonNotStable}, "element is not stable (its box moved between two animation frames)"},
		{probeResult{Reason: reasonNoBox}, "element has no box to point at (display: none?)"},
		{probeResult{Reason: reasonOutsideViewport}, "element is outside of the viewport"},
		{
			probeResult{Reason: reasonCovered, Hit: `<span class="msg"> from <div class="toast"> subtree`, X: floatPtr(60.4), Y: floatPtr(59.5)},
			`point (60, 60) is covered by <span class="msg"> from <div class="toast"> subtree`,
		},
		{
			probeResult{Reason: reasonHitsAncestor, Hit: "<body>", X: floatPtr(54), Y: floatPtr(58)},
			"point (54, 58) hits its ancestor <body>, not the element (pointer-events: none, or clipped?)",
		},
		{probeResult{}, "the page did not answer the actionability probe"},
	}

	for _, tc := range cases {
		if got := tc.res.describe(); got != tc.want {
			t.Errorf("describe(%q) = %q, want %q", tc.res.Reason, got, tc.want)
		}
	}
}

func TestActionabilityErrorCarriesTheTestedPoint(t *testing.T) {
	t.Parallel()

	last := probeResult{Reason: reasonCovered, Hit: `<div class="toast" role="status">`, X: floatPtr(37), Y: floatPtr(90)}

	err := actionabilityError(clickAction, "[data-testid='toast.submit']", last, context.DeadlineExceeded, time.Second)

	var notActionable *driver.ActionabilityError
	if !errors.As(err, &notActionable) {
		t.Fatalf("error %T is not a *driver.ActionabilityError", err)
	}

	want := `click [data-testid='toast.submit']: not actionable after 1s: point (37, 90) is covered by <div class="toast" role="status">`
	if err.Error() != want {
		t.Fatalf("error = %q\nwant    %q", err.Error(), want)
	}

	if notActionable.Point == nil || *notActionable.Point != (driver.Point{X: 37, Y: 90}) {
		t.Fatalf("point = %+v, want (37, 90)", notActionable.Point)
	}
}

func TestBudgetOfMeasuresFromTheActionStart(t *testing.T) {
	t.Parallel()

	start := time.Now()

	ctx, cancel := context.WithDeadline(context.Background(), start.Add(2*time.Second))
	defer cancel()

	if got := budgetOf(ctx, start); got != 2*time.Second {
		t.Fatalf("budgetOf = %s, want 2s", got)
	}

	if got := budgetOf(context.Background(), start); got != 0 {
		t.Fatalf("budgetOf without a deadline = %s, want 0", got)
	}
}

func TestMouseActionsMoveBeforePressing(t *testing.T) {
	t.Parallel()

	point := driver.Point{X: 12.5, Y: 40}

	types := func(action pointerAction) []input.MouseType {
		var out []input.MouseType

		for _, a := range mouseActions(action, point) {
			p, ok := a.(*input.DispatchMouseEventParams)
			if !ok {
				t.Fatalf("action %T is not a mouse event", a)
			}

			if p.X != point.X || p.Y != point.Y {
				t.Fatalf("mouse event at (%v, %v), want (%v, %v)", p.X, p.Y, point.X, point.Y)
			}

			out = append(out, p.Type)
		}

		return out
	}

	click := types(clickAction)
	if len(click) != 3 || click[0] != input.MouseMoved || click[1] != input.MousePressed || click[2] != input.MouseReleased {
		t.Fatalf("click events = %v, want moved, pressed, released", click)
	}

	hover := types(hoverAction)
	if len(hover) != 1 || hover[0] != input.MouseMoved {
		t.Fatalf("hover events = %v, want moved only", hover)
	}
}

func TestProbeScriptIsAFunctionExpression(t *testing.T) {
	t.Parallel()

	// The driver evaluates `(<probeScript>)(selector, options)`: the
	// embedded file must hold exactly one function expression.
	script := strings.TrimSpace(probeScript)

	start := strings.Index(script, "(async function (selector, options) {")
	if start < 0 {
		t.Fatal("probeScript does not define the async probe function")
	}

	if !strings.HasSuffix(script, "})") {
		t.Fatalf("probeScript must end with the function expression, got %q", script[len(script)-20:])
	}
}
