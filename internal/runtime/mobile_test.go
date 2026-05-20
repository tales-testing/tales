package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/parser"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/provider/mobile"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple"
	"github.com/hyperxlab/tales/internal/provider/mobile/driver"
	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
	"github.com/hyperxlab/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

type stubMobileDriver struct {
	driver.NoopDriver

	hierarchy *tree.ViewNode
	taps      atomic.Int32
	inputs    []string
}

func (s *stubMobileDriver) Hierarchy(_ context.Context, _ string) (*tree.ViewNode, error) {
	return s.hierarchy, nil
}
func (s *stubMobileDriver) Tap(_ context.Context, _, _ string, _, _ float64) error {
	s.taps.Add(1)

	return nil
}
func (s *stubMobileDriver) InputText(_ context.Context, _, _, t string, _ bool) error {
	s.inputs = append(s.inputs, t)

	return nil
}
func (s *stubMobileDriver) Screenshot(_ context.Context) ([]byte, error) {
	return []byte("png"), nil
}

type noopSim struct{}

func (noopSim) FindDeviceByName(_ context.Context, _ string) (apple.Device, error) {
	return apple.Device{UDID: "UDID"}, nil
}
func (noopSim) Boot(_ context.Context, _ string) error                        { return nil }
func (noopSim) WaitBooted(_ context.Context, _ string, _ time.Duration) error { return nil }
func (noopSim) Install(_ context.Context, _, _ string) error                  { return nil }
func (noopSim) Uninstall(_ context.Context, _, _ string) error                { return nil }
func (noopSim) Launch(_ context.Context, _, _ string) error                   { return nil }
func (noopSim) Terminate(_ context.Context, _, _ string) error                { return nil }
func (noopSim) Screenshot(_ context.Context, _, _ string) error               { return nil }

func newStubProvider(drv *stubMobileDriver) *mobile.Provider {
	builder := mobile.SessionBuilderFunc(func(_ context.Context, target apple.Target) (*mobile.Session, error) {
		return &mobile.Session{
			Target: target,
			UDID:   "UDID",
			Driver: drv,
			Lifecycle: &apple.Lifecycle{
				Simctl: noopSim{},
			},
		}, nil
	})

	return mobile.New(mobile.WithSessionBuilder(builder))
}

func loadTales(t *testing.T, content string) *model.Suite {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "in.tales")

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write tales file: %v", err)
	}

	suite, diags := parser.LoadPath(path)
	if diags.HasErrors() {
		t.Fatalf("load: %s", diags.Error())
	}

	return suite
}

const mobileTalesContent = `version = 1

config {
  mobile = {
    targets = {
      iphone = {
        platform    = "ios"
        device_name = "iPhone 16"
        app         = "./MyApp.app"
        bundle_id   = "com.example.MyApp"
        driver = {
          host     = "127.0.0.1"
          port     = 9080
          external = true
        }
      }
    }
  }
}

scenario "demo" {
  step "mobile" "register" {
    platform = "ios"
    target   = "iphone"
    actions {
      tap {
        id = "welcome.register"
      }
      input_text {
        id    = "register.email"
        value = "user@example.com"
      }
    }
    expect {
      visible {
        id = "register.verification_code"
        timeout = "1s"
      }
    }
    capture {
      email = value("register.email")
    }
  }
}
`

func sampleHierarchyForTales() *tree.ViewNode {
	return &tree.ViewNode{
		ID: "root", Visible: true, Enabled: true,
		Children: []*tree.ViewNode{
			{ID: "welcome.register", Visible: true, Enabled: true, Bounds: tree.Rect{X: 0, Y: 0, Width: 100, Height: 40}},
			{ID: "register.email", Visible: true, Enabled: true, Value: "user@example.com", Bounds: tree.Rect{X: 0, Y: 60, Width: 200, Height: 30}},
			{ID: "register.verification_code", Visible: true, Enabled: true},
		},
	}
}

