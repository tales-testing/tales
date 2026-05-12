package apple

import (
	"fmt"

	"github.com/zclconf/go-cty/cty"
)

// DefaultDriverHost is used when the target driver block omits "host".
const DefaultDriverHost = "127.0.0.1"

// DefaultDriverPort is used when the target driver block omits "port".
const DefaultDriverPort = 9080

// Target holds the resolved configuration for one mobile target.
type Target struct {
	Name       string
	Platform   string
	DeviceName string
	AppPath    string
	BundleID   string
	Driver     DriverConfig
}

// DriverConfig captures how the mobile provider should talk to the driver.
type DriverConfig struct {
	Host     string
	Port     int
	External bool
	Mode     string
	Project  string
	Scheme   string
}

// BaseURL returns the HTTP base URL the driver client should hit.
func (d DriverConfig) BaseURL() string {
	host := d.Host
	if host == "" {
		host = DefaultDriverHost
	}

	port := d.Port
	if port == 0 {
		port = DefaultDriverPort
	}

	return fmt.Sprintf("http://%s:%d", host, port)
}

// ResolveTarget reads `config.mobile.targets[name]` from the provided config
// values and returns a typed Target. Missing optional fields fall back to
// sensible defaults; missing required fields are reported as errors.
func ResolveTarget(config map[string]cty.Value, name string) (Target, error) {
	mobile, ok := config["mobile"]
	if !ok {
		return Target{}, fmt.Errorf("config.mobile is not declared")
	}

	targetsVal, err := readAttr(mobile, "targets")
	if err != nil {
		return Target{}, fmt.Errorf("config.mobile.targets: %w", err)
	}

	targetVal, err := readAttr(targetsVal, name)
	if err != nil {
		return Target{}, fmt.Errorf("config.mobile.targets.%s: %w", name, err)
	}

	platform, err := readRequiredString(targetVal, "platform")
	if err != nil {
		return Target{}, fmt.Errorf("config.mobile.targets.%s.platform: %w", name, err)
	}

	device, err := readRequiredString(targetVal, "device_name")
	if err != nil {
		return Target{}, fmt.Errorf("config.mobile.targets.%s.device_name: %w", name, err)
	}

	app, err := readRequiredString(targetVal, "app")
	if err != nil {
		return Target{}, fmt.Errorf("config.mobile.targets.%s.app: %w", name, err)
	}

	bundleID, err := readRequiredString(targetVal, "bundle_id")
	if err != nil {
		return Target{}, fmt.Errorf("config.mobile.targets.%s.bundle_id: %w", name, err)
	}

	driver, err := resolveDriverConfig(targetVal)
	if err != nil {
		return Target{}, fmt.Errorf("config.mobile.targets.%s.driver: %w", name, err)
	}

	return Target{
		Name:       name,
		Platform:   platform,
		DeviceName: device,
		AppPath:    app,
		BundleID:   bundleID,
		Driver:     driver,
	}, nil
}

func resolveDriverConfig(targetVal cty.Value) (DriverConfig, error) {
	driver := DriverConfig{Host: DefaultDriverHost, Port: DefaultDriverPort}

	driverVal, ok := readOptionalAttr(targetVal, "driver")
	if !ok {
		return driver, nil
	}

	if driverVal.IsNull() {
		return driver, nil
	}

	if host, ok := readOptionalString(driverVal, "host"); ok {
		driver.Host = host
	}

	if port, ok, err := readOptionalInt(driverVal, "port"); err != nil {
		return driver, fmt.Errorf("port: %w", err)
	} else if ok {
		driver.Port = port
	}

	if external, ok, err := readOptionalBool(driverVal, "external"); err != nil {
		return driver, fmt.Errorf("external: %w", err)
	} else if ok {
		driver.External = external
	}

	if mode, ok := readOptionalString(driverVal, "mode"); ok {
		driver.Mode = mode
	}

	if project, ok := readOptionalString(driverVal, "project"); ok {
		driver.Project = project
	}

	if scheme, ok := readOptionalString(driverVal, "scheme"); ok {
		driver.Scheme = scheme
	}

	return driver, nil
}

func readAttr(value cty.Value, name string) (cty.Value, error) {
	if value.IsNull() || !value.IsKnown() {
		return cty.NilVal, fmt.Errorf("not an object")
	}

	switch {
	case value.Type().IsObjectType():
		if !value.Type().HasAttribute(name) {
			return cty.NilVal, fmt.Errorf("missing %q", name)
		}

		return value.GetAttr(name), nil
	case value.Type().IsMapType():
		key := cty.StringVal(name)

		has := value.HasIndex(key)
		if !has.IsKnown() || has.IsNull() || !has.True() {
			return cty.NilVal, fmt.Errorf("missing %q", name)
		}

		return value.Index(key), nil
	default:
		return cty.NilVal, fmt.Errorf("not an object")
	}
}

func readRequiredString(value cty.Value, name string) (string, error) {
	attr, err := readAttr(value, name)
	if err != nil {
		return "", err
	}

	if attr.IsNull() {
		return "", fmt.Errorf("%q is null", name)
	}

	if attr.Type() != cty.String {
		return "", fmt.Errorf("%q must be a string", name)
	}

	str := attr.AsString()
	if str == "" {
		return "", fmt.Errorf("%q must not be empty", name)
	}

	return str, nil
}

func readOptionalAttr(value cty.Value, name string) (cty.Value, bool) {
	attr, err := readAttr(value, name)
	if err != nil {
		return cty.NilVal, false
	}

	return attr, true
}

func readOptionalString(value cty.Value, name string) (string, bool) {
	attr, ok := readOptionalAttr(value, name)
	if !ok {
		return "", false
	}

	if attr.IsNull() || attr.Type() != cty.String {
		return "", false
	}

	return attr.AsString(), true
}

func readOptionalInt(value cty.Value, name string) (int, bool, error) {
	attr, ok := readOptionalAttr(value, name)
	if !ok {
		return 0, false, nil
	}

	if attr.IsNull() {
		return 0, false, nil
	}

	if attr.Type() != cty.Number {
		return 0, false, fmt.Errorf("%q must be a number", name)
	}

	n, acc := attr.AsBigFloat().Int64()
	if acc != 0 {
		return 0, false, fmt.Errorf("%q must be an integer", name)
	}

	return int(n), true, nil
}

func readOptionalBool(value cty.Value, name string) (bool, bool, error) {
	attr, ok := readOptionalAttr(value, name)
	if !ok {
		return false, false, nil
	}

	if attr.IsNull() {
		return false, false, nil
	}

	if attr.Type() != cty.Bool {
		return false, false, fmt.Errorf("%q must be a bool", name)
	}

	return attr.True(), true, nil
}
