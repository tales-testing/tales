package provider

import (
	"context"

	"github.com/tales-testing/tales/internal/model"
)

// ScenarioContext carries scenario-wide paths a hook may need to honor user
// expressions (paths relative to the scenario workspace, project root). It is
// kept minimal on purpose: the runner sets it once per scenario before any
// hook fires and the values do not change for the lifetime of the scenario.
type ScenarioContext struct {
	// Workdir is the absolute root of the per-scenario workspace.
	Workdir string
	// ArtifactsDir is the stable artifacts subdirectory of Workdir.
	ArtifactsDir string
	// ProjectDir is the absolute project / repository root.
	ProjectDir string
}

// ScenarioArtifact is the provider-agnostic shape a ScenarioHook returns
// when it has produced a file the user should see in the report. The runner
// converts it into a report.Artifact and attaches it to the scenario.
type ScenarioArtifact struct {
	// Type names the artifact in a tooling-friendly way (e.g. "video").
	Type string
	// Path is absolute or relative to the scenario workdir; the report
	// layer surfaces it verbatim.
	Path string
}

// ScenarioHook is an optional capability a Provider may implement to react
// to scenario-level boundaries. The runner type-asserts every registered
// provider (the same pattern used for io.Closer at suite shutdown) and
// invokes the matching method around runScenario.
//
// BeginScenario fires once after the scenario workspace is built and the
// scenario-level skip rules have been evaluated (so skipped scenarios do
// not run any hook). Returning an error fails the scenario before any step
// runs; EndScenario still fires so partially-initialized resources are
// released.
//
// EndScenario fires once between the main steps and the teardown steps,
// so a hook can finalize side effects (e.g. flush a screen recording) in
// time for teardown assertions to observe them. It also fires from a
// defer as a panic / early-return safety net; the runner guarantees a
// single invocation per scenario regardless of which path triggered it.
// The runErr argument is the first error captured by the runner (or nil
// on success); hooks may use it to decide whether to attach a partial
// artifact or to skip cleanup. Returned artifacts are appended to the
// scenario result in the order hooks were invoked.
type ScenarioHook interface {
	BeginScenario(ctx context.Context, scenario *model.Scenario, hctx ScenarioContext) error
	EndScenario(ctx context.Context, scenario *model.Scenario, hctx ScenarioContext, runErr error) ([]ScenarioArtifact, error)
}
