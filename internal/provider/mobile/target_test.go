package mobile

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/zclconf/go-cty/cty"
)

func sampleConfig() map[string]cty.Value {
	return map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"iphone": cty.ObjectVal(map[string]cty.Value{
					"platform":    cty.StringVal("ios"),
					"device_name": cty.StringVal("iPhone 16"),
					"app":         cty.StringVal("./build/MyApp.app"),
					"bundle_id":   cty.StringVal("com.example.MyApp"),
					"driver": cty.ObjectVal(map[string]cty.Value{
						"host":     cty.StringVal("127.0.0.1"),
						"port":     cty.NumberIntVal(9080),
						"external": cty.True,
						"mode":     cty.StringVal("xctest"),
					}),
				}),
			}),
		}),
	}
}

func TestResolveTargetFullyPopulated(t *testing.T) {
	t.Parallel()

	target, err := ResolveTarget(sampleConfig(), "iphone")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if target.Platform != "ios" || target.DeviceName != "iPhone 16" {
		t.Fatalf("unexpected target: %+v", target)
	}

	if target.AppPath != "./build/MyApp.app" || target.AppID != "com.example.MyApp" {
		t.Fatalf("unexpected target app/bundle: %+v", target)
	}

	if !target.Driver.External || target.Driver.Port != 9080 {
		t.Fatalf("unexpected driver config: %+v", target.Driver)
	}

	if !target.Driver.PortSet {
		t.Fatalf("expected PortSet true when port is explicit, got %+v", target.Driver)
	}

	if target.Driver.Mode != "xctest" {
		t.Fatalf("expected mode to be set, got %+v", target.Driver)
	}
}

func TestResolveTargetRejectsLegacyProject(t *testing.T) {
	t.Parallel()

	config := map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"iphone": cty.ObjectVal(map[string]cty.Value{
					"platform":    cty.StringVal("ios"),
					"device_name": cty.StringVal("iPhone 16"),
					"app":         cty.StringVal("./MyApp.app"),
					"bundle_id":   cty.StringVal("com.example.MyApp"),
					"driver": cty.ObjectVal(map[string]cty.Value{
						"project": cty.StringVal("drivers/apple/Foo.xcodeproj"),
					}),
				}),
			}),
		}),
	}

	_, err := ResolveTarget(config, "iphone")
	if err == nil || !strings.Contains(err.Error(), `"project" is no longer supported`) {
		t.Fatalf("expected migration error for driver.project, got %v", err)
	}
}

func TestResolveTargetRejectsLegacyScheme(t *testing.T) {
	t.Parallel()

	config := map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"iphone": cty.ObjectVal(map[string]cty.Value{
					"platform":    cty.StringVal("ios"),
					"device_name": cty.StringVal("iPhone 16"),
					"app":         cty.StringVal("./MyApp.app"),
					"bundle_id":   cty.StringVal("com.example.MyApp"),
					"driver": cty.ObjectVal(map[string]cty.Value{
						"scheme": cty.StringVal("FooUITests"),
					}),
				}),
			}),
		}),
	}

	_, err := ResolveTarget(config, "iphone")
	if err == nil || !strings.Contains(err.Error(), `"scheme" is no longer supported`) {
		t.Fatalf("expected migration error for driver.scheme, got %v", err)
	}
}

func TestResolveTargetDefaultsDriverHostPort(t *testing.T) {
	t.Parallel()

	config := map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"iphone": cty.ObjectVal(map[string]cty.Value{
					"platform":    cty.StringVal("ios"),
					"device_name": cty.StringVal("iPhone 16"),
					"app":         cty.StringVal("./MyApp.app"),
					"bundle_id":   cty.StringVal("com.example.MyApp"),
				}),
			}),
		}),
	}

	target, err := ResolveTarget(config, "iphone")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if target.Driver.Host != DefaultDriverHost || target.Driver.Port != DefaultDriverPort {
		t.Fatalf("expected defaults, got %+v", target.Driver)
	}

	if target.Driver.PortSet {
		t.Fatalf("expected PortSet false when port is omitted, got %+v", target.Driver)
	}

	if target.Driver.BaseURL() != "http://127.0.0.1:9080" {
		t.Fatalf("unexpected base URL: %q", target.Driver.BaseURL())
	}
}

