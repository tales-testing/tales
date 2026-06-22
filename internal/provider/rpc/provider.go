// Package rpc is the dynamic ConnectRPC + gRPC provider for Tales. It loads
// Protobuf descriptors at runtime (file or reflection), encodes request
// messages from cty values via the codec subpackage, dispatches the call
// through the appropriate transport (Connect or gRPC), decodes the response
// back to cty, and writes per-step artifacts (request.json, response.json,
// metadata.json) under the scenario workspace. No code generation is
// involved at any layer.
package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/rpc/codec"
	"github.com/tales-testing/tales/internal/provider/rpc/descriptor"
	"github.com/tales-testing/tales/internal/provider/rpc/transport"

	"github.com/zclconf/go-cty/cty"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// providerType is the step provider label that triggers rpc execution.
const providerType = "rpc"

// Response key names exposed to the user via response.<key> assertions.
// Pulled out as constants so the provider entry point and the metadata
// artifact writer always agree on the spelling.
const (
	keyStatus     = "status"
	keyCode       = "code"
	keyMessage    = "message"
	keyError      = "error"
	keyHeaders    = "headers"
	keyMetadata   = "metadata"
	keyTrailers   = "trailers"
	keyDurationMs = "duration_ms"
	keyRaw        = "raw"

	keyTarget     = "target"
	keyProtocol   = "protocol"
	keyEncoding   = "encoding"
	keyService    = "service"
	keyMethod     = "method"
	keyFullMethod = "full_method"
	keyJSON       = "json"
)

// Provider implements provider.Provider for the rpc step. One instance is
// registered at process startup; concurrent scenarios share its descriptor
// cache and per-target transport cache through sync primitives. Close()
// releases every transport at suite end.
type Provider struct {
	descriptors *descriptor.Registry

	mu         sync.Mutex
	transports map[string]transport.Transport // keyed by target name
	types      map[string]*protoregistry.Types
}

// Option mutates a Provider during construction. V1 has no options; the
// signature is kept for forward compatibility with the existing
// provider.New(...Option) pattern used by other providers.
type Option func(*Provider)

