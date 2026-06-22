package transport

import (
	"reflect"
	"testing"
)

func TestMaskHeaders_ExactNames(t *testing.T) {
	t.Parallel()

	got := MaskHeaders(map[string][]string{
		"Authorization": {"Bearer secret"},
		"Cookie":        {"session=abc"},
		"X-Trace":       {"trace-1"},
	})

	want := map[string][]string{
		"Authorization": {maskValue},
		"Cookie":        {maskValue},
		"X-Trace":       {"trace-1"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("MaskHeaders = %v, want %v", got, want)
	}
}

func TestMaskHeaders_Substrings(t *testing.T) {
	t.Parallel()

	got := MaskHeaders(map[string][]string{
		"X-Service-Token":   {"abc"},
		"Internal-Password": {"hunter2"},
		"X-Api-Key":         {"key123"},
		"X-Custom":          {"safe"},
	})

	if got["X-Service-Token"][0] != maskValue {
		t.Errorf("X-Service-Token not masked")
	}

	if got["Internal-Password"][0] != maskValue {
		t.Errorf("Internal-Password not masked")
	}

	if got["X-Api-Key"][0] != maskValue {
		t.Errorf("X-Api-Key not masked")
	}

	if got["X-Custom"][0] != "safe" {
		t.Errorf("X-Custom masked unexpectedly")
	}
}

func TestMaskHeaders_CanonicalisesKeys(t *testing.T) {
	t.Parallel()

	got := MaskHeaders(map[string][]string{
		"authorization":   {"Bearer x"},
		"X-trace":         {"trace-1"},
		"CONTENT-TYPE":    {"application/json"},
		"x-api-key":       {"k"},
		"set-cookie":      {"a=b", "c=d"},
		"x-correlationid": {"id"},
	})

	if _, ok := got["Authorization"]; !ok {
		t.Errorf("Authorization not canonicalised: %v", got)
	}

	if got["X-Trace"][0] != "trace-1" {
		t.Errorf("X-Trace mishandled: %v", got)
	}

	if got["Content-Type"][0] != "application/json" {
		t.Errorf("Content-Type mishandled: %v", got)
	}

	if got["X-Api-Key"][0] != maskValue {
		t.Errorf("X-Api-Key not masked: %v", got)
	}

	if len(got["Set-Cookie"]) != 2 || got["Set-Cookie"][0] != maskValue || got["Set-Cookie"][1] != maskValue {
		t.Errorf("Set-Cookie multi-value masking wrong: %v", got)
	}
}

func TestMaskHeaders_Nil(t *testing.T) {
	t.Parallel()

	if got := MaskHeaders(nil); got != nil {
		t.Errorf("MaskHeaders(nil) = %v, want nil", got)
	}
}

func TestMaskStringMap(t *testing.T) {
	t.Parallel()

	got := MaskStringMap(map[string]string{
		"Authorization": "Bearer x",
		"X-Trace":       "id",
		"X-API-Key":     "key",
	})

	if got["Authorization"] != maskValue {
		t.Errorf("Authorization not masked")
	}

	if got["X-Api-Key"] != maskValue {
		t.Errorf("X-Api-Key not masked")
	}

	if got["X-Trace"] != "id" {
		t.Errorf("X-Trace mishandled: %v", got)
	}
}
