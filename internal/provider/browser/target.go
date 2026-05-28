package browser

import (
	"fmt"
	"time"

	"github.com/zclconf/go-cty/cty"
)

// Defaults applied when the target config omits the field.
const (
	defaultBrowserName  = "chrome"
	defaultHeadless     = true
	defaultWindowWidth  = 1440
	defaultWindowHeight = 1000
	defaultTimeout      = 30 * time.Second
)

// Target is the resolved configuration for one browser target entry.
type Target struct {
	Name   string
	Driver DriverConfig
}

// DriverConfig captures how the browser provider launches and talks to Chrome.
type DriverConfig struct {
	// Browser identifies the engine. Only "chrome" is accepted in V1.
	Browser string
	// Headless controls --headless flag on the Chrome process.
	Headless bool
	// Executable optionally overrides the auto-detected Chrome binary.
	Executable string
	// Args holds extra command-line flags appended to Chrome's invocation
	// (e.g. ["--disable-gpu"]).
	Args []string
	// Viewport is the initial window size for the browser context.
	Viewport Viewport
	// Timeout is the default action / expect timeout for steps that omit it.
	Timeout time.Duration
}

// Viewport carries the initial window dimensions in pixels.
type Viewport struct {
	Width  int
	Height int
}

// ResolveTarget reads `config.browser.targets[name]` and returns a typed
// Target. When name is empty and exactly one target is declared, that
// single target is returned (single-target shortcut). Otherwise a missing
// name is an error.
func ResolveTarget(config map[string]cty.Value, name string) (Target, error) {
	browser, ok := config["browser"]
	if !ok {
		return Target{}, fmt.Errorf("config.browser is not declared")
	}

	targetsVal, err := readAttr(browser, "targets")
	if err != nil {
		return Target{}, fmt.Errorf("config.browser.targets: %w", err)
	}

	resolved, err := pickTarget(targetsVal, name)
	if err != nil {
		return Target{}, err
	}

	drv, err := resolveDriverConfig(resolved.value)
	if err != nil {
		return Target{}, fmt.Errorf("config.browser.targets.%s.driver: %w", resolved.name, err)
	}

	if drv.Browser != defaultBrowserName {
		return Target{}, fmt.Errorf(`unsupported browser %q; only "chrome" is supported in V1`, drv.Browser)
	}

	return Target{Name: resolved.name, Driver: drv}, nil
}

type resolvedTarget struct {
	name  string
	value cty.Value
}

func pickTarget(targetsVal cty.Value, name string) (resolvedTarget, error) {
	if name != "" {
		val, err := readAttr(targetsVal, name)
		if err != nil {
			return resolvedTarget{}, fmt.Errorf("browser target %q not found", name)
		}

		return resolvedTarget{name: name, value: val}, nil
	}

	keys, err := listKeys(targetsVal)
	if err != nil {
		return resolvedTarget{}, err
	}

	if len(keys) == 0 {
		return resolvedTarget{}, fmt.Errorf("config.browser.targets is empty")
	}

	if len(keys) > 1 {
		return resolvedTarget{}, fmt.Errorf("multiple browser targets declared (%d); step must set target = \"<name>\"", len(keys))
	}

	val, err := readAttr(targetsVal, keys[0])
	if err != nil {
		return resolvedTarget{}, err
	}

	return resolvedTarget{name: keys[0], value: val}, nil
}

func resolveDriverConfig(targetVal cty.Value) (DriverConfig, error) {
	drv := DriverConfig{
		Browser:  defaultBrowserName,
		Headless: defaultHeadless,
		Viewport: Viewport{Width: defaultWindowWidth, Height: defaultWindowHeight},
		Timeout:  defaultTimeout,
	}

	if browser, ok := readOptionalString(targetVal, "browser"); ok {
		drv.Browser = browser
	}

	if headless, ok, err := readOptionalBool(targetVal, "headless"); err != nil {
		return drv, fmt.Errorf("headless: %w", err)
	} else if ok {
		drv.Headless = headless
	}

	if exec, ok := readOptionalString(targetVal, "executable"); ok {
		drv.Executable = exec
	}

	if args, ok, err := readOptionalStringList(targetVal, "args"); err != nil {
		return drv, fmt.Errorf("args: %w", err)
	} else if ok {
		drv.Args = args
	}

	if viewportVal, ok := readOptionalAttr(targetVal, "viewport"); ok && !viewportVal.IsNull() {
		if w, ok, err := readOptionalInt(viewportVal, "width"); err != nil {
			return drv, fmt.Errorf("viewport.width: %w", err)
		} else if ok {
			drv.Viewport.Width = w
		}

		if h, ok, err := readOptionalInt(viewportVal, "height"); err != nil {
			return drv, fmt.Errorf("viewport.height: %w", err)
		} else if ok {
			drv.Viewport.Height = h
		}
	}

	if timeout, ok, err := readOptionalDuration(targetVal, "timeout"); err != nil {
		return drv, fmt.Errorf("timeout: %w", err)
	} else if ok {
		drv.Timeout = timeout
	}

	return drv, nil
}
