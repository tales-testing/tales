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
