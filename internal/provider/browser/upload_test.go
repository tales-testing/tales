package browser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// writeFixture creates a file under dir and returns its absolute path.
func writeFixture(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	return path
}

func TestResolveUploadPathsRelativeToStepFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fixtureDir := filepath.Join(dir, "fixtures")

	if err := os.MkdirAll(fixtureDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	want := writeFixture(t, fixtureDir, "contract.pdf")
	stepFile := filepath.Join(dir, "suite.tales")

	got, err := resolveUploadPaths(stepFile, []string{"./fixtures/contract.pdf"})
	if err != nil {
		t.Fatalf("resolveUploadPaths returned: %v", err)
	}

	if len(got) != 1 || got[0] != want {
		t.Fatalf("resolveUploadPaths = %v, want [%s]", got, want)
	}
}

// A suite invoked as `tales test ./e2e/browser` carries a relative
// Step.File. DOM.setFileInputFiles takes host paths and silently attaches
// nothing when handed a relative one, so the resolver must always return an
// absolute path.
func TestResolveUploadPathsAlwaysAbsolute(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "contract.pdf")

	t.Chdir(dir)

	got, err := resolveUploadPaths("suite.tales", []string{"contract.pdf"})
	if err != nil {
		t.Fatalf("resolveUploadPaths returned: %v", err)
	}

	if !filepath.IsAbs(got[0]) {
		t.Fatalf("resolveUploadPaths = %q, want an absolute path", got[0])
	}
}

func TestResolveUploadPathsAbsolute(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	want := writeFixture(t, dir, "contract.pdf")

	got, err := resolveUploadPaths(filepath.Join(dir, "suite.tales"), []string{want})
	if err != nil {
		t.Fatalf("resolveUploadPaths returned: %v", err)
	}

	if len(got) != 1 || got[0] != want {
		t.Fatalf("resolveUploadPaths = %v, want [%s]", got, want)
	}
}

func TestResolveUploadPathsErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stepFile := filepath.Join(dir, "suite.tales")

	cases := []struct {
		name     string
		stepFile string
		paths    []string
		want     string
	}{
		{name: "empty list", stepFile: stepFile, paths: nil, want: "at least one file"},
		{name: "missing file", stepFile: stepFile, paths: []string{"./nope.pdf"}, want: "nope.pdf"},
		{name: "directory", stepFile: stepFile, paths: []string{"."}, want: "is a directory"},
		{name: "empty entry", stepFile: stepFile, paths: []string{""}, want: "path is empty"},
		{name: "unknown step file", stepFile: "", paths: []string{"./rel.pdf"}, want: "step file is unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := resolveUploadPaths(tc.stepFile, tc.paths)
			if err == nil {
				t.Fatalf("expected an error for %s", tc.name)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestProviderExecuteUploadFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := writeFixture(t, dir, "contract.pdf")
	second := writeFixture(t, dir, "appendix.pdf")

	p, fake := buildFakeProvider(t, provider.CaptureNone, nil)
	defer p.Close()

	out, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "attach", File: filepath.Join(dir, "suite.tales")},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser: &provider.BrowserExecution{
			TargetName: "chrome",
			Actions: []provider.BrowserActionExec{{
				Kind:     model.BrowserActionUploadFile,
				Selector: "#file",
				Paths:    []string{"contract.pdf", "./appendix.pdf"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned: %v", err)
	}

	call := findCall(t, fake, "SetFileInputs")

	if call.Selector != "#file" {
		t.Errorf("selector = %q, want #file", call.Selector)
	}

	if want := first + "," + second; call.Value != want {
		t.Errorf("paths = %q, want %q", call.Value, want)
	}

	if len(out.ActionResults) != 1 || out.ActionResults[0].Status != actionStatusPass {
		t.Fatalf("unexpected action results: %+v", out.ActionResults)
	}
}

func TestProviderExecuteUploadFileMissingPathFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	p, fake := buildFakeProvider(t, provider.CaptureNone, nil)
	defer p.Close()

	_, err := p.Execute(context.Background(), provider.Input{
		Scenario: "demo",
		Step:     &model.Step{Name: "attach", File: filepath.Join(dir, "suite.tales")},
		Phase:    "step",
		Attempt:  1,
		Config:   sampleConfig(),
		Browser: &provider.BrowserExecution{
			TargetName: "chrome",
			Actions: []provider.BrowserActionExec{{
				Kind:     model.BrowserActionUploadFile,
				Selector: "#file",
				Paths:    []string{"./missing.pdf"},
			}},
		},
	})
	if err == nil {
		t.Fatal("expected the step to fail on a missing upload path")
	}

	if !strings.Contains(err.Error(), "missing.pdf") {
		t.Fatalf("error = %q, want it to name the missing file", err.Error())
	}

	for _, c := range fake.Calls {
		if c.Method == "SetFileInputs" {
			t.Fatal("driver must not be called when a path does not exist")
		}
	}
}

func findCall(t *testing.T, fake *driver.FakeDriver, method string) driver.Call {
	t.Helper()

	for _, c := range fake.Calls {
		if c.Method == method {
			return c
		}
	}

	t.Fatalf("no %s call recorded, got %s", method, fake.CallsJoined())

	return driver.Call{}
}
