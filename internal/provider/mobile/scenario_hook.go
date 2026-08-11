package mobile

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/hashicorp/hcl/v2"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/workspace"
	"github.com/zclconf/go-cty/cty"
)

// artifactTypeRecording is the artifact type string surfaced for the screen
// recording produced by a scenario-level record block.
const artifactTypeRecording = "recording"

// pendingRecord holds the recording spec resolved at BeginScenario time.
// The Recorder is built later, lazily, the first time a mobile session is
// acquired for the scenario, so the device is guaranteed to be up and its
// id known.
type pendingRecord struct {
	targetName string
	options    RecordOptions
}

// activeRecording owns the live Recorder for a running scenario.
type activeRecording struct {
	recorder Recorder
	output   string
}

// recordController centralizes the recorder state so the Provider struct
// only carries a pointer. It is created lazily so providers built without
// any record block pay zero memory.
type recordController struct {
	mu          sync.Mutex
	newRecorder RecorderFactory
	pending     map[string]*pendingRecord
	active      map[string]*activeRecording
	// deviceUsed maps a device id to the scenario recording it, which
	// blocks two scenarios from recording the same device at once.
	deviceUsed map[string]string
}

func (p *Provider) recordCtrl() *recordController {
	p.recordOnce.Do(func() {
		p.recording = &recordController{
			newRecorder: p.recorderFactory,
			pending:     map[string]*pendingRecord{},
			active:      map[string]*activeRecording{},
			deviceUsed:  map[string]string{},
		}
	})

	return p.recording
}

// BeginScenario implements provider.ScenarioHook on the mobile provider.
// When the scenario carries a record block, the expressions are resolved
// here so the simulator-boot path can read a ready-to-spawn options bundle
// without re-evaluating HCL. The recorder itself is started lazily inside
// acquireSession to ensure the simulator is booted (UDID known) first.
func (p *Provider) BeginScenario(_ context.Context, scenario *model.Scenario, hctx provider.ScenarioContext) error {
	if scenario.Record == nil {
		return nil
	}

	spec, err := resolveRecordSpec(scenario.Record, hctx)
	if err != nil {
		return fmt.Errorf("mobile: resolve record block: %w", err)
	}

	ctrl := p.recordCtrl()

	ctrl.mu.Lock()
	ctrl.pending[scenario.Name] = spec
	ctrl.mu.Unlock()

	return nil
}

// EndScenario stops the active recording for this scenario (if any) and
// returns the captured artifact path. A scenario whose record block never
// reached the simulator-boot point (no mobile step) returns no artifact.
func (p *Provider) EndScenario(ctx context.Context, scenario *model.Scenario, _ provider.ScenarioContext, _ error) ([]provider.ScenarioArtifact, error) {
	if scenario.Record == nil {
		return nil, nil
	}

	ctrl := p.recordCtrl()

	ctrl.mu.Lock()

	rec, hasActive := ctrl.active[scenario.Name]
	delete(ctrl.pending, scenario.Name)
	delete(ctrl.active, scenario.Name)

	for deviceID, name := range ctrl.deviceUsed {
		if name == scenario.Name {
			delete(ctrl.deviceUsed, deviceID)
		}
	}

	ctrl.mu.Unlock()

	if !hasActive {
		return nil, nil
	}

	path, stopErr := rec.recorder.Stop(ctx)
	if path == "" {
		path = rec.output
	}

	artifacts := []provider.ScenarioArtifact{{Type: artifactTypeRecording, Path: path}}

	if stopErr != nil {
		return artifacts, fmt.Errorf("stop screen recording: %w", stopErr)
	}

	return artifacts, nil
}