// New builds an empty Provider with fresh caches.
func New(opts ...Option) *Provider {
	p := &Provider{
		descriptors: descriptor.NewRegistry(),
		transports:  map[string]transport.Transport{},
		types:       map[string]*protoregistry.Types{},
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Type returns the step provider label, matching the literal users write in
// step "rpc" "...".
func (*Provider) Type() string {
	return providerType
}

// Execute performs one unary RPC call. It resolves the target + descriptor
// configuration from input.Config, loads (and caches) the Protobuf
// descriptor set, encodes the request message from the supplied cty map,
// picks the matching transport, decodes the response, and writes artifacts
// to disk if input.RPC.ArtifactsDir is set. Transport-level failures return
// an error; protocol-level errors (a non-OK gRPC code, a Connect error
// envelope) populate output.Response.error so the user can assert on them.
func (p *Provider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	if input.RPC == nil {
		return nil, errors.New("rpc provider: input.RPC is nil")
	}

	targetCfg, err := ResolveTarget(input.Config, input.RPC.Target)
	if err != nil {
		return nil, err
	}

	descCfg, err := ResolveDescriptor(input.Config, targetCfg.Descriptor)
	if err != nil {
		return nil, err
	}

	resolveProjectPaths(input.RPC.ProjectDir, targetCfg, descCfg)

	files, err := p.loadDescriptor(ctx, descCfg)
	if err != nil {
		return nil, err
	}

	types, err := p.typesFor(descCfg.Name, files)
	if err != nil {
		return nil, err
	}

	resolved, err := descriptor.Resolve(files, descCfg.Name, input.RPC.Service, input.RPC.Method)
	if err != nil {
		return nil, fmt.Errorf("rpc %s/%s: %w", input.RPC.Service, input.RPC.Method, err)
	}

	reqMsg, reqJSON, err := codec.EncodeMessage(resolved.Input, ctyObjectFromMap(input.RPC.Message), types)
	if err != nil {
		return nil, fmt.Errorf("rpc encode request: %w", err)
	}

	_, reqValue, err := codec.DecodeMessage(reqMsg, types)
	if err != nil {
		return nil, fmt.Errorf("rpc decode request: %w", err)
	}

	trans, err := p.transportFor(targetCfg, types)
	if err != nil {
		return nil, err
	}

	timeout := input.Timeout
	if timeout == 0 {
		timeout = input.RPC.Timeout
	}

	call := transport.Call{
		FullMethod: resolved.FullMethod,
		Service:    input.RPC.Service,
		Method:     input.RPC.Method,
		Output:     resolved.Output,
		Request:    reqMsg,
		Types:      types,
		Headers:    mergeStringMaps(targetCfg.Headers, input.RPC.HeadersOverride),
		Metadata:   mergeStringMaps(targetCfg.Metadata, input.RPC.MetadataOverride),
		Timeout:    timeout,
	}

	result, err := trans.Invoke(ctx, call)
	if err != nil {
		return nil, fmt.Errorf("rpc transport invoke: %w", err)
	}

	respJSON, respValue, err := codec.DecodeMessage(result.Message, types)
	if err != nil {
		return nil, fmt.Errorf("rpc decode response: %w", err)
	}

	output := buildOutput(targetCfg, resolved, result, reqJSON, reqValue, respJSON, respValue)

	if input.RPC.ArtifactsDir != "" {
		writeArtifacts(input.RPC.ArtifactsDir, targetCfg, resolved, result, reqJSON, respJSON)
	}

	return output, nil
}

func resolveProjectPaths(projectDir string, target *TargetConfig, desc *DescriptorConfig) {
	if desc != nil {
		desc.Path = resolveProjectPath(projectDir, desc.Path)
		if desc.Reflection != nil {
			resolveTLSPaths(projectDir, desc.Reflection.TLS)
		}
	}

	if target != nil {
		resolveTLSPaths(projectDir, target.TLS)
	}
}

func resolveTLSPaths(projectDir string, cfg *TLSConfig) {
	if cfg == nil {
		return
	}

	cfg.CAFile = resolveProjectPath(projectDir, cfg.CAFile)
	cfg.CertFile = resolveProjectPath(projectDir, cfg.CertFile)
	cfg.KeyFile = resolveProjectPath(projectDir, cfg.KeyFile)
}

func resolveProjectPath(projectDir, path string) string {
	if path == "" || filepath.IsAbs(path) || projectDir == "" {
		return path
	}

	return filepath.Join(projectDir, path)
}

// Close releases every cached transport (and through it any open gRPC
// connection or idle HTTP transport). Safe to call multiple times.
func (p *Provider) Close() error {
	p.mu.Lock()
	transports := p.transports
	p.transports = map[string]transport.Transport{}
	p.mu.Unlock()

	var firstErr error

	for _, tr := range transports {
		if err := tr.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (p *Provider) loadDescriptor(ctx context.Context, cfg *DescriptorConfig) (*protoregistry.Files, error) {
	var loader descriptor.Loader

	switch {
	case cfg.Path != "":
		loader = &descriptor.FileLoader{Path: cfg.Path}
	case cfg.Reflection != nil:
		tlsCfg, err := BuildTLSConfig(cfg.Reflection.TLS)
		if err != nil {
			return nil, fmt.Errorf("rpc descriptor %q reflection tls: %w", cfg.Name, err)
		}

		loader = &descriptor.ReflectionLoader{
			Address:   cfg.Reflection.Address,
			Plaintext: cfg.Reflection.Plaintext,
			TLS:       tlsCfg,
			Headers:   cfg.Reflection.Headers,
		}
	default:
		return nil, fmt.Errorf("rpc descriptor %q has no source", cfg.Name)
	}

	files, err := p.descriptors.Get(ctx, cfg.Name, loader)
	if err != nil {
		return nil, fmt.Errorf("rpc descriptor load: %w", err)
	}

	return files, nil
}

func (p *Provider) typesFor(name string, files *protoregistry.Files) (*protoregistry.Types, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if t, ok := p.types[name]; ok {
		return t, nil
	}

	t, err := descriptor.BuildTypes(files)
	if err != nil {
		return nil, fmt.Errorf("rpc descriptor %q: build types: %w", name, err)
	}

	p.types[name] = t

	return t, nil
}

func (p *Provider) transportFor(target *TargetConfig, types *protoregistry.Types) (transport.Transport, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, ok := p.transports[target.Name]; ok {
		return existing, nil
	}

	tr, err := buildTransport(target, types)
	if err != nil {
		return nil, err
	}

	p.transports[target.Name] = tr

	return tr, nil
}

func buildTransport(target *TargetConfig, types *protoregistry.Types) (transport.Transport, error) {
	tlsCfg, err := BuildTLSConfig(target.TLS)
	if err != nil {
		return nil, fmt.Errorf("rpc target %q tls: %w", target.Name, err)
	}

	switch target.Protocol {
	case ProtocolConnect:
		return transport.NewConnectClient(transport.ConnectConfig{
			BaseURL:        target.BaseURL,
			Encoding:       target.Encoding,
			TLS:            tlsCfg,
			DefaultHeaders: target.Headers,
			Types:          types,
		}), nil
	case ProtocolGRPC:
		return transport.NewGRPCClient(transport.GRPCConfig{
			Address:         target.Address,
			Plaintext:       target.Plaintext,
			TLS:             tlsCfg,
			DefaultMetadata: target.Metadata,
		}), nil
	default:
		return nil, fmt.Errorf("rpc target %q: unsupported protocol %q", target.Name, target.Protocol)
	}
}

func buildOutput(target *TargetConfig, resolved *descriptor.Resolved, result *transport.Result, reqJSON []byte, reqValue cty.Value, respJSON []byte, respValue cty.Value) *provider.Output {
	response := map[string]cty.Value{
		keyStatus:     cty.StringVal(result.Status),
		keyCode:       cty.NumberIntVal(int64(result.Code)),
		keyMessage:    respValue,
		keyError:      errorToCty(result.Error),
		keyHeaders:    headersToCty(result.Headers),
		keyMetadata:   headersToCty(result.Metadata),
		keyTrailers:   headersToCty(result.Trailers),
		keyDurationMs: cty.NumberIntVal(result.Duration.Milliseconds()),
		keyRaw:        cty.StringVal(string(respJSON)),
	}

	request := map[string]cty.Value{
		keyTarget:   cty.StringVal(target.Name),
		keyProtocol: cty.StringVal(target.Protocol),
		keyEncoding: cty.StringVal(target.Encoding),
		keyService:  cty.StringVal(string(resolved.Service.FullName())),
		keyMethod:   cty.StringVal(string(resolved.Method.Name())),
		keyMessage:  reqValue,
		keyJSON:     cty.StringVal(string(reqJSON)),
	}

	return &provider.Output{
		Request:  request,
		Response: response,
		Duration: result.Duration,
	}
}

func writeArtifacts(dir string, target *TargetConfig, resolved *descriptor.Resolved, result *transport.Result, reqJSON, respJSON []byte) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return
	}

	_ = os.WriteFile(filepath.Join(dir, "request.json"), reqJSON, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "response.json"), respJSON, 0o600)

	meta := map[string]any{
		keyTarget:     target.Name,
		keyProtocol:   target.Protocol,
		keyEncoding:   target.Encoding,
		keyService:    string(resolved.Service.FullName()),
		keyMethod:     string(resolved.Method.Name()),
		keyFullMethod: resolved.FullMethod,
		keyStatus:     result.Status,
		keyCode:       result.Code,
		keyDurationMs: result.Duration.Milliseconds(),
		keyHeaders:    result.Headers,
		keyMetadata:   result.Metadata,
		keyTrailers:   result.Trailers,
	}

	if result.Error != nil {
		meta[keyError] = map[string]any{
			keyCode:    result.Error.Code,
			keyMessage: result.Error.Message,
		}
	}

	bytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}

	_ = os.WriteFile(filepath.Join(dir, "metadata.json"), bytes, 0o600)
}

