package lang

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// PKCE method names as exposed in HCL. Comparisons are case-insensitive on
// input but Tales canonicalizes to the spec form ("S256", "plain") internally.
const (
	pkceMethodS256       = "S256"
	pkceMethodPlain      = "plain"
	pkceOptionMethod     = "method"
	pkceOptionsParamName = "options"
)

// PKCEOptions controls pkce_challenge behavior. Zero value means S256.
type PKCEOptions struct {
	Method string
}

// PKCEChallenge derives an RFC 7636 code_challenge from a verifier. S256 is
// the default and recommended path; plain is supported for completeness.
// Errors never embed the verifier.
func PKCEChallenge(verifier string, opts PKCEOptions) (string, error) {
	method := opts.Method
	if method == "" {
		method = pkceMethodS256
	}

	switch strings.ToLower(method) {
	case strings.ToLower(pkceMethodS256):
		// RFC 7636 S256: BASE64URL-ENCODE(SHA256(ASCII(verifier))).
		// The raw 32 hash bytes are encoded — composing this from sha256_hex
		// then base64url_encode would be wrong (it would encode the hex
		// string, not the bytes).
		sum := sha256.Sum256([]byte(verifier))

		return base64.RawURLEncoding.EncodeToString(sum[:]), nil
	case strings.ToLower(pkceMethodPlain):
		return verifier, nil
	default:
		return "", fmt.Errorf("unsupported PKCE method %q; supported methods: S256, plain", method)
	}
}

// pkceChallengeFunc registers pkce_challenge(verifier, options?) in HCL.
func pkceChallengeFunc() function.Function {
	allowed := map[string]struct{}{
		pkceOptionMethod: {},
	}

	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "verifier", Type: cty.String},
		},
		VarParam: &function.Parameter{
			Name:             pkceOptionsParamName,
			Type:             cty.DynamicPseudoType,
			AllowDynamicType: true,
			AllowNull:        true,
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			if len(args) > 2 {
				return cty.NilVal, fmt.Errorf("pkce_challenge: too many arguments")
			}

			opts, err := decodePKCEOptionsArg(args, allowed)
			if err != nil {
				return cty.NilVal, err
			}

			challenge, err := PKCEChallenge(args[0].AsString(), opts)
			if err != nil {
				return cty.NilVal, err
			}

			return cty.StringVal(challenge), nil
		},
	})
}

func decodePKCEOptionsArg(args []cty.Value, allowed map[string]struct{}) (PKCEOptions, error) {
	if len(args) < 2 || args[1].IsNull() {
		return PKCEOptions{}, nil
	}

	optsVal := args[1]
	if !optsVal.Type().IsObjectType() {
		return PKCEOptions{}, fmt.Errorf("pkce_challenge: options must be an object")
	}

	opts := PKCEOptions{}

	for name, attr := range optsVal.AsValueMap() {
		if _, ok := allowed[name]; !ok {
			return PKCEOptions{}, fmt.Errorf("pkce_challenge: unknown option %q", name)
		}

		if attr.IsNull() {
			continue
		}

		if name == pkceOptionMethod {
			if attr.Type() != cty.String {
				return PKCEOptions{}, fmt.Errorf("pkce_challenge: option %q must be a string", name)
			}

			opts.Method = attr.AsString()
		}
	}

	return opts, nil
}
