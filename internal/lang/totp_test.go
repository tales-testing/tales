package lang

import (
	"strings"
	"testing"
)

// rfc6238Secret is the ASCII secret "12345678901234567890" encoded as Base32 —
// the SHA-1 test secret from RFC 6238, Appendix B.
const rfc6238Secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestGenerateTOTPRFC6238Vectors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		timestamp int64
		expected  string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}

	for _, tc := range cases {
		got, err := GenerateTOTP(rfc6238Secret, TOTPOptions{
			Period:    30,
			Digits:    8,
			Algorithm: "SHA1",
			Timestamp: tc.timestamp,
		})
		if err != nil {
			t.Fatalf("ts=%d: unexpected error: %v", tc.timestamp, err)
		}

		if got != tc.expected {
			t.Fatalf("ts=%d: expected %s, got %s", tc.timestamp, tc.expected, got)
		}
	}
}

func TestGenerateTOTPDefaultsToSixDigits(t *testing.T) {
	t.Parallel()

	got, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Timestamp: 59})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 6 {
		t.Fatalf("expected 6-digit string, got %q (%d chars)", got, len(got))
	}
}

func TestGenerateTOTPCustomPeriod(t *testing.T) {
	t.Parallel()

	// Two timestamps inside the same 60s window ([60, 120)) should yield the
	// same code; bumping past the window must change the code.
	a, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Period: 60, Timestamp: 60})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Period: 60, Timestamp: 119})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if a != b {
		t.Fatalf("expected same code inside one 60s window, got %s vs %s", a, b)
	}

	c, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Period: 60, Timestamp: 120})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if a == c {
		t.Fatalf("expected different code in the next 60s window, both were %s", a)
	}
}

func TestGenerateTOTPSecretNormalization(t *testing.T) {
	t.Parallel()

	canonical, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Period: 30, Digits: 8, Timestamp: 59})
	if err != nil {
		t.Fatalf("canonical secret failed: %v", err)
	}

	variants := []string{
		"  " + rfc6238Secret + "  ",         // surrounding whitespace
		strings.ToLower(rfc6238Secret),       // lowercase
		"GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ",
		"GEZD-GNBV-GY3T-QOJQ-GEZD-GNBV-GY3T-QOJQ",
		rfc6238Secret + "===",                // trailing padding
	}

	for _, variant := range variants {
		got, err := GenerateTOTP(variant, TOTPOptions{Period: 30, Digits: 8, Timestamp: 59})
		if err != nil {
			t.Fatalf("variant %q: unexpected error: %v", variant, err)
		}

		if got != canonical {
			t.Fatalf("variant %q: expected %s, got %s", variant, canonical, got)
		}
	}
}

func TestGenerateTOTPRejectsInvalidOptions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		opts TOTPOptions
		want string
	}{
		{"unsupported algorithm", TOTPOptions{Algorithm: "SHA256"}, "unsupported TOTP algorithm"},
		{"negative period", TOTPOptions{Period: -1}, "period must be > 0"},
		{"negative digits", TOTPOptions{Digits: -1}, "digits must be > 0"},
		{"too many digits", TOTPOptions{Digits: 11}, "digits must be <="},
		{"negative timestamp", TOTPOptions{Timestamp: -1}, "timestamp must be >= 0"},
	}

	for _, tc := range cases {
		_, err := GenerateTOTP(rfc6238Secret, tc.opts)
		if err == nil {
			t.Fatalf("%s: expected error, got nil", tc.name)
		}

		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error to contain %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestGenerateTOTPInvalidSecretIsOpaque(t *testing.T) {
	t.Parallel()

	const badSecret = "BADBADBAD!!!totally-not-base32"

	_, err := GenerateTOTP(badSecret, TOTPOptions{Timestamp: 59})
	if err == nil {
		t.Fatalf("expected invalid-secret error")
	}

	if err.Error() != "invalid TOTP base32 secret" {
		t.Fatalf("expected opaque error message, got %q", err.Error())
	}

	if strings.Contains(err.Error(), "BADBADBAD") {
		t.Fatalf("error message must not echo raw secret material: %v", err)
	}
}

func TestGenerateTOTPEmptySecret(t *testing.T) {
	t.Parallel()

	_, err := GenerateTOTP("    ", TOTPOptions{Timestamp: 59})
	if err == nil || err.Error() != "invalid TOTP base32 secret" {
		t.Fatalf("expected opaque invalid-secret error, got %v", err)
	}
}

func TestGenerateTOTPDigitsTenIsAllowed(t *testing.T) {
	t.Parallel()

	got, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Period: 30, Digits: 10, Timestamp: 59})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 10 {
		t.Fatalf("expected 10-digit string, got %q (%d chars)", got, len(got))
	}
}
