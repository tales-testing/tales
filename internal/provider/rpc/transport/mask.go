package transport

import (
	"net/textproto"
	"strings"
)

// maskValue is what every secret-bearing header / metadata value is
// rewritten to before it reaches a report or artifact.
const maskValue = "***"

// sensitiveExactNames are headers / metadata keys whose presence is by
// itself a sufficient signal that the value is a secret. Matched
// case-insensitively against the canonical MIME header form.
//
//nolint:gochecknoglobals // immutable lookup table; effectively a const.
var sensitiveExactNames = map[string]struct{}{
	"Authorization":       {},
	"Cookie":              {},
	"Set-Cookie":          {},
	"Proxy-Authorization": {},
	"X-Api-Key":           {},
}

// sensitiveSubstrings are matched against the lowercased key name; a header
// is masked when its name contains any of these. Catches custom names like
// "X-Service-Token" or "Internal-Password".
//
//nolint:gochecknoglobals // immutable lookup table; effectively a const.
var sensitiveSubstrings = []string{
	"token",
	"secret",
	"password",
	"api-key",
	"api_key",
	"apikey",
	"private",
}

// MaskHeaders returns a copy of in where every value of a header whose name
// matches the sensitive list is replaced with "***". Header names are
// canonicalised via textproto.CanonicalMIMEHeaderKey so callers receive a
// stable representation regardless of the source casing.
//
// Returning a copy means the original map (typically the inbound
// http.Header) is left untouched so subsequent network reads or other
// observers still see the real values.
func MaskHeaders(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}

	out := make(map[string][]string, len(in))

	for raw, values := range in {
		key := textproto.CanonicalMIMEHeaderKey(raw)

		if isSensitive(key) {
			masked := make([]string, len(values))
			for i := range values {
				masked[i] = maskValue
			}

			out[key] = masked

			continue
		}

		copyVals := make([]string, len(values))
		copy(copyVals, values)
		out[key] = copyVals
	}

	return out
}

// MaskStringMap is the same masking applied to a flat map (used for
// target.Headers, target.Metadata which are configured as cty objects).
func MaskStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}

	out := make(map[string]string, len(in))

	for raw, value := range in {
		key := textproto.CanonicalMIMEHeaderKey(raw)
		if isSensitive(key) {
			out[key] = maskValue

			continue
		}

		out[key] = value
	}

	return out
}

func isSensitive(canonicalKey string) bool {
	if _, ok := sensitiveExactNames[canonicalKey]; ok {
		return true
	}

	lower := strings.ToLower(canonicalKey)
	for _, needle := range sensitiveSubstrings {
		if strings.Contains(lower, needle) {
			return true
		}
	}

	return false
}
