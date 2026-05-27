package lang

import (
	"strings"
	"testing"
)

// rfc6238Secret is the ASCII secret "12345678901234567890" encoded as Base32 —
// the SHA-1 test secret from RFC 6238, Appendix B.
const rfc6238Secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

// totpTS is a tiny helper that promotes an int64 literal to *int64 inline,
// keeping TOTPOptions test literals readable now that Timestamp is a pointer.
func totpTS(v int64) *int64 { return &v }

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
			Timestamp: totpTS(tc.timestamp),
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

	got, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Timestamp: totpTS(59)})
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
	a, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Period: 60, Timestamp: totpTS(60)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Period: 60, Timestamp: totpTS(119)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if a != b {
		t.Fatalf("expected same code inside one 60s window, got %s vs %s", a, b)
	}

	c, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Period: 60, Timestamp: totpTS(120)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if a == c {
		t.Fatalf("expected different code in the next 60s window, both were %s", a)
	}
}

func TestGenerateTOTPSecretNormalization(t *testing.T) {
	t.Parallel()

	canonical, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Period: 30, Digits: 8, Timestamp: totpTS(59)})
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
		got, err := GenerateTOTP(variant, TOTPOptions{Period: 30, Digits: 8, Timestamp: totpTS(59)})
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
		{"negative timestamp", TOTPOptions{Timestamp: totpTS(-1)}, "timestamp must be >= 0"},
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

	_, err := GenerateTOTP(badSecret, TOTPOptions{Timestamp: totpTS(59)})
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

	_, err := GenerateTOTP("    ", TOTPOptions{Timestamp: totpTS(59)})
	if err == nil || err.Error() != "invalid TOTP base32 secret" {
		t.Fatalf("expected opaque invalid-secret error, got %v", err)
	}
}

func TestGenerateTOTPExplicitZeroTimestampHonored(t *testing.T) {
	t.Parallel()

	// Regression test for the *int64 timestamp pointer fix: an explicit
	// timestamp = 0 must reach the algorithm unchanged, not be silently
	// replaced with the wall clock. With period=30 the counter is 0, and
	// RFC 4226 Appendix D pins the 6-digit HOTP for the same SHA-1 secret
	// at counter=0 to "755224".
	got, err := GenerateTOTP(rfc6238Secret, TOTPOptions{
		Period:    30,
		Digits:    6,
		Algorithm: "SHA1",
		Timestamp: totpTS(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != "755224" {
		t.Fatalf("explicit Timestamp=0 must yield HOTP counter=0 code 755224, got %s", got)
	}

	// And it must be stable, i.e. not accidentally re-reading the wall clock.
	again, err := GenerateTOTP(rfc6238Secret, TOTPOptions{
		Period:    30,
		Digits:    6,
		Algorithm: "SHA1",
		Timestamp: totpTS(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if again != got {
		t.Fatalf("explicit Timestamp=0 produced different codes across calls: %s vs %s", got, again)
	}
}

func TestGenerateTOTPDigitsTenIsAllowed(t *testing.T) {
	t.Parallel()

	got, err := GenerateTOTP(rfc6238Secret, TOTPOptions{Period: 30, Digits: 10, Timestamp: totpTS(59)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 10 {
		t.Fatalf("expected 10-digit string, got %q (%d chars)", got, len(got))
	}
}