func TestResolveTargetSupportsMapTypedConfig(t *testing.T) {
	t.Parallel()

	config := map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.MapVal(map[string]cty.Value{
				"iphone": cty.MapVal(map[string]cty.Value{
					"platform":    cty.StringVal("ios"),
					"device_name": cty.StringVal("iPhone 17"),
					"app":         cty.StringVal("./MyApp.app"),
					"bundle_id":   cty.StringVal("com.example.MyApp"),
				}),
			}),
		}),
	}

	target, err := ResolveTarget(config, "iphone")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if target.DeviceName != "iPhone 17" || target.AppID != "com.example.MyApp" {
		t.Fatalf("unexpected target: %+v", target)
	}
}

func TestResolveTargetMissingTarget(t *testing.T) {
	t.Parallel()

	_, err := ResolveTarget(sampleConfig(), "android-phone")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing-target error, got %v", err)
	}
}

func TestResolveTargetMissingMobile(t *testing.T) {
	t.Parallel()

	_, err := ResolveTarget(map[string]cty.Value{}, "iphone")
	if err == nil || !strings.Contains(err.Error(), "config.mobile") {
		t.Fatalf("expected config.mobile error, got %v", err)
	}
}

func TestResolveTargetCapturesSourcePath(t *testing.T) {
	t.Parallel()

	config := map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"iphone": cty.ObjectVal(map[string]cty.Value{
					"platform":    cty.StringVal("ios"),
					"device_name": cty.StringVal("iPhone 16"),
					"app":         cty.StringVal("./MyApp.app"),
					"bundle_id":   cty.StringVal("com.example.MyApp"),
					"driver": cty.ObjectVal(map[string]cty.Value{
						"source_path": cty.StringVal("./drivers/apple/TalesAppleDriver"),
					}),
				}),
			}),
		}),
	}

	target, err := ResolveTarget(config, "iphone")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if target.Driver.SourcePath != "./drivers/apple/TalesAppleDriver" {
		t.Fatalf("expected SourcePath to be captured, got %+v", target.Driver)
	}
}

func TestResolveTargetRejectsMissingRequiredField(t *testing.T) {
	t.Parallel()

	config := map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"iphone": cty.ObjectVal(map[string]cty.Value{
					"platform":    cty.StringVal("ios"),
					"device_name": cty.StringVal("iPhone 16"),
					"app":         cty.StringVal("./MyApp.app"),
					// bundle_id missing
				}),
			}),
		}),
	}

	_, err := ResolveTarget(config, "iphone")
	if err == nil || !strings.Contains(err.Error(), "bundle_id") {
		t.Fatalf("expected bundle_id error, got %v", err)
	}
}

func TestResolveTargetPrefersAppID(t *testing.T) {
	t.Parallel()

	config := map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"phone": cty.ObjectVal(map[string]cty.Value{
					"platform":    cty.StringVal("android"),
					"device_name": cty.StringVal("tales-e2e"),
					"app":         cty.StringVal("./app.apk"),
					"app_id":      cty.StringVal("com.example.app"),
				}),
			}),
		}),
	}

	target, err := ResolveTarget(config, "phone")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if target.AppID != "com.example.app" {
		t.Fatalf("app_id = %q", target.AppID)
	}
}

