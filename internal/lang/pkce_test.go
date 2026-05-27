package lang

import (
	"strings"
	"testing"
)

// RFC 7636 appendix B vector — proves the S256 transformation end-to-end.
const (
	rfc7636Verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	rfc7636Challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
)

func TestPKCEChallengeRFC7636Vector(t *testing.T) {
	t.Parallel()

	got, err := PKCEChallenge(rfc7636Verifier, PKCEOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != rfc7636Challenge {
		t.Fatalf("expected %s, got %s", rfc7636Challenge, got)
	}
}

func TestPKCEChallengeDefaultIsS256(t *testing.T) {
	t.Parallel()

	defaulted, err := PKCEChallenge(rfc7636Verifier, PKCEOptions{})
	if err != nil {
		t.Fatalf("default: unexpected error: %v", err)
	}

	explicit, err := PKCEChallenge(rfc7636Verifier, PKCEOptions{Method: "S256"})
	if err != nil {
		t.Fatalf("explicit S256: unexpected error: %v", err)
	}

	if defaulted != explicit {
		t.Fatalf("default and explicit S256 should match: %s vs %s", defaulted, explicit)
	}
}

func TestPKCEChallengePlain(t *testing.T) {
	t.Parallel()

	got, err := PKCEChallenge(rfc7636Verifier, PKCEOptions{Method: "plain"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != rfc7636Verifier {
		t.Fatalf("plain method should return verifier unchanged, got %s", got)
	}
}

func TestPKCEChallengeUnsupportedMethod(t *testing.T) {
	t.Parallel()

	_, err := PKCEChallenge(rfc7636Verifier, PKCEOptions{Method: "MD5"})
	if err == nil {
		t.Fatalf("expected error for unsupported method")
	}

	if !strings.Contains(err.Error(), "unsupported PKCE method") {
		t.Fatalf("expected unsupported-method message, got %v", err)
	}
}

func TestPKCEChallengeCTYExplicitS256(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t,
		`pkce_challenge("`+rfc7636Verifier+`", {method = "S256"})`,
	)

	if value.AsString() != rfc7636Challenge {
		t.Fatalf("cty surface mismatch: got %s", value.AsString())
	}
}

func TestPKCEChallengeCTYDefault(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `pkce_challenge("`+rfc7636Verifier+`")`)
	if value.AsString() != rfc7636Challenge {
		t.Fatalf("cty default mismatch: got %s", value.AsString())
	}
}

func TestPKCEChallengeCTYRejectsUnknownOption(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`pkce_challenge("v", {foo = "bar"})`)
	if err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("expected unknown-option error, got %v", err)
	}
}

func TestPKCEChallengeCTYRejectsUnsupportedMethod(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`pkce_challenge("v", {method = "MD5"})`)
	if err == nil || !strings.Contains(err.Error(), "unsupported PKCE method") {
		t.Fatalf("expected unsupported-method error, got %v", err)
	}
}

func TestPKCEChallengeCTYRejectsTooManyArguments(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`pkce_challenge("v", {method = "S256"}, {extra = true})`)
	if err == nil || !strings.Contains(err.Error(), "too many arguments") {
		t.Fatalf("expected too-many-arguments error, got %v", err)
	}
}

func TestPKCEChallengeCTYRejectsNonObjectOptions(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`pkce_challenge("v", "not-an-object")`)
	if err == nil || !strings.Contains(err.Error(), "options must be an object") {
		t.Fatalf("expected options-must-be-an-object error, got %v", err)
	}
}

func TestPKCEChallengeIsNotBase64URLOfHex(t *testing.T) {
	t.Parallel()

	// Guard against the natural mistake: composing base64url_encode(sha256_hex(...))
	// would encode the 64-char hex string, not the 32 raw bytes. The two
	// outputs must differ — pkce_challenge is the only correct path.
	wrong := evalTestExpression(t,
		`base64url_encode(sha256_hex("`+rfc7636Verifier+`"))`,
	).AsString()

	if wrong == rfc7636Challenge {
		t.Fatalf("base64url_encode(sha256_hex(...)) accidentally equals the PKCE challenge — vectors collide")
	}
}
