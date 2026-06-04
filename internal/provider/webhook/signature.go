package webhook

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: HMAC-SHA1 is offered because webhook senders (Stripe-style) still emit it; the user opts in.
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// signatureAlgorithms maps the supported V1 algorithm names to their hash
// constructors. Adding a new algorithm is a single map entry.
var signatureAlgorithms = map[string]func() hash.Hash{
	"sha1":   sha1.New,
	"sha256": sha256.New,
	"sha384": sha512.New384,
	"sha512": sha512.New,
}

// Capture-group names shared between the format compiler and the parser.
const (
	groupTimestamp = "timestamp"
	groupSignature = "signature"
)

// placeholderPattern matches the {timestamp} / {signature} tokens inside a
// signature format string.
var placeholderPattern = regexp.MustCompile(`\{(timestamp|signature)\}`)

// SignatureFormat is a compiled signature header format. The regexp carries
// named capture groups for the placeholders found in the literal format.
type SignatureFormat struct {
	re           *regexp.Regexp
	HasTimestamp bool
	HasSignature bool
}

// ParseSignatureFormat converts a literal format such as
// "t={timestamp},v1={signature}" into an anchored regexp with named capture
// groups. Literal segments are escaped; {timestamp} becomes a comma-bounded
// group and {signature} a hex group. Each placeholder may appear at most once
// and {signature} is mandatory.
func ParseSignatureFormat(format string) (*SignatureFormat, error) {
	if strings.TrimSpace(format) == "" {
		return nil, fmt.Errorf("signature format must not be empty")
	}

	out := &SignatureFormat{}

	var (
		builder strings.Builder
		last    int
	)

	builder.WriteString("^")

	for _, loc := range placeholderPattern.FindAllStringSubmatchIndex(format, -1) {
		builder.WriteString(regexp.QuoteMeta(format[last:loc[0]]))

		switch format[loc[2]:loc[3]] {
		case groupTimestamp:
			if out.HasTimestamp {
				return nil, fmt.Errorf("signature format may use {timestamp} at most once")
			}

			out.HasTimestamp = true

			builder.WriteString(`(?P<timestamp>[^,]+)`)
		case groupSignature:
			if out.HasSignature {
				return nil, fmt.Errorf("signature format may use {signature} at most once")
			}

			out.HasSignature = true

			builder.WriteString(`(?P<signature>[A-Fa-f0-9]+)`)
		}

		last = loc[1]
	}

	builder.WriteString(regexp.QuoteMeta(format[last:]))
	builder.WriteString("$")

	if !out.HasSignature {
		return nil, fmt.Errorf("signature format must contain a {signature} placeholder")
	}

	re, err := regexp.Compile(builder.String())
	if err != nil {
		return nil, fmt.Errorf("invalid signature format: %w", err)
	}

	out.re = re

	return out, nil
}

// Parse extracts the timestamp and signature from a header value. The boolean
// reports whether the header matched the format at all.
func (f *SignatureFormat) Parse(headerValue string) (timestamp, signature string, ok bool) {
	match := f.re.FindStringSubmatch(headerValue)
	if match == nil {
		return "", "", false
	}

	for i, name := range f.re.SubexpNames() {
		switch name {
		case groupTimestamp:
			timestamp = match[i]
		case groupSignature:
			signature = match[i]
		}
	}

	return timestamp, signature, true
}

// ComputeHMAC returns the lowercase hex HMAC of payload keyed by secret using
// the named algorithm. The secret is never embedded in the error.
func ComputeHMAC(algorithm, secret, payload string) (string, error) {
	newHash, ok := signatureAlgorithms[algorithm]
	if !ok {
		return "", fmt.Errorf("unsupported HMAC algorithm %q", algorithm)
	}

	mac := hmac.New(newHash, []byte(secret))
	if _, err := mac.Write([]byte(payload)); err != nil {
		return "", fmt.Errorf("compute HMAC: %w", err)
	}

	return hex.EncodeToString(mac.Sum(nil)), nil
}

// SignaturesEqual compares two hex signatures in constant time, case
// insensitively (hex digests may arrive upper or lower case).
func SignaturesEqual(a, b string) bool {
	return hmac.Equal([]byte(strings.ToLower(a)), []byte(strings.ToLower(b)))
}

// TimestampWithinTolerance reports whether a unix-seconds timestamp string is
// within tolerance of now. A non-positive tolerance disables the check.
func TimestampWithinTolerance(timestamp string, now time.Time, tolerance time.Duration) (bool, error) {
	if tolerance <= 0 {
		return true, nil
	}

	ts, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return false, fmt.Errorf("webhook signature timestamp is not a unix timestamp")
	}

	delta := now.Unix() - ts
	if delta < 0 {
		delta = -delta
	}

	return time.Duration(delta)*time.Second <= tolerance, nil
}