func TestRunnerDispatchesMobileStep(t *testing.T) {
	t.Parallel()

	suite := loadTales(t, mobileTalesContent)
	drv := &stubMobileDriver{hierarchy: sampleHierarchyForTales()}
	p := newStubProvider(drv)

	runner := NewRunner(provider.NewRegistry(p))

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if result.Failed() {
		t.Fatalf("expected pass, got result: %+v", result)
	}

	if got := drv.taps.Load(); got < 2 {
		t.Fatalf("expected at least 2 taps (tap + input_text focus), got %d", got)
	}

	if len(drv.inputs) != 1 || drv.inputs[0] != "user@example.com" {
		t.Fatalf("unexpected inputs: %v", drv.inputs)
	}

	scenario := result.Scenarios[0]
	step := scenario.Steps[0]

	if step.Status != report.StatusPass {
		t.Fatalf("expected pass, got %+v", step)
	}
}

func TestRunnerMobileCaptureUsesValueFunction(t *testing.T) {
	t.Parallel()

	suite := loadTales(t, mobileTalesContent)
	drv := &stubMobileDriver{hierarchy: sampleHierarchyForTales()}
	p := newStubProvider(drv)

	runner := NewRunner(provider.NewRegistry(p))

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if result.Failed() {
		t.Fatal("expected suite to pass")
	}

	// The hierarchy is recorded for the mobile step under the scenario / step
	// names, and value("register.email") returns the stored value.
	hierarchy := p.LastHierarchy("demo", "register")
	if hierarchy == nil {
		t.Fatal("expected hierarchy recorded")
	}

	node, found, _ := tree.FindByID(hierarchy, "register.email")
	if !found || node.Value != "user@example.com" {
		t.Fatalf("expected register.email value, got node=%+v found=%v", node, found)
	}
}

func TestRunnerMobileFailureProducesErrorDetail(t *testing.T) {
	t.Parallel()

	suite := loadTales(t, mobileTalesContent)
	drv := &stubMobileDriver{
		hierarchy: &tree.ViewNode{ID: "root", Visible: true},
	}
	p := newStubProvider(drv)

	runner := NewRunner(provider.NewRegistry(p))

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !result.Failed() {
		t.Fatal("expected failure when target element is missing")
	}

	step := result.Scenarios[0].Steps[0]
	if step.Failure == nil || !strings.Contains(step.Failure.Message, "welcome.register") {
		t.Fatalf("expected failure on welcome.register, got %+v", step.Failure)
	}
}

func TestArtifactsFromOutputExtractsValues(t *testing.T) {
	t.Parallel()

	output := &provider.Output{
		Response: map[string]cty.Value{
			"artifacts": cty.ListVal([]cty.Value{
				cty.ObjectVal(map[string]cty.Value{
					"type": cty.StringVal("screenshot"),
					"path": cty.StringVal("/tmp/x.png"),
				}),
			}),
		},
	}

	got := artifactsFromOutput(output)
	if len(got) != 1 || got[0].Type != "screenshot" || got[0].Path != "/tmp/x.png" {
		t.Fatalf("unexpected artifacts: %+v", got)
	}
}

func TestArtifactsFromOutputSkipsMalformedEntries(t *testing.T) {
	t.Parallel()

	// Mix of well-formed and malformed entries: a null item, an entry with a
	// non-string type (we keep path but drop the bogus type), an entry with
	// a null path (must be skipped — path is the load-bearing field), and a
	// well-formed entry. None of these must panic.
	output := &provider.Output{
		Response: map[string]cty.Value{
			"artifacts": cty.TupleVal([]cty.Value{
				cty.NullVal(cty.Object(map[string]cty.Type{"type": cty.String, "path": cty.String})),
				cty.ObjectVal(map[string]cty.Value{
					"type": cty.NumberIntVal(42),
					"path": cty.StringVal("/tmp/bad-type.png"),
				}),
				cty.ObjectVal(map[string]cty.Value{
					"type": cty.StringVal("screenshot"),
					"path": cty.NullVal(cty.String),
				}),
				cty.ObjectVal(map[string]cty.Value{
					"type": cty.StringVal("screenshot"),
					"path": cty.StringVal("/tmp/ok.png"),
				}),
			}),
		},
	}

	got := artifactsFromOutput(output)
	if len(got) != 2 {
		t.Fatalf("expected 2 surviving artifacts, got %d: %+v", len(got), got)
	}

	if got[0].Type != "" || got[0].Path != "/tmp/bad-type.png" {
		t.Fatalf("expected bad-type artifact path to survive with empty type, got %+v", got[0])
	}

	if got[1].Type != "screenshot" || got[1].Path != "/tmp/ok.png" {
		t.Fatalf("unexpected well-formed artifact: %+v", got[1])
	}
}
