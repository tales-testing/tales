package rpc

import (
	"fmt"

	"github.com/zclconf/go-cty/cty"
)

// Protocol identifiers used in target configuration.
const (
	ProtocolConnect = "connect"
	ProtocolGRPC    = "grpc"
)

// Encoding identifiers used in target configuration. Connect supports both;
// gRPC only supports proto on the wire.
const (
	EncodingJSON  = "json"
	EncodingProto = "proto"
)

// DescriptorConfig is the resolved configuration for one descriptor entry
// under config.rpc.descriptors.<name>. Exactly one of Path / Reflection is
// populated.
type DescriptorConfig struct {
	Name       string
	Path       string
	Reflection *ReflectionConfig
}

// ReflectionConfig configures the gRPC reflection v1 loader.
type ReflectionConfig struct {
	Address   string
	Plaintext bool
	TLS       *TLSConfig
	Headers   map[string]string
}

// TLSConfig is the resolved TLS configuration for a target or reflection
// source. CAFile / CertFile / KeyFile are absolute paths (the caller must
// resolve them via expressions / project.dir before reaching this struct).
type TLSConfig struct {
	CAFile             string
	CertFile           string
	KeyFile            string
	ServerName         string
	InsecureSkipVerify bool
}

// TargetConfig is the resolved configuration for one target entry under
// config.rpc.targets.<name>.
type TargetConfig struct {
	Name       string
	Descriptor string
	Protocol   string
	Encoding   string
	BaseURL    string // connect targets
	Address    string // grpc targets
	Plaintext  bool
	TLS        *TLSConfig
	Headers    map[string]string
	Metadata   map[string]string
}

// ResolveDescriptor extracts a descriptor entry by name from the rpc config
// block, validating that exactly one source (path or reflection) is set.
// Errors never include file paths beyond the one the user explicitly set.
func ResolveDescriptor(config map[string]cty.Value, name string) (*DescriptorConfig, error) {
	if name == "" {
		return nil, fmt.Errorf("rpc descriptor name is empty")
	}

	descValue, err := lookupRPCValue(config, "descriptors", name)
	if err != nil {
		return nil, err
	}

	out := &DescriptorConfig{Name: name}

	path, hasPath, err := optionalString(descValue, "path")
	if err != nil {
		return nil, fmt.Errorf("rpc descriptor %q: %w", name, err)
	}

	reflectionValue, hasReflection := lookupAttr(descValue, "reflection")

	switch {
	case hasPath && hasReflection:
		return nil, fmt.Errorf("rpc descriptor %q must define exactly one of path or reflection", name)
	case !hasPath && !hasReflection:
		return nil, fmt.Errorf("rpc descriptor %q must define either path or reflection", name)
	case hasPath:
		if path == "" {
			return nil, fmt.Errorf("rpc descriptor %q has empty path", name)
		}

		out.Path = path
	case hasReflection:
		ref, refErr := resolveReflection(reflectionValue, name)
		if refErr != nil {
			return nil, refErr
		}

		out.Reflection = ref
	}

	return out, nil
}

// ResolveTarget extracts a target entry by name from the rpc config block,
// validating required fields per protocol and applying encoding defaults.
func ResolveTarget(config map[string]cty.Value, name string) (*TargetConfig, error) {
	if name == "" {
		return nil, fmt.Errorf("rpc target name is empty")
	}

	targetValue, err := lookupRPCValue(config, "targets", name)
	if err != nil {
		return nil, err
	}

	descriptor, err := requiredAttrString(targetValue, "descriptor", name, "target")
	if err != nil {
		return nil, err
	}

	protocol, err := requiredAttrString(targetValue, "protocol", name, "target")
	if err != nil {
		return nil, err
	}

	if protocol != ProtocolConnect && protocol != ProtocolGRPC {
		return nil, fmt.Errorf("rpc target %q protocol must be %q or %q, got %q", name, ProtocolConnect, ProtocolGRPC, protocol)
	}

	out := &TargetConfig{Name: name, Descriptor: descriptor, Protocol: protocol}

	if err := applyTargetProtocolFields(targetValue, out); err != nil {
		return nil, err
	}

	if err := applyTargetEncoding(targetValue, out); err != nil {
		return nil, err
	}

	if err := applyTargetTLS(targetValue, out); err != nil {
		return nil, err
	}

	headers, err := optionalStringMap(targetValue, "headers")
	if err != nil {
		return nil, fmt.Errorf("rpc target %q headers: %w", name, err)
	}

	metadata, err := optionalStringMap(targetValue, "metadata")
	if err != nil {
		return nil, fmt.Errorf("rpc target %q metadata: %w", name, err)
	}

	out.Headers = headers
	out.Metadata = metadata

	return out, nil
}

