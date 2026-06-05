package lang

import (
	"testing"

	"github.com/zclconf/go-cty/cty"
)

// TestHashHexKnownVectors pins HashHex against the canonical empty-input
// digests for every algorithm Tales exposes. The empty-string vectors are
// standard and let a reviewer verify the wiring without an external tool.
func TestHashHexKnownVectors(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"sha1":       "da39a3ee5e6b4b0d3255bfef95601890afd80709",
		"sha224":     "d14a028c2a3a2bc9476102bb288234c415a2b01f828ea62ac5b3e42f",
		"sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"sha384":     "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b",
		"sha512":     "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
		"sha512_224": "6ed0dd02806fa89e25de060c19d3ac86cabb87d6a0ddd05c333b84f4",
		"sha512_256": "c672b8d1ef56ed28ab87c3622c5114069bdd3ad7b8f9737498d0c01ecef0967a",
	}

	for algo, want := range cases {
		t.Run(algo, func(t *testing.T) {
			t.Parallel()

			got, err := HashHex(algo, []byte(""))
			if err != nil {
				t.Fatalf("HashHex(%q) error: %v", algo, err)
			}

			if got != want {
				t.Fatalf("HashHex(%q) = %q, want %q", algo, got, want)
			}
		})
	}
}

// TestHashHexNonEmpty checks a non-empty input against the well-known
// SHA-256("abc") vector and confirms HashHex is byte-exact (not string-coerced).
func TestHashHexNonEmpty(t *testing.T) {
	t.Parallel()

	const wantSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

	got, err := HashHex("sha256", []byte("abc"))
	if err != nil {
		t.Fatalf("HashHex error: %v", err)
	}

	if got != wantSHA256 {
		t.Fatalf("HashHex(sha256, abc) = %q, want %q", got, wantSHA256)
	}
}

// TestHashAlgorithmsMatchConstructors guards the invariant that the ordered
// presentation list and the constructor registry stay in sync.
func TestHashAlgorithmsMatchConstructors(t *testing.T) {
	t.Parallel()

	algos := HashAlgorithms()
	if len(algos) != len(hashConstructors) {
		t.Fatalf("HashAlgorithms() has %d entries, hashConstructors has %d", len(algos), len(hashConstructors))
	}

	for _, algo := range algos {
		if _, ok := hashConstructors[algo]; !ok {
			t.Fatalf("HashAlgorithms() lists %q, missing from hashConstructors", algo)
		}
	}
}

func TestHashHexUnknownAlgorithm(t *testing.T) {
	t.Parallel()

	if _, err := HashHex("md5", []byte("x")); err == nil {
		t.Fatal("expected error for unknown algorithm, got nil")
	}
}

// TestHashHexMatchesCTYFunction guards the invariant that the byte helper and
// the expression-level `*_hex` functions share one implementation: hashing the
// same bytes both ways must agree for every registered algorithm.
func TestHashHexMatchesCTYFunction(t *testing.T) {
	t.Parallel()

	functions := hashFunctions()

	for algo := range hashConstructors {
		t.Run(algo, func(t *testing.T) {
			t.Parallel()

			fn, ok := functions[algo+"_hex"]
			if !ok {
				t.Fatalf("missing cty function for algorithm %q", algo)
			}

			viaHelper, err := HashHex(algo, []byte("tales"))
			if err != nil {
				t.Fatalf("HashHex error: %v", err)
			}

			viaFunc, err := fn.Call([]cty.Value{cty.StringVal("tales")})
			if err != nil {
				t.Fatalf("cty function error: %v", err)
			}

			if viaFunc.AsString() != viaHelper {
				t.Fatalf("cty %s_hex = %q, HashHex = %q", algo, viaFunc.AsString(), viaHelper)
			}
		})
	}
}
