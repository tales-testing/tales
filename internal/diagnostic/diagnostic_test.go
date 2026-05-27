package diagnostic

import (
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func TestFromCTYRendering(t *testing.T) {
	t.Parallel()

	value := cty.ObjectVal(map[string]cty.Value{
		"num":   cty.NumberIntVal(42),
		"str":   cty.StringVal("hello"),
		"bool":  cty.True,
		"obj":   cty.ObjectVal(map[string]cty.Value{"x": cty.StringVal("y")}),
		"array": cty.TupleVal([]cty.Value{cty.StringVal("a"), cty.NumberIntVal(2)}),
	})

	converted := FromCTY(value)
	mapped, ok := converted.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", converted)
	}

	if mapped["str"] != "hello" {
		t.Fatalf("unexpected string value: %#v", mapped["str"])
	}

	if mapped["bool"] != true {
		t.Fatalf("unexpected bool value: %#v", mapped["bool"])
	}

	if mapped["num"] != int64(42) {
		t.Fatalf("unexpected number value: %#v", mapped["num"])
	}

	obj, ok := mapped["obj"].(map[string]interface{})
	if !ok || obj["x"] != "y" {
		t.Fatalf("unexpected object value: %#v", mapped["obj"])
	}

	array, ok := mapped["array"].([]interface{})
	if !ok {
		t.Fatalf("unexpected array type: %T", mapped["array"])
	}

	if len(array) != 2 {
		t.Fatalf("unexpected array length: %d", len(array))
	}
}

func TestMaskHeadersCaseInsensitive(t *testing.T) {
	t.Parallel()

	headers := MaskHeaders(map[string]interface{}{
		"Authorization": "Bearer 123",
		"x-api-key":     "abc",
		"Accept":        "application/json",
	})

	if headers["Authorization"] != "***" {
		t.Fatalf("authorization should be masked: %v", headers)
	}

	if headers["x-api-key"] != "***" {
		t.Fatalf("x-api-key should be masked: %v", headers)
	}

	if headers["Accept"] != "application/json" {
		t.Fatalf("accept should remain visible: %v", headers)
	}
}

func TestMaskJSONNestedAndArrays(t *testing.T) {
	t.Parallel()

	payload := map[string]interface{}{
		"user": map[string]interface{}{
			"password": "secret",
			"profile": map[string]interface{}{
				"access_token": "token-1",
			},
		},
		"items": []interface{}{
			map[string]interface{}{"token": "abc"},
			map[string]interface{}{"name": "visible"},
		},
	}

	masked := MaskJSON(payload)
	mapped := masked.(map[string]interface{})

	user := mapped["user"].(map[string]interface{})
	if user["password"] != "***" {
		t.Fatalf("password should be masked: %#v", user)
	}

	profile := user["profile"].(map[string]interface{})
	if profile["access_token"] != "***" {
		t.Fatalf("access token should be masked: %#v", profile)
	}

	items := mapped["items"].([]interface{})
	first := items[0].(map[string]interface{})
	if first["token"] != "***" {
		t.Fatalf("token in array should be masked: %#v", first)
	}

	second := items[1].(map[string]interface{})
	if second["name"] != "visible" {
		t.Fatalf("non-sensitive fields should be preserved: %#v", second)
	}
}

func TestMaskBodyFormEncodedSecrets(t *testing.T) {
	t.Parallel()

	masked := MaskBody("grant_type=password&password=pa%2Bss+%25%23&username=user%40example.com")
	if masked != "grant_type=password&password=***&username=user%40example.com" {
		t.Fatalf("unexpected masked form body: %v", masked)
	}
}

func TestSanitizeMapMasksAuthPassword(t *testing.T) {
	t.Parallel()

	sanitized := SanitizeMap(map[string]interface{}{
		"auth": map[string]interface{}{
			"basic": map[string]interface{}{
				"username": "admin",
				"password": "secret",
			},
		},
	})

	auth := sanitized["auth"].(map[string]interface{})
	basic := auth["basic"].(map[string]interface{})
	if basic["username"] != "admin" {
		t.Fatalf("username should remain visible: %#v", basic)
	}
	if basic["password"] != "***" {
		t.Fatalf("password should be masked: %#v", basic)
	}
}