func TestResolveTargetAcceptsTheDeprecatedBundleID(t *testing.T) {
	t.Parallel()

	// Existing iOS suites are all written with bundle_id; they must keep
	// working, warning rather than failing.
	config := map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"legacy-phone": cty.ObjectVal(map[string]cty.Value{
					"platform":    cty.StringVal("ios"),
					"device_name": cty.StringVal("iPhone 17"),
					"app":         cty.StringVal("./MyApp.app"),
					"bundle_id":   cty.StringVal("com.example.MyApp"),
				}),
			}),
		}),
	}

	target, err := ResolveTarget(config, "legacy-phone")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if target.AppID != "com.example.MyApp" {
		t.Fatalf("app id = %q", target.AppID)
	}
}

func TestResolveTargetReportsTheCanonicalNameWhenBothAreMissing(t *testing.T) {
	t.Parallel()

	config := map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"phone": cty.ObjectVal(map[string]cty.Value{
					"platform":    cty.StringVal("android"),
					"device_name": cty.StringVal("tales-e2e"),
					"app":         cty.StringVal("./app.apk"),
				}),
			}),
		}),
	}

	_, err := ResolveTarget(config, "phone")
	if err == nil {
		t.Fatal("expected an error when no application identifier is set")
	}

	// Point at app_id, not the deprecated spelling: the error is what an
	// author reads while writing a new target.
	if !strings.Contains(err.Error(), "app_id") {
		t.Fatalf("error should name app_id, got: %v", err)
	}
}

// driverConfigWith builds a config whose driver block carries the given
// extra attributes, so the timeout cases stay readable.
func driverConfigWith(extra map[string]cty.Value) map[string]cty.Value {
	driver := map[string]cty.Value{
		"host": cty.StringVal("127.0.0.1"),
		"port": cty.NumberIntVal(9080),
	}

	maps.Copy(driver, extra)

	return map[string]cty.Value{
		"mobile": cty.ObjectVal(map[string]cty.Value{
			"targets": cty.ObjectVal(map[string]cty.Value{
				"iphone": cty.ObjectVal(map[string]cty.Value{
					"platform":    cty.StringVal("ios"),
					"device_name": cty.StringVal("iPhone 17"),
					"app":         cty.StringVal("./build/MyApp.app"),
					"app_id":      cty.StringVal("com.example.MyApp"),
					"driver":      cty.ObjectVal(driver),
				}),
			}),
		}),
	}
}

func TestResolveTargetReadsDriverTimeout(t *testing.T) {
	t.Parallel()

	config := driverConfigWith(map[string]cty.Value{"timeout": cty.StringVal("2m30s")})

	target, err := ResolveTarget(config, "iphone")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if target.Driver.Timeout != 150*time.Second {
		t.Fatalf("timeout = %v, want 2m30s", target.Driver.Timeout)
	}
}

// An absent timeout must stay zero rather than becoming a default here:
// the driver client owns the default, and baking it in twice would make
// the two drift.
func TestResolveTargetLeavesDriverTimeoutUnsetWhenAbsent(t *testing.T) {
	t.Parallel()

	target, err := ResolveTarget(sampleConfig(), "iphone")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if target.Driver.Timeout != 0 {
		t.Fatalf("timeout = %v, want zero", target.Driver.Timeout)
	}
}

func TestResolveTargetRejectsBadDriverTimeout(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		value cty.Value
		want  string
	}{
		// A bare number is ambiguous (seconds? milliseconds?), so it is
		// rejected instead of guessed at.
		"number":       {value: cty.NumberIntVal(30), want: "duration string"},
		"unparseable":  {value: cty.StringVal("soon"), want: "not a valid duration"},
		"zero":         {value: cty.StringVal("0s"), want: "must be positive"},
		"negative":     {value: cty.StringVal("-5s"), want: "must be positive"},
		"bare integer": {value: cty.StringVal("30"), want: "not a valid duration"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := ResolveTarget(driverConfigWith(map[string]cty.Value{"timeout": tc.value}), "iphone")
			if err == nil {
				t.Fatal("expected an error")
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should contain %q", err, tc.want)
			}
		})
	}
}
