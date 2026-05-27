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
// reuses hashHexFunc so behaviour stays identical across variants — only the
// hash constructor differs.
func hashFunctions() map[string]function.Function {
	return map[string]function.Function{
		"sha1_hex":       hashHexFunc("sha1_hex", sha1.New),
		"sha224_hex":     hashHexFunc("sha224_hex", sha256.New224),
		"sha256_hex":     hashHexFunc("sha256_hex", sha256.New),
		"sha384_hex":     hashHexFunc("sha384_hex", sha512.New384),
		"sha512_hex":     hashHexFunc("sha512_hex", sha512.New),
		"sha512_224_hex": hashHexFunc("sha512_224_hex", sha512.New512_224),
		"sha512_256_hex": hashHexFunc("sha512_256_hex", sha512.New512_256),
	}
}