func TestMaskHeadersSignatureContains(t *testing.T) {
	t.Parallel()

	headers := MaskHeaders(map[string]interface{}{
		"X-Anchorify-Signature": "deadbeef",
		"X-My-Signature-Token":  "abc123",
		"X-Visible":             "ok",
	})

	if headers["X-Anchorify-Signature"] != "***" {
		t.Fatalf("X-Anchorify-Signature should be masked: %v", headers)
	}

	if headers["X-My-Signature-Token"] != "***" {
		t.Fatalf("X-My-Signature-Token should be masked: %v", headers)
	}

	if headers["X-Visible"] != "ok" {
		t.Fatalf("non-sensitive header should stay visible: %v", headers)
	}
}

func TestMaskHeadersAllMasksEverySetCookie(t *testing.T) {
	t.Parallel()

	headers := MaskHeadersAll(map[string]interface{}{
		"Set-Cookie": []interface{}{
			"ia_session=abc; Path=/; HttpOnly",
			"theme=dark; Path=/",
		},
		"Content-Type": []interface{}{"application/json"},
	})

	setCookie, ok := headers["Set-Cookie"]
	if !ok {
		t.Fatalf("expected Set-Cookie in masked headers_all")
	}

	if len(setCookie) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(setCookie))
	}

	for i, v := range setCookie {
		if v != "***" {
			t.Fatalf("Set-Cookie[%d] should be masked, got %q", i, v)
		}
	}

	if got := headers["Content-Type"]; len(got) != 1 || got[0] != "application/json" {
		t.Fatalf("Content-Type should pass through unchanged, got %v", got)
	}
}

func TestMaskCookiesRedactsValueAndRaw(t *testing.T) {
	t.Parallel()

	cookies := MaskCookies(map[string]interface{}{
		"ia_session": map[string]interface{}{
			"name":      "ia_session",
			"value":     "abc123",
			"raw":       "ia_session=abc123; Path=/; HttpOnly",
			"path":      "/",
			"http_only": true,
		},
	})

	session, ok := cookies["ia_session"]
	if !ok {
		t.Fatalf("expected ia_session in masked cookies")
	}

	if session["value"] != "***" {
		t.Fatalf("cookie value must be masked: %#v", session)
	}

	if session["raw"] != "***" {
		t.Fatalf("cookie raw must be masked: %#v", session)
	}

	if session["name"] != "ia_session" {
		t.Fatalf("cookie name must stay visible: %#v", session)
	}

	if session["path"] != "/" {
		t.Fatalf("cookie path must stay visible: %#v", session)
	}

	if session["http_only"] != true {
		t.Fatalf("cookie http_only must stay visible: %#v", session)
	}
}

func TestSanitizeMapDispatchesCookiesAndHeadersAll(t *testing.T) {
	t.Parallel()

	sanitized := SanitizeMap(map[string]interface{}{
		"headers_all": map[string]interface{}{
			"Set-Cookie": []interface{}{"session=secret-1", "theme=dark"},
		},
		"cookies": map[string]interface{}{
			"session": map[string]interface{}{
				"value": "secret-1",
				"raw":   "session=secret-1; Path=/",
				"name":  "session",
			},
		},
	})

	headersAll := sanitized["headers_all"].(map[string][]string)
	if headersAll["Set-Cookie"][0] != "***" || headersAll["Set-Cookie"][1] != "***" {
		t.Fatalf("Set-Cookie values should be masked end-to-end: %#v", headersAll)
	}

	cookies := sanitized["cookies"].(map[string]map[string]interface{})
	session := cookies["session"]
	if session["value"] != "***" || session["raw"] != "***" {
		t.Fatalf("cookie value/raw should be masked end-to-end: %#v", session)
	}
}

func TestIsSensitiveJSONFieldMasksCodeVerifier(t *testing.T) {
	t.Parallel()

	if !isSensitiveJSONField("code_verifier") {
		t.Fatalf("code_verifier (RFC 7636 PKCE) must be masked in JSON bodies")
	}

	if !isSensitiveJSONField("Code_Verifier") {
		t.Fatalf("code_verifier check should be case-insensitive")
	}

	if isSensitiveJSONField("verifier") {
		t.Fatalf("bare \"verifier\" should not be masked — too prone to false positives (is_verified, etc.)")
	}
}

func TestIsSensitiveJSONFieldSecretContains(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"secret":        true,
		"mfa_secret":    true,
		"MFA_Secret":    true,
		"hmac_secret":   true,
		"client-secret": true,
		"secret_value":  true,
		"secretary":     false,
		"discreet":      false,
		"name":          false,
	}

	for name, want := range cases {
		if got := isSensitiveJSONField(name); got != want {
			t.Fatalf("isSensitiveJSONField(%q) = %v, want %v", name, got, want)
		}
	}
}
