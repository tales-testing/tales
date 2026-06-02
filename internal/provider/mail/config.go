package mail

import (
	"fmt"
	"time"

	"github.com/zclconf/go-cty/cty"
)

const (
	protocolSMTP = "smtp"
	protocolLMTP = "lmtp"

	networkTCP  = "tcp"
	networkUnix = "unix"

	defaultTimeout = 10 * time.Second
)

// Target is the resolved configuration for one mail target declared under
// config.mail.targets.<name>.
type Target struct {
	Name     string
	Protocol string // smtp | lmtp

	// SMTP fields.
	Host               string
	Port               int
	TLS                bool
	StartTLS           bool
	Username           string
	Password           string
	InsecureSkipVerify bool

	// LMTP fields.
	Network string // tcp | unix
	Address string

	// Common.
	Timeout time.Duration
}

// resolveTarget extracts and validates the configuration for a named entry
// under config.mail.targets.<name>. Errors are descriptive but never echo the
// SMTP password.
func resolveTarget(config map[string]cty.Value, name string) (Target, error) {
	if name == "" {
		return Target{}, fmt.Errorf("mail step requires a non-empty target name")
	}

	targetValue, err := lookupTargetValue(config, name)
	if err != nil {
		return Target{}, err
	}

	protocol := stringAttr(targetValue, "protocol")

	target := Target{
		Name:               name,
		Protocol:           protocol,
		Timeout:            resolveTimeout(targetValue),
		TLS:                boolAttr(targetValue, "tls"),
		StartTLS:           boolAttr(targetValue, "starttls"),
		InsecureSkipVerify: boolAttr(targetValue, "insecure_skip_verify"),
	}

	switch protocol {
	case protocolSMTP:
		if err := resolveSMTPTarget(targetValue, &target); err != nil {
			return Target{}, err
		}
	case protocolLMTP:
		if err := resolveLMTPTarget(targetValue, &target); err != nil {
			return Target{}, err
		}
	default:
		return Target{}, fmt.Errorf("unsupported mail protocol %q; supported protocols: smtp, lmtp", protocol)
	}

	if target.TLS && target.StartTLS {
		return Target{}, fmt.Errorf("mail target %q cannot enable both tls and starttls", name)
	}

	return target, nil
}

func resolveSMTPTarget(targetValue cty.Value, target *Target) error {
	target.Host = stringAttr(targetValue, "host")
	if target.Host == "" {
		return fmt.Errorf("mail target %q has empty SMTP host", target.Name)
	}

	port, ok := intAttr(targetValue, "port")
	if !ok || port <= 0 {
		return fmt.Errorf("mail target %q has empty SMTP port", target.Name)
	}

	target.Port = port

	if auth, ok := objectAttr(targetValue, "auth"); ok {
		target.Username = stringAttr(auth, "username")
		target.Password = stringAttr(auth, "password")
	}

	return nil
}

func resolveLMTPTarget(targetValue cty.Value, target *Target) error {
	network := stringAttr(targetValue, "network")

	switch network {
	case networkTCP, networkUnix:
		target.Network = network
	case "":
		return fmt.Errorf("mail target %q has empty LMTP network", target.Name)
	default:
		return fmt.Errorf("mail target %q has unsupported LMTP network %q", target.Name, network)
	}

	target.Address = stringAttr(targetValue, "address")
	if target.Address == "" {
		return fmt.Errorf("mail target %q has empty LMTP address", target.Name)
	}

	return nil
}

func resolveTimeout(targetValue cty.Value) time.Duration {
	raw := stringAttr(targetValue, "timeout")
	if raw == "" {
		return defaultTimeout
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return defaultTimeout
	}

	return parsed
}

// lookupTargetValue navigates config.mail.targets.<name> and returns the
// entry, or a descriptive not-found error.
func lookupTargetValue(config map[string]cty.Value, name string) (cty.Value, error) {
	mailBlock, ok := config["mail"]
	if !ok || mailBlock.IsNull() || !mailBlock.IsKnown() {
		return cty.NilVal, fmt.Errorf("mail target %q not found: config.mail is not defined", name)
	}

	targets, ok := indexValue(mailBlock, "targets")
	if !ok || targets.IsNull() || !targets.IsKnown() {
		return cty.NilVal, fmt.Errorf("mail target %q not found: config.mail.targets is empty", name)
	}

	target, ok := indexValue(targets, name)
	if !ok || target.IsNull() || !target.IsKnown() {
		return cty.NilVal, fmt.Errorf("mail target %q not found", name)
	}

	return target, nil
}

// indexValue reads an attribute or map key from a cty value, supporting both
// object and map types so config consumers do not special-case the schema.
func indexValue(value cty.Value, key string) (cty.Value, bool) {
	if value.IsNull() || !value.IsKnown() {
		return cty.NilVal, false
	}

	switch {
	case value.Type().IsObjectType():
		if !value.Type().HasAttribute(key) {
			return cty.NilVal, false
		}

		return value.GetAttr(key), true
	case value.Type().IsMapType():
		index := cty.StringVal(key)
		if !value.HasIndex(index).True() {
			return cty.NilVal, false
		}

		return value.Index(index), true
	default:
		return cty.NilVal, false
	}
}

func stringAttr(value cty.Value, key string) string {
	attr, ok := indexValue(value, key)
	if !ok || attr.IsNull() || !attr.IsKnown() || attr.Type() != cty.String {
		return ""
	}

	return attr.AsString()
}

func boolAttr(value cty.Value, key string) bool {
	attr, ok := indexValue(value, key)
	if !ok || attr.IsNull() || !attr.IsKnown() || attr.Type() != cty.Bool {
		return false
	}

	return attr.True()
}

func intAttr(value cty.Value, key string) (int, bool) {
	attr, ok := indexValue(value, key)
	if !ok || attr.IsNull() || !attr.IsKnown() || attr.Type() != cty.Number {
		return 0, false
	}

	bf := attr.AsBigFloat()
	if !bf.IsInt() {
		// A non-integer port (e.g. 2525.9) is invalid; reject it rather than
		// silently truncating.
		return 0, false
	}

	i, _ := bf.Int64()

	return int(i), true
}

func objectAttr(value cty.Value, key string) (cty.Value, bool) {
	attr, ok := indexValue(value, key)
	if !ok || attr.IsNull() || !attr.IsKnown() {
		return cty.NilVal, false
	}

	if !attr.Type().IsObjectType() && !attr.Type().IsMapType() {
		return cty.NilVal, false
	}

	return attr, true
}
