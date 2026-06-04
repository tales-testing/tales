package webhook

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: exercising the HMAC-SHA1 path the provider offers.
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"strings"
	"testing"
	"time"
)

func TestParseSignatureFormatRoundTrip(t *testing.T) {
	t.Parallel()

	format, err := ParseSignatureFormat("t={timestamp},v1={signature}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !format.HasTimestamp || !format.HasSignature {
		t.Fatalf("expected both placeholders, got ts=%v sig=%v", format.HasTimestamp, format.HasSignature)
	}

	ts, sig, ok := format.Parse("t=1700000000,v1=abcdef0123456789")
	if !ok {
		t.Fatal("expected header to match format")
	}

	if ts != "1700000000" || sig != "abcdef0123456789" {
		t.Fatalf("unexpected parse result ts=%q sig=%q", ts, sig)
	}
}

func TestParseSignatureFormatSignatureOnly(t *testing.T) {
	t.Parallel()

	format, err := ParseSignatureFormat("sha256={signature}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if format.HasTimestamp {
		t.Fatal("did not expect a timestamp placeholder")
	}

	ts, sig, ok := format.Parse("sha256=deadbeef")
	if !ok || ts != "" || sig != "deadbeef" {
		t.Fatalf("unexpected parse ok=%v ts=%q sig=%q", ok, ts, sig)
	}
}

func TestParseSignatureFormatErrors(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"", "t={timestamp}", "t={timestamp},v1={signature},x={signature}"} {
		if _, err := ParseSignatureFormat(format); err == nil {
			t.Fatalf("expected error for format %q", format)
		}
	}
}

func TestParseSignatureFormatNonMatchingHeader(t *testing.T) {
	t.Parallel()

	format, err := ParseSignatureFormat("t={timestamp},v1={signature}")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if _, _, ok := format.Parse("nonsense"); ok {
		t.Fatal("expected non-matching header to be rejected")
	}
}

func TestComputeHMACKnownVector(t *testing.T) {
	t.Parallel()

	// RFC-style test vector: HMAC-SHA256(key="key", msg="The quick brown fox
	// jumps over the lazy dog").
	got, err := ComputeHMAC("sha256", "key", "The quick brown fox jumps over the lazy dog")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}

	const want = "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8"
	if got != want {
		t.Fatalf("hmac-sha256 = %q, want %q", got, want)
	}
}

func TestComputeHMACAllAlgorithms(t *testing.T) {
	t.Parallel()

	constructors := map[string]func() hash.Hash{
		"sha1":   sha1.New,
		"sha256": sha256.New,
		"sha384": sha512.New384,
		"sha512": sha512.New,
	}

	for algorithm, newHash := range constructors {
		got, err := ComputeHMAC(algorithm, "secret", "payload")
		if err != nil {
			t.Fatalf("%s: %v", algorithm, err)
		}

		mac := hmac.New(newHash, []byte("secret"))
		_, _ = mac.Write([]byte("payload"))

		want := hex.EncodeToString(mac.Sum(nil))
		if got != want {
			t.Fatalf("%s = %q, want %q", algorithm, got, want)
		}
	}
}

func TestComputeHMACUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()

	if _, err := ComputeHMAC("md5", "secret", "payload"); err == nil {
		t.Fatal("expected unsupported algorithm error")
	}
}

func TestSignaturesEqual(t *testing.T) {
	t.Parallel()

	if !SignaturesEqual("ABCDEF", "abcdef") {
		t.Fatal("expected case-insensitive match")
	}

	if SignaturesEqual("abcdef", "abcdee") {
		t.Fatal("expected mismatch")
	}
}

func TestTimestampWithinTolerance(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)

	within, err := TimestampWithinTolerance("1700000100", now, 5*time.Minute)
	if err != nil || !within {
		t.Fatalf("expected within tolerance, got within=%v err=%v", within, err)
	}

	outside, err := TimestampWithinTolerance("1700000400", now, time.Minute)
	if err != nil || outside {
		t.Fatalf("expected outside tolerance, got outside=%v err=%v", outside, err)
	}

	disabled, err := TimestampWithinTolerance("not-a-number", now, 0)
	if err != nil || !disabled {
		t.Fatalf("zero tolerance must disable the check, got %v err=%v", disabled, err)
	}

	if _, err := TimestampWithinTolerance("not-a-number", now, time.Minute); err == nil {
		t.Fatal("expected error for non-numeric timestamp")
	}
}

// TestComputeHMACNoSecretLeak ensures the secret never appears in an error.
func TestComputeHMACNoSecretLeak(t *testing.T) {
	t.Parallel()

	_, err := ComputeHMAC("bogus", "super-secret-value", "payload")
	if err == nil {
		t.Fatal("expected error")
	}

	if got := err.Error(); strings.Contains(got, "super-secret-value") {
		t.Fatalf("error leaked secret: %q", got)
	}
}
