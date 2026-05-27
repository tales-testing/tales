package lang

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: HMAC-SHA1 is mandated by RFC 6238 TOTP; this implementation is the spec, not a security primitive choice
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TOTPOptions parameterizes GenerateTOTP. All fields are optional; zero values
// trigger the documented RFC 6238 defaults (Period=30, Digits=6,
// Algorithm="SHA1", Timestamp=time.Now().Unix()).
type TOTPOptions struct {
	Period    int64
	Digits    int
	Algorithm string
	Timestamp int64
}

const (
	totpDefaultPeriod    int64  = 30
	totpDefaultDigits    int    = 6
	totpDefaultAlgorithm string = "SHA1"
	totpMaxDigits        int    = 10
)

// errInvalidTOTPSecret is the public-facing error for any Base32 decode
// failure. It never embeds the raw secret material.
var errInvalidTOTPSecret = errors.New("invalid TOTP base32 secret")

// GenerateTOTP implements RFC 6238 on top of RFC 4226 HOTP. The secret is a
// Base32-encoded byte string (with or without padding, spaces, or hyphens —
// the function normalizes user-friendly variants). Errors never expose the
// raw secret.
func GenerateTOTP(secretBase32 string, opts TOTPOptions) (string, error) {
	period := opts.Period
	if period == 0 {
		period = totpDefaultPeriod
	}

	if period <= 0 {
		return "", fmt.Errorf("totp: period must be > 0")
	}

	digits := opts.Digits
	if digits == 0 {
		digits = totpDefaultDigits
	}

	if digits <= 0 {
		return "", fmt.Errorf("totp: digits must be > 0")
	}

	if digits > totpMaxDigits {
		return "", fmt.Errorf("totp: digits must be <= %d", totpMaxDigits)
	}

	algorithm := opts.Algorithm
	if algorithm == "" {
		algorithm = totpDefaultAlgorithm
	}

	if !strings.EqualFold(algorithm, totpDefaultAlgorithm) {
		return "", fmt.Errorf("unsupported TOTP algorithm %q; supported algorithms: SHA1", algorithm)
	}

	timestamp := opts.Timestamp
	if opts.Timestamp == 0 {
		// A zero option means "use the wall clock". Negative timestamps are
		// explicitly rejected below; zero on the wire is therefore unreachable.
		timestamp = time.Now().Unix()
	}

	if timestamp < 0 {
		return "", fmt.Errorf("totp: timestamp must be >= 0")
	}

	secretBytes, err := decodeTOTPSecret(secretBase32)
	if err != nil {
		return "", err
	}

	//nolint:gosec // G115: timestamp >= 0 and period > 0 are both validated above, so the quotient is non-negative
	counter := uint64(timestamp / period)

	var counterBytes [8]byte

	binary.BigEndian.PutUint64(counterBytes[:], counter)

	mac := hmac.New(sha1.New, secretBytes)
	if _, writeErr := mac.Write(counterBytes[:]); writeErr != nil {
		return "", fmt.Errorf("totp: hmac write failed")
	}

	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	binaryCode := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]&0xff) << 16) |
		(uint32(sum[offset+2]&0xff) << 8) |
		uint32(sum[offset+3]&0xff)

	modulus := uint64(1)
	for i := 0; i < digits; i++ {
		modulus *= 10
	}

	otp := uint64(binaryCode) % modulus
	width := fmt.Sprintf("%%0%dd", digits)

	return fmt.Sprintf(width, otp), nil
}

// decodeTOTPSecret normalizes a user-supplied Base32 secret and decodes it.
// Accepts upper/lower case, surrounding whitespace, embedded spaces, hyphens,
// and both padded and unpadded forms. Returns errInvalidTOTPSecret for any
// decode failure; the raw input is never echoed.
func decodeTOTPSecret(secretBase32 string) ([]byte, error) {
	normalized := strings.ToUpper(strings.TrimSpace(secretBase32))
	normalized = strings.NewReplacer(" ", "", "-", "").Replace(normalized)
	normalized = strings.TrimRight(normalized, "=")

	if normalized == "" {
		return nil, errInvalidTOTPSecret
	}

	bytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil {
		return nil, errInvalidTOTPSecret
	}

	return bytes, nil
}
