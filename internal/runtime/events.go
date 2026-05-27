package runtime

import (
	"time"

	"github.com/tales-testing/tales/internal/report"
)

// EventSink receives runtime progress events as they happen. Implementations
// must be safe for concurrent calls — scenarios run in parallel and emit
// events from their own goroutine. A nil sink is allowed: the runner checks
// before dispatching.
//
// The sink is the only observability surface available before runner.Run
// returns. PrintConsole runs after the whole suite finishes, so a hang in
// one scenario would otherwise produce zero output — see runner.Run.
type EventSink interface {
	// SuiteStarted fires once, before any scenario goroutine is spawned.
	SuiteStarted(totalScenarios, parallel int, seed int64)

	// ScenarioStarted fires when a scenario actually begins executing
	// (after waiting on the parallelism semaphore — so "starting" really
	// means "running now", not "queued").
	ScenarioStarted(name string)

	// ScenarioEnded fires when a scenario finishes (pass, fail, or skip).
	// failureMessage is empty unless status == StatusFail.
	ScenarioEnded(name string, status report.Status, duration time.Duration, failureMessage string)

	// Heartbeat fires from the runtime's ticker goroutine when
	// Options.HeartbeatInterval > 0. The active slice lists scenarios
	// still in flight, each with the wall-clock elapsed since they
	// started. The slice is empty when nothing is running, in which
	// case the sink should generally stay quiet.
	Heartbeat(active []ActiveScenario)

	// SuiteEnded fires once, after every scenario has ended.
	SuiteEnded(duration time.Duration)
}

// emitSuiteStarted dispatches the event when sink is non-nil. Centralizes the
// nil-check so the runner's hot path stays uncluttered.
func emitSuiteStarted(sink EventSink, total, parallel int, seed int64) {
	if sink == nil {
		return
	}

	sink.SuiteStarted(total, parallel, seed)
}

func emitScenarioStarted(sink EventSink, name string) {
	if sink == nil {
		return
	}

	sink.ScenarioStarted(name)
}

func emitHeartbeat(sink EventSink, active []ActiveScenario) {
	if sink == nil || len(active) == 0 {
		return
	}

	sink.Heartbeat(active)
}

func emitScenarioEnded(sink EventSink, scenario *report.ScenarioResult) {
	if sink == nil || scenario == nil {
		return
	}

	failureMessage := ""
	if scenario.Failure != nil {
		failureMessage = scenario.Failure.Message
	}

	sink.ScenarioEnded(scenario.Name, scenario.Status, scenario.Duration, failureMessage)
}

func emitSuiteEnded(sink EventSink, duration time.Duration) {
	if sink == nil {
		return
	}

	sink.SuiteEnded(duration)
}
