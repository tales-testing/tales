package embeddeddriver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type recordingRunner struct {
	calls []recordedCall
	hook  func(name string, args []string, logPath string) error
}

type recordedCall struct {
	name    string
	args    []string
	logPath string
}

func (r *recordingRunner) Run(_ context.Context, name string, args []string, logPath string, _ map[string]string) error {
	r.calls = append(r.calls, recordedCall{name: name, args: append([]string(nil), args...), logPath: logPath})

	if r.hook != nil {
		return r.hook(name, args, logPath)
	}

	return nil
}

func TestBuildForTestingSuccess(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	derived := filepath.Join(tmp, "derived")
	source := filepath.Join(tmp, "source")
	logPath := filepath.Join(tmp, "build.log")
	products := filepath.Join(derived, "Build", "Products")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}

	runner := &recordingRunner{
		hook: func(_ string, _ []string, lp string) error {
			if err := os.MkdirAll(products, 0o755); err != nil {
				return err
			}
			xctestrun := filepath.Join(products, "TalesAppleDriverUITests_Debug_iphonesimulator.xctestrun")
			if err := os.WriteFile(xctestrun, []byte("xml"), 0o600); err != nil {
				return err
			}
			if err := os.WriteFile(lp, []byte("build ok\n"), 0o600); err != nil {
				return err
			}
			return nil
		},
	}

	builder := &XcodebuildBuilder{Runner: runner}

	xctestrun, err := builder.BuildForTesting(context.Background(), source, derived, logPath)
	if err != nil {
		t.Fatalf("BuildForTesting: %v", err)
	}

	if filepath.Base(xctestrun) != "TalesAppleDriverUITests_Debug_iphonesimulator.xctestrun" {
		t.Fatalf("unexpected xctestrun path %q", xctestrun)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected one xcodebuild invocation, got %d", len(runner.calls))
	}

	if runner.calls[0].name != "xcodebuild" {
		t.Fatalf("expected xcodebuild, got %q", runner.calls[0].name)
	}

	if !containsArg(runner.calls[0].args, "build-for-testing") {
		t.Errorf("args missing build-for-testing: %v", runner.calls[0].args)
	}

	if !containsArg(runner.calls[0].args, "CODE_SIGNING_ALLOWED=NO") {
		t.Errorf("args missing CODE_SIGNING_ALLOWED=NO: %v", runner.calls[0].args)
	}

	if !containsArg(runner.calls[0].args, "-sdk") {
		t.Errorf("args missing -sdk flag: %v", runner.calls[0].args)
	}
}

func TestBuildForTestingFailureWrapsLogPath(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "build.log")
	runner := &recordingRunner{
		hook: func(_ string, _ []string, _ string) error { return errors.New("xcodebuild boom") },
	}

	builder := &XcodebuildBuilder{Runner: runner}

	_, err := builder.BuildForTesting(context.Background(), filepath.Join(tmp, "source"), filepath.Join(tmp, "derived"), logPath)
	if err == nil {
		t.Fatalf("expected build error")
	}

	var berr *BuildError
	if !errors.As(err, &berr) {
		t.Fatalf("expected *BuildError, got %T (%v)", err, err)
	}

	if berr.LogPath != logPath {
		t.Errorf("BuildError.LogPath = %q, want %q", berr.LogPath, logPath)
	}
}

func TestBuildForTestingRequiresRunner(t *testing.T) {
	t.Parallel()

	builder := &XcodebuildBuilder{}

	if _, err := builder.BuildForTesting(context.Background(), "src", "derived", "log"); err == nil {
		t.Fatalf("expected error when runner is nil")
	}
}

func TestBuildForTestingFailsWhenNoXCTestRunProduced(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	runner := &recordingRunner{
		hook: func(_ string, _ []string, _ string) error { return nil },
	}

	builder := &XcodebuildBuilder{Runner: runner}

	_, err := builder.BuildForTesting(context.Background(), filepath.Join(tmp, "source"), filepath.Join(tmp, "derived"), filepath.Join(tmp, "build.log"))
	if err == nil {
		t.Fatalf("expected error when no .xctestrun is produced")
	}
}

func TestBuildForTestingFailsOnMultipleXCTestRun(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	derived := filepath.Join(tmp, "derived")
	products := filepath.Join(derived, "Build", "Products")

	runner := &recordingRunner{
		hook: func(_ string, _ []string, _ string) error {
			if err := os.MkdirAll(products, 0o755); err != nil {
				return err
			}

			for _, name := range []string{"a.xctestrun", "b.xctestrun"} {
				if err := os.WriteFile(filepath.Join(products, name), []byte("x"), 0o600); err != nil {
					return err
				}
			}

			return nil
		},
	}

	builder := &XcodebuildBuilder{Runner: runner}

	_, err := builder.BuildForTesting(context.Background(), filepath.Join(tmp, "source"), derived, filepath.Join(tmp, "build.log"))
	if err == nil {
		t.Fatalf("expected error when multiple xctestrun files are produced")
	}
}

func TestBuildForTestingWipesStaleXCTestRun(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	derived := filepath.Join(tmp, "derived")
	products := filepath.Join(derived, "Build", "Products")

	if err := os.MkdirAll(products, 0o755); err != nil {
		t.Fatalf("mkdir products: %v", err)
	}

	stalePath := filepath.Join(products, "stale.xctestrun")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	runner := &recordingRunner{
		hook: func(_ string, _ []string, _ string) error {
			if _, statErr := os.Stat(stalePath); !os.IsNotExist(statErr) {
				return errors.New("stale xctestrun should have been wiped before build")
			}

			fresh := filepath.Join(products, "fresh.xctestrun")
			return os.WriteFile(fresh, []byte("fresh"), 0o600)
		},
	}

	builder := &XcodebuildBuilder{Runner: runner}

	xctestrun, err := builder.BuildForTesting(context.Background(), filepath.Join(tmp, "source"), derived, filepath.Join(tmp, "build.log"))
	if err != nil {
		t.Fatalf("BuildForTesting: %v", err)
	}

	if filepath.Base(xctestrun) != "fresh.xctestrun" {
		t.Fatalf("expected fresh xctestrun, got %q", xctestrun)
	}
}

func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}
