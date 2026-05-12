package apple

import (
	"strings"
	"testing"

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
						"project":  cty.StringVal("drivers/apple/TalesAppleDriver/TalesAppleDriver.xcodeproj"),
						"scheme":   cty.StringVal("TalesAppleDriverUITests"),
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

	if target.AppPath != "./build/MyApp.app" || target.BundleID != "com.example.MyApp" {
		t.Fatalf("unexpected target app/bundle: %+v", target)
	}

	if !target.Driver.External || target.Driver.Port != 9080 {
		t.Fatalf("unexpected driver config: %+v", target.Driver)
	}

	if target.Driver.Scheme != "TalesAppleDriverUITests" {
		t.Fatalf("expected scheme to be set, got %+v", target.Driver)
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

	if target.DeviceName != "iPhone 17" || target.BundleID != "com.example.MyApp" {
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
