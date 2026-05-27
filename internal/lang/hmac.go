package lang

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: HMAC-SHA1 is mandated by RFC 6238 TOTP; callers opt in by name
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// hmacHexFunc registers an HMAC variant returning the lowercase hex digest.
// The shape mirrors hashHexFunc — one map entry per algorithm, generic body.
// Errors never embed the secret or message.
func hmacHexFunc(name string, newHash func() hash.Hash) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: paramSecret, Type: cty.String},
			{Name: paramMessage, Type: cty.String},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			mac := hmac.New(newHash, []byte(args[0].AsString()))
			if _, err := mac.Write([]byte(args[1].AsString())); err != nil {
				return cty.NilVal, fmt.Errorf("%s: write failed", name)
			}

			return cty.StringVal(hex.EncodeToString(mac.Sum(nil))), nil
		},
	})
}

// hmacFunctions mirrors hashFunctions — each entry is a thin wrapper around
// hmacHexFunc with a different hash constructor. Adding a variant is one line.
func hmacFunctions() map[string]function.Function {
	return map[string]function.Function{
		"hmac_sha1_hex":       hmacHexFunc("hmac_sha1_hex", sha1.New),
		"hmac_sha224_hex":     hmacHexFunc("hmac_sha224_hex", sha256.New224),
		"hmac_sha256_hex":     hmacHexFunc("hmac_sha256_hex", sha256.New),
		"hmac_sha384_hex":     hmacHexFunc("hmac_sha384_hex", sha512.New384),
		"hmac_sha512_hex":     hmacHexFunc("hmac_sha512_hex", sha512.New),
		"hmac_sha512_224_hex": hmacHexFunc("hmac_sha512_224_hex", sha512.New512_224),
		"hmac_sha512_256_hex": hmacHexFunc("hmac_sha512_256_hex", sha512.New512_256),
	}
}