// errorToCty converts an *ErrorDetail into a cty value the user can assert
// on via expect { error = {...} }. nil collapses to a null object so
// response.error == null when the call succeeded.
func errorToCty(detail *transport.ErrorDetail) cty.Value {
	if detail == nil {
		return cty.NullVal(cty.Object(map[string]cty.Type{
			keyCode:    cty.String,
			keyMessage: cty.String,
		}))
	}

	out := map[string]cty.Value{
		keyCode:    cty.StringVal(detail.Code),
		keyMessage: cty.StringVal(detail.Message),
	}

	if len(detail.Details) > 0 {
		items := make([]cty.Value, len(detail.Details))

		for i, d := range detail.Details {
			items[i] = cty.StringVal(d.Type)
		}

		out["details"] = cty.ListVal(items)
	}

	return cty.ObjectVal(out)
}

// headersToCty turns the masked map[string][]string into a cty object so the
// user can index it as response.headers["x-foo"][0]. nil collapses to an
// empty object.
func headersToCty(headers map[string][]string) cty.Value {
	if len(headers) == 0 {
		return cty.EmptyObjectVal
	}

	attrs := make(map[string]cty.Value, len(headers))

	for k, values := range headers {
		items := make([]cty.Value, len(values))
		for i, v := range values {
			items[i] = cty.StringVal(v)
		}

		if len(items) == 0 {
			attrs[k] = cty.ListValEmpty(cty.String)

			continue
		}

		attrs[k] = cty.ListVal(items)
	}

	return cty.ObjectVal(attrs)
}

// ctyObjectFromMap turns an HCL-decoded request map into a cty object, or
// returns the empty object when nil.
func ctyObjectFromMap(m map[string]cty.Value) cty.Value {
	if m == nil {
		return cty.EmptyObjectVal
	}

	return cty.ObjectVal(m)
}

// mergeStringMaps returns the union of two string maps, with override
// winning on conflict. Both inputs are read-only.
func mergeStringMaps(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}

	out := make(map[string]string, len(base)+len(override))

	for k, v := range base {
		out[k] = v
	}

	for k, v := range override {
		out[k] = v
	}

	return out
}
