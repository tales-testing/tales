package lang

import (
	"strings"
	"testing"
)

// Known empty-string digests for every registered hash variant. Values come
// directly from the relevant RFC / NIST published test vectors.
var emptyStringDigests = map[string]string{
	"sha1_hex":       "da39a3ee5e6b4b0d3255bfef95601890afd80709",
	"sha224_hex":     "d14a028c2a3a2bc9476102bb288234c415a2b01f828ea62ac5b3e42f",
	"sha256_hex":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	"sha384_hex":     "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b",
	"sha512_hex":     "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
	"sha512_224_hex": "6ed0dd02806fa89e25de060c19d3ac86cabb87d6a0ddd05c333b84f4",
	"sha512_256_hex": "c672b8d1ef56ed28ab87c3622c5114069bdd3ad7b8f9737498d0c01ecef0967a",
}

var helloDigests = map[string]string{
	"sha1_hex":   "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d",
	"sha256_hex": "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
	"sha512_hex": "9b71d224bd62f3785d96d46ad3ea3d73319bfbc2890caadae2dff72519673ca72323c3d99ba5c11d7c7acc6e14b8c5da0c4663475c2e5c3adef46f73bcdec043",
}

func TestHashFunctionsEmptyStringVectors(t *testing.T) {
	t.Parallel()

	for name, expected := range emptyStringDigests {
		value := evalTestExpression(t, name+`("")`)
		if value.AsString() != expected {
			t.Fatalf("%s(\"\") = %s, want %s", name, value.AsString(), expected)
		}
	}
}

func TestHashFunctionsHelloVectors(t *testing.T) {
	t.Parallel()

	for name, expected := range helloDigests {
		value := evalTestExpression(t, name+`("hello")`)
		if value.AsString() != expected {
			t.Fatalf("%s(\"hello\") = %s, want %s", name, value.AsString(), expected)
		}
	}
}

func TestHashFunctionsLowercaseHex(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `sha256_hex("hello")`)
	if got := value.AsString(); got != strings.ToLower(got) {
		t.Fatalf("expected lowercase hex, got %s", got)
	}
}

func TestHashFunctionsRejectsArity(t *testing.T) {
	t.Parallel()

	if _, err := evalTestExpressionError(`sha256_hex()`); err == nil {
		t.Fatalf("expected error for missing argument")
	}

	if _, err := evalTestExpressionError(`sha256_hex("a", "b")`); err == nil {
		t.Fatalf("expected error for extra argument")
	}
}

func TestHashFunctionsDoNotLeakInput(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`sha256_hex()`)
	if err == nil {
		t.Fatalf("expected error for missing arguments")
	}

	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("error message must not embed user material: %v", err)
	}
}

func TestHashFunctionsUnicode(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `sha256_hex("café")`)
	if len(value.AsString()) != 64 {
		t.Fatalf("expected 64-char hex, got %d chars: %s", len(value.AsString()), value.AsString())
	}
}
