package browser

import (
	"strings"
	"testing"
	"time"

	"github.com/zclconf/go-cty/cty"
)

func cfg(targets map[string]cty.Value) map[string]cty.Value {
	return map[string]cty.Value{
		"browser": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(targets),
		}),
	}
}

func TestResolveTargetDefaultsApplied(t *testing.T) {
	t.Parallel()

	config := cfg(map[string]cty.Value{
		"chrome": cty.ObjectVal(map[string]cty.Value{
			"browser":  cty.StringVal("chrome"),
			"headless": cty.True,
		}),
	})

	target, err := ResolveTarget(config, "chrome")
	if err != nil {
		t.Fatalf("ResolveTarget returned error: %v", err)
	}

	if target.Name != "chrome" {
		t.Errorf("Name = %q, want chrome", target.Name)
	}

	if !target.Driver.Headless {
		t.Error("Headless should default true")
	}

	if target.Driver.Viewport.Width != defaultWindowWidth || target.Driver.Viewport.Height != defaultWindowHeight {
		t.Errorf("Viewport = %v, want default", target.Driver.Viewport)
	}

	if target.Driver.Timeout != defaultTimeout {
		t.Errorf("Timeout = %v, want %v", target.Driver.Timeout, defaultTimeout)
	}
}

func TestResolveTargetOverrides(t *testing.T) {
	t.Parallel()

	config := cfg(map[string]cty.Value{
		"chrome": cty.ObjectVal(map[string]cty.Value{
			"browser":    cty.StringVal("chrome"),
			"headless":   cty.False,
			"executable": cty.StringVal("/custom/chrome"),
			"args":       cty.ListVal([]cty.Value{cty.StringVal("--disable-gpu")}),
			"timeout":    cty.StringVal("45s"),
			"viewport": cty.ObjectVal(map[string]cty.Value{
				"width":  cty.NumberIntVal(1920),
				"height": cty.NumberIntVal(1080),
			}),
		}),
	})

	target, err := ResolveTarget(config, "chrome")
	if err != nil {
		t.Fatalf("ResolveTarget returned error: %v", err)
	}

	if target.Driver.Headless {
		t.Error("Headless should be false")
	}

	if target.Driver.Executable != "/custom/chrome" {
		t.Errorf("Executable = %q", target.Driver.Executable)
	}

	if len(target.Driver.Args) != 1 || target.Driver.Args[0] != "--disable-gpu" {
		t.Errorf("Args = %v", target.Driver.Args)
	}

	if target.Driver.Timeout != 45*time.Second {
		t.Errorf("Timeout = %v", target.Driver.Timeout)
	}

	if target.Driver.Viewport.Width != 1920 || target.Driver.Viewport.Height != 1080 {
		t.Errorf("Viewport = %v", target.Driver.Viewport)
	}
}

func TestResolveTargetUnsupportedBrowser(t *testing.T) {
	t.Parallel()

	config := cfg(map[string]cty.Value{
		"firefox": cty.ObjectVal(map[string]cty.Value{
			"browser": cty.StringVal("firefox"),
		}),
	})

	_, err := ResolveTarget(config, "firefox")
	if err == nil || !strings.Contains(err.Error(), `unsupported browser "firefox"`) {
		t.Fatalf("expected unsupported-browser error, got: %v", err)
	}
}

func TestResolveTargetMissingTarget(t *testing.T) {
	t.Parallel()

	config := cfg(map[string]cty.Value{
		"chrome": cty.ObjectVal(map[string]cty.Value{
			"browser": cty.StringVal("chrome"),
		}),
	})

	_, err := ResolveTarget(config, "missing")
	if err == nil || !strings.Contains(err.Error(), `browser target "missing" not found`) {
		t.Fatalf("expected missing-target error, got: %v", err)
	}
}

func TestResolveTargetMissingConfig(t *testing.T) {
	t.Parallel()

	_, err := ResolveTarget(map[string]cty.Value{}, "chrome")
	if err == nil || !strings.Contains(err.Error(), "config.browser is not declared") {
		t.Fatalf("expected missing-config error, got: %v", err)
	}
}

func TestResolveTargetSingleTargetShortcut(t *testing.T) {
	t.Parallel()

	config := cfg(map[string]cty.Value{
		"only": cty.ObjectVal(map[string]cty.Value{
			"browser": cty.StringVal("chrome"),
		}),
	})

	target, err := ResolveTarget(config, "")
	if err != nil {
		t.Fatalf("single-target shortcut should resolve: %v", err)
	}

	if target.Name != "only" {
		t.Errorf("Name = %q, want only", target.Name)
	}
}

func TestResolveTargetMultiTargetRequiresName(t *testing.T) {
	t.Parallel()

	config := cfg(map[string]cty.Value{
		"a": cty.ObjectVal(map[string]cty.Value{"browser": cty.StringVal("chrome")}),
		"b": cty.ObjectVal(map[string]cty.Value{"browser": cty.StringVal("chrome")}),
	})

	_, err := ResolveTarget(config, "")
	if err == nil || !strings.Contains(err.Error(), "multiple browser targets declared") {
		t.Fatalf("expected ambiguity error, got: %v", err)
	}
}