// maybeStartRecording is called from Execute right after a session is
// acquired. It starts a Recorder when the scenario has a pending record
// spec that matches this session's target. A non-matching target is
// silently ignored: the spec is still pending and will be picked up by a
// later mobile step targeting the right device.
//
// Errors are wrapped and returned so the caller can decide whether to fail
// the step; the current call site logs them and proceeds rather than
// breaking an otherwise valid mobile step over a recording problem.
func (p *Provider) maybeStartRecording(ctx context.Context, scenarioName string, session *Session) error {
	if scenarioName == "" || session == nil || session.DeviceID == "" {
		return nil
	}

	ctrl := p.recordCtrl()

	ctrl.mu.Lock()

	spec, hasPending := ctrl.pending[scenarioName]
	if !hasPending {
		ctrl.mu.Unlock()

		return nil
	}

	if _, alreadyActive := ctrl.active[scenarioName]; alreadyActive {
		ctrl.mu.Unlock()

		return nil
	}

	if spec.targetName != "" && spec.targetName != session.Target.Name {
		ctrl.mu.Unlock()

		return nil
	}

	if other, used := ctrl.deviceUsed[session.DeviceID]; used && other != scenarioName {
		ctrl.mu.Unlock()

		return fmt.Errorf("scenario %q cannot start a recording on device %s: scenario %q is already recording", scenarioName, session.DeviceID, other)
	}

	newRecorder := ctrl.newRecorder
	opts := spec.options

	ctrl.mu.Unlock()

	if newRecorder == nil {
		return fmt.Errorf("platform %q does not support scenario recording", session.Target.Platform)
	}

	recorder := newRecorder(session.DeviceID)

	if err := recorder.Start(ctx, opts); err != nil {
		return fmt.Errorf("start screen recording: %w", err)
	}

	ctrl.mu.Lock()
	ctrl.active[scenarioName] = &activeRecording{recorder: recorder, output: opts.Output}
	ctrl.deviceUsed[session.DeviceID] = scenarioName
	ctrl.mu.Unlock()

	return nil
}

// resolveRecordSpec evaluates the scenario record expressions against a
// minimal HCL context (scenario.workdir, scenario.artifacts_dir,
// project.dir) and returns a ready-to-spawn options bundle. The output
// path is rerouted through the workspace resolver so anything escaping
// the per-scenario workdir is rejected.
func resolveRecordSpec(spec *model.ScenarioRecord, hctx provider.ScenarioContext) (*pendingRecord, error) {
	ctx := scenarioEvalContext(hctx)

	output, err := evalRecordString(spec.Output, ctx, "output")
	if err != nil {
		return nil, err
	}

	if output == "" {
		return nil, errors.New("output is empty")
	}

	resolver := workspace.Resolver{Workdir: hctx.Workdir, ProjectDir: hctx.ProjectDir}

	absOutput, err := resolver.ResolveOutput(output)
	if err != nil {
		return nil, fmt.Errorf("resolve output path: %w", err)
	}

	codec, err := evalOptionalRecordString(spec.Codec, ctx, "codec")
	if err != nil {
		return nil, err
	}

	mask, err := evalOptionalRecordString(spec.Mask, ctx, "mask")
	if err != nil {
		return nil, err
	}

	display, err := evalOptionalRecordString(spec.Display, ctx, "display")
	if err != nil {
		return nil, err
	}

	target, err := evalOptionalRecordString(spec.Target, ctx, "target")
	if err != nil {
		return nil, err
	}

	force := true

	if !spec.Force.Empty() {
		b, err := evalRecordBool(spec.Force, ctx, "force")
		if err != nil {
			return nil, err
		}

		force = b
	}

	return &pendingRecord{
		targetName: target,
		options: RecordOptions{
			Output:  absOutput,
			Codec:   codec,
			Mask:    mask,
			Display: display,
			Force:   force,
		},
	}, nil
}

func scenarioEvalContext(hctx provider.ScenarioContext) *hcl.EvalContext {
	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"scenario": cty.ObjectVal(map[string]cty.Value{
				"workdir":       cty.StringVal(hctx.Workdir),
				"artifacts_dir": cty.StringVal(hctx.ArtifactsDir),
			}),
			"project": cty.ObjectVal(map[string]cty.Value{
				"dir": cty.StringVal(hctx.ProjectDir),
			}),
		},
	}
}

func evalRecordString(e model.Expression, ctx *hcl.EvalContext, field string) (string, error) {
	if e.Empty() {
		return "", fmt.Errorf("%s is required", field)
	}

	val, diags := e.Expr.Value(ctx)
	if diags.HasErrors() {
		return "", fmt.Errorf("evaluate %s: %s", field, diags.Error())
	}

	if val.Type() != cty.String {
		return "", fmt.Errorf("%s must be a string, got %s", field, val.Type().FriendlyName())
	}

	return val.AsString(), nil
}

func evalOptionalRecordString(e model.Expression, ctx *hcl.EvalContext, field string) (string, error) {
	if e.Empty() {
		return "", nil
	}

	return evalRecordString(e, ctx, field)
}

func evalRecordBool(e model.Expression, ctx *hcl.EvalContext, field string) (bool, error) {
	val, diags := e.Expr.Value(ctx)
	if diags.HasErrors() {
		return false, fmt.Errorf("evaluate %s: %s", field, diags.Error())
	}

	if val.Type() != cty.Bool {
		return false, fmt.Errorf("%s must be a boolean, got %s", field, val.Type().FriendlyName())
	}

	return val.True(), nil
}