func applyTargetProtocolFields(value cty.Value, out *TargetConfig) error {
	switch out.Protocol {
	case ProtocolConnect:
		baseURL, err := requiredAttrString(value, "base_url", out.Name, "target")
		if err != nil {
			return err
		}

		out.BaseURL = baseURL
	case ProtocolGRPC:
		address, err := requiredAttrString(value, "address", out.Name, "target")
		if err != nil {
			return err
		}

		out.Address = address
	}

	return nil
}

func applyTargetEncoding(value cty.Value, out *TargetConfig) error {
	encoding, _, err := optionalString(value, "encoding")
	if err != nil {
		return fmt.Errorf("rpc target %q encoding: %w", out.Name, err)
	}

	if encoding == "" {
		if out.Protocol == ProtocolConnect {
			out.Encoding = EncodingJSON
		} else {
			out.Encoding = EncodingProto
		}

		return nil
	}

	if encoding != EncodingJSON && encoding != EncodingProto {
		return fmt.Errorf("rpc target %q encoding must be %q or %q, got %q", out.Name, EncodingJSON, EncodingProto, encoding)
	}

	if out.Protocol == ProtocolGRPC && encoding == EncodingJSON {
		return fmt.Errorf("rpc target %q: gRPC protocol does not support encoding %q (gRPC is proto-only)", out.Name, EncodingJSON)
	}

	out.Encoding = encoding

	return nil
}

func applyTargetTLS(value cty.Value, out *TargetConfig) error {
	plaintext, _, err := optionalBool(value, "plaintext")
	if err != nil {
		return fmt.Errorf("rpc target %q plaintext: %w", out.Name, err)
	}

	out.Plaintext = plaintext

	tlsValue, hasTLS := lookupAttr(value, "tls")
	if !hasTLS {
		return nil
	}

	tlsCfg, err := resolveTLSValue(tlsValue, out.Name, "target")
	if err != nil {
		return err
	}

	out.TLS = tlsCfg

	return nil
}

func resolveReflection(value cty.Value, name string) (*ReflectionConfig, error) {
	address, err := requiredAttrString(value, "address", name, "descriptor reflection")
	if err != nil {
		return nil, err
	}

	plaintext, _, err := optionalBool(value, "plaintext")
	if err != nil {
		return nil, fmt.Errorf("rpc descriptor %q reflection plaintext: %w", name, err)
	}

	headers, err := optionalStringMap(value, "headers")
	if err != nil {
		return nil, fmt.Errorf("rpc descriptor %q reflection headers: %w", name, err)
	}

	ref := &ReflectionConfig{Address: address, Plaintext: plaintext, Headers: headers}

	tlsValue, hasTLS := lookupAttr(value, "tls")
	if hasTLS {
		tlsCfg, tlsErr := resolveTLSValue(tlsValue, name, "descriptor reflection")
		if tlsErr != nil {
			return nil, tlsErr
		}

		ref.TLS = tlsCfg
	}

	return ref, nil
}

func resolveTLSValue(value cty.Value, ownerName, ownerKind string) (*TLSConfig, error) {
	out := &TLSConfig{}

	for _, field := range []struct {
		name string
		dst  *string
	}{
		{"ca_file", &out.CAFile},
		{"cert_file", &out.CertFile},
		{"key_file", &out.KeyFile},
		{"server_name", &out.ServerName},
	} {
		got, _, err := optionalString(value, field.name)
		if err != nil {
			return nil, fmt.Errorf("rpc %s %q tls.%s: %w", ownerKind, ownerName, field.name, err)
		}

		*field.dst = got
	}

	skip, _, err := optionalBool(value, "insecure_skip_verify")
	if err != nil {
		return nil, fmt.Errorf("rpc %s %q tls.insecure_skip_verify: %w", ownerKind, ownerName, err)
	}

	out.InsecureSkipVerify = skip

	if (out.CertFile == "") != (out.KeyFile == "") {
		return nil, fmt.Errorf("rpc %s %q: tls.cert_file and tls.key_file must be set together", ownerKind, ownerName)
	}

	return out, nil
}

