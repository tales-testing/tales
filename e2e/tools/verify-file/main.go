// Command verify-file is a tiny, generic e2e helper for the exec provider. It
// reads the file named by its first argument, computes its size and SHA-256 /
// SHA-512 digests, and prints a JSON report to stdout:
//
//	{"valid":true,"size_bytes":123,"sha256":"...","sha512":"..."}
//
// It is intentionally domain-agnostic (no PDF / Merkle / Sealway logic): it
// stands in for any external verifier a real project would run.
package main

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

type report struct {
	Valid     bool   `json:"valid"`
	SizeBytes int    `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	SHA512    string `json:"sha512"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: verify-file <path>")
		os.Exit(2)
	}

	//nolint:gosec // G703: reading the caller-supplied path is the entire purpose of this generic e2e verifier CLI.
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}

	sum256 := sha256.Sum256(data)
	sum512 := sha512.Sum512(data)

	out := report{
		Valid:     true,
		SizeBytes: len(data),
		SHA256:    hex.EncodeToString(sum256[:]),
		SHA512:    hex.EncodeToString(sum512[:]),
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode report: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write(encoded); err != nil {
		os.Exit(1)
	}
}
