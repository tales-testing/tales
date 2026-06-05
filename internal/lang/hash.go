package lang

import (
	"crypto/sha1" //nolint:gosec // G505: sha1_hex is exposed as a named primitive on user demand; callers opt in by name
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// hashConstructors is the single source of truth for the digest algorithms
// Tales exposes. The keys are the short algorithm names used by the file /
// download / exec surfaces (sha256, sha512, …); the cty `*_hex` functions and
// the exported HashHex helper both range over this map, so adding a variant is
// one entry. New224 / New512_224 / New512_256 cover the truncated SHA-2 forms.
var hashConstructors = map[string]func() hash.Hash{
	"sha1":       sha1.New,
	"sha224":     sha256.New224,
	"sha256":     sha256.New,
	"sha384":     sha512.New384,
	"sha512":     sha512.New,
	"sha512_224": sha512.New512_224,
	"sha512_256": sha512.New512_256,
}

// HashHex returns the lowercase hex digest of data for the named algorithm
// (sha1, sha224, sha256, sha384, sha512, sha512_224, sha512_256). It shares
// hashConstructors with the cty `*_hex` functions so the byte-level helper
// (used by save / file / exec) and the expression functions can never drift.
// The error never embeds data.
func HashHex(algo string, data []byte) (string, error) {
	newHash, ok := hashConstructors[algo]
	if !ok {
		return "", fmt.Errorf("unknown hash algorithm %q", algo)
	}

	h := newHash()
	if _, err := h.Write(data); err != nil {
		return "", fmt.Errorf("hash %s: write failed", algo)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashHexFunc registers a single-argument hash function returning the
// lowercase hex digest. The hash constructor is closed over by the caller, so
// adding a new variant is one map entry. Errors never embed the input.
func hashHexFunc(name string, newHash func() hash.Hash) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "value", Type: cty.String},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			h := newHash()
			if _, err := h.Write([]byte(args[0].AsString())); err != nil {
				return cty.NilVal, fmt.Errorf("%s: write failed", name)
			}

			return cty.StringVal(hex.EncodeToString(h.Sum(nil))), nil
		},
	})
}

// hashFunctions returns the full hash-function registration map. Each entry
// reuses hashHexFunc over hashConstructors so behavior stays identical across
// variants — only the hash constructor differs. The cty function name is the
// algorithm key suffixed with "_hex".
func hashFunctions() map[string]function.Function {
	functions := make(map[string]function.Function, len(hashConstructors))
	for algo, newHash := range hashConstructors {
		name := algo + "_hex"
		functions[name] = hashHexFunc(name, newHash)
	}

	return functions
}