// lookupRPCValue navigates config.rpc.<section>.<name> with descriptive
// errors that never leak attribute values.
func lookupRPCValue(config map[string]cty.Value, section, name string) (cty.Value, error) {
	rpcBlock, ok := config["rpc"]
	if !ok || rpcBlock.IsNull() || !rpcBlock.IsKnown() {
		return cty.NilVal, fmt.Errorf("rpc %s %q not found: config.rpc is not defined", section, name)
	}

	if !isObjectish(rpcBlock) {
		return cty.NilVal, fmt.Errorf("rpc %s %q not found: config.rpc must be an object", section, name)
	}

	sectionValue, ok := indexCty(rpcBlock, section)
	if !ok || sectionValue.IsNull() || !sectionValue.IsKnown() {
		return cty.NilVal, fmt.Errorf("rpc %s %q not found: config.rpc.%s is not defined", section, name, section)
	}

	if !isObjectish(sectionValue) {
		return cty.NilVal, fmt.Errorf("rpc %s %q not found: config.rpc.%s must be an object", section, name, section)
	}

	value, ok := indexCty(sectionValue, name)
	if !ok || value.IsNull() || !value.IsKnown() {
		return cty.NilVal, fmt.Errorf("rpc %s %q not found in config.rpc.%s", section, name, section)
	}

	if !isObjectish(value) {
		return cty.NilVal, fmt.Errorf("rpc %s %q must be an object", section, name)
	}

	return value, nil
}

func lookupAttr(value cty.Value, key string) (cty.Value, bool) {
	got, ok := indexCty(value, key)
	if !ok {
		return cty.NilVal, false
	}

	if got.IsNull() || !got.IsKnown() {
		return cty.NilVal, false
	}

	return got, true
}

func indexCty(value cty.Value, key string) (cty.Value, bool) {
	if !value.IsKnown() || value.IsNull() {
		return cty.NilVal, false
	}

	typ := value.Type()

	switch {
	case typ.IsObjectType():
		if !typ.HasAttribute(key) {
			return cty.NilVal, false
		}

		return value.GetAttr(key), true
	case typ.IsMapType():
		idx := cty.StringVal(key)
		if !value.HasIndex(idx).True() {
			return cty.NilVal, false
		}

		return value.Index(idx), true
	default:
		return cty.NilVal, false
	}
}

func isObjectish(value cty.Value) bool {
	typ := value.Type()

	return typ.IsObjectType() || typ.IsMapType()
}

func optionalString(value cty.Value, key string) (string, bool, error) {
	got, ok := lookupAttr(value, key)
	if !ok {
		return "", false, nil
	}

	if got.Type() != cty.String {
		return "", true, fmt.Errorf("%s must be a string", key)
	}

	return got.AsString(), true, nil
}

func optionalBool(value cty.Value, key string) (bool, bool, error) {
	got, ok := lookupAttr(value, key)
	if !ok {
		return false, false, nil
	}

	if got.Type() != cty.Bool {
		return false, true, fmt.Errorf("%s must be a boolean", key)
	}

	return got.True(), true, nil
}

func optionalStringMap(value cty.Value, key string) (map[string]string, error) {
	got, ok := lookupAttr(value, key)
	if !ok {
		return nil, nil
	}

	typ := got.Type()
	if !typ.IsObjectType() && !typ.IsMapType() {
		return nil, fmt.Errorf("%s must be an object", key)
	}

	out := map[string]string{}

	for k, v := range got.AsValueMap() {
		if v.IsNull() || !v.IsKnown() {
			continue
		}

		if v.Type() != cty.String {
			return nil, fmt.Errorf("%s.%s must be a string", key, k)
		}

		out[k] = v.AsString()
	}

	return out, nil
}

func requiredAttrString(value cty.Value, key, ownerName, ownerKind string) (string, error) {
	got, has, err := optionalString(value, key)
	if err != nil {
		return "", fmt.Errorf("rpc %s %q %s: %w", ownerKind, ownerName, key, err)
	}

	if !has || got == "" {
		return "", fmt.Errorf("rpc %s %q has empty %s", ownerKind, ownerName, key)
	}

	return got, nil
}
