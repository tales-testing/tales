// Package file implements the file provider (step "file"), which inspects a
// file produced earlier in a scenario — existence, size, hex digests, UTF-8
// text and parsed JSON — and exposes them under the file.* namespace for
// assertions and capture. The provider is stateless and safe to share across
// parallel scenarios; the runtime resolves the path and decides which reads
// are required before calling Execute.
package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

const providerType = "file"

const (
	attrPath      = "path"
	attrExists    = "exists"
	attrSizeBytes = "size_bytes"
	attrText      = "text"
	attrJSON      = "json"
)

// Provider executes file steps. It holds no state.
type Provider struct{}

// New creates a file provider.
func New() *Provider {
	return &Provider{}
}

// Type returns the provider type.
func (p *Provider) Type() string {
	return providerType
}

// Execute inspects the file at input.File.Path. Existence and size are always
// reported; hashes / text / json are read only when the step needs them, so a
// missing file (exists = false) or an unreadable form fails only when an
// assertion or capture actually depends on it.
func (p *Provider) Execute(_ context.Context, input provider.Input) (*provider.Output, error) {
	fe := input.File
	if fe == nil {
		return nil, errors.New("file step is missing execution data")
	}

	attrs := baseAttrs(fe.Path)

	info, statErr := os.Stat(fe.Path)
	exists := statErr == nil && !info.IsDir()
	attrs[attrExists] = cty.BoolVal(exists)

	if !exists {
		if err := requireExisting(fe, statErr, info); err != nil {
			return nil, err
		}

		return output(attrs), nil
	}

	attrs[attrSizeBytes] = cty.NumberIntVal(info.Size())

	if !fe.NeedHash && !fe.NeedText && !fe.NeedJSON {
		return output(attrs), nil
	}

	data, err := os.ReadFile(fe.Path)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", fe.Path, err)
	}

	if err := populateContentAttrs(attrs, fe, data); err != nil {
		return nil, err
	}

	return output(attrs), nil
}

// baseAttrs seeds the file.* object with every key present so expressions can
// reference any attribute without an "unknown attribute" error. Unread fields
// stay null; size defaults to 0 and exists is set by the caller.
func baseAttrs(path string) map[string]cty.Value {
	attrs := map[string]cty.Value{
		attrPath:      cty.StringVal(path),
		attrExists:    cty.False,
		attrSizeBytes: cty.NumberIntVal(0),
		attrText:      cty.NullVal(cty.String),
		attrJSON:      cty.NullVal(cty.DynamicPseudoType),
	}

	for _, algo := range lang.HashAlgorithms() {
		attrs[algo] = cty.NullVal(cty.String)
	}

	return attrs
}

// requireExisting returns an error when the file is absent (or a directory)
// but the step needs to read it. A pure existence check (no Need* flag) is not
// an error: expect { exists = false } must pass.
func requireExisting(fe *provider.FileExecution, statErr error, info os.FileInfo) error {
	if !fe.NeedSize && !fe.NeedHash && !fe.NeedText && !fe.NeedJSON {
		return nil
	}

	if statErr == nil && info != nil && info.IsDir() {
		return fmt.Errorf("path %q is a directory, not a file", fe.Path)
	}

	return fmt.Errorf("file %q does not exist", fe.Path)
}

// populateContentAttrs fills the hash / text / json attributes from data
// according to the Need* flags, failing clearly when a required form is
// invalid (non-UTF-8 text, malformed JSON).
func populateContentAttrs(attrs map[string]cty.Value, fe *provider.FileExecution, data []byte) error {
	if fe.NeedHash {
		for _, algo := range lang.HashAlgorithms() {
			digest, err := lang.HashHex(algo, data)
			if err != nil {
				return fmt.Errorf("hash file: %w", err)
			}

			attrs[algo] = cty.StringVal(digest)
		}
	}

	if fe.NeedText {
		if !utf8.Valid(data) {
			return fmt.Errorf("file %q is not valid UTF-8 text", fe.Path)
		}

		attrs[attrText] = cty.StringVal(string(data))
	}

	if fe.NeedJSON {
		value, err := decodeJSON(data)
		if err != nil {
			return fmt.Errorf("file %q is not valid JSON: %w", fe.Path, err)
		}

		attrs[attrJSON] = value
	}

	return nil
}

// decodeJSON parses data into a cty value, mirroring the HTTP provider's JSON
// handling so file.json and response.json behave identically.
func decodeJSON(data []byte) (cty.Value, error) {
	inputType, err := ctyjson.ImpliedType(data)
	if err != nil {
		return cty.NilVal, fmt.Errorf("imply type: %w", err)
	}

	value, err := ctyjson.Unmarshal(data, inputType)
	if err != nil {
		return cty.NilVal, fmt.Errorf("unmarshal: %w", err)
	}

	return value, nil
}

func output(attrs map[string]cty.Value) *provider.Output {
	return &provider.Output{
		Request:  map[string]cty.Value{attrPath: attrs[attrPath]},
		Response: attrs,
	}
}
