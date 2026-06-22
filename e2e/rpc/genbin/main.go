// genbin emits e2e/rpc/descriptor.bin, the FileDescriptorSet the rpc e2e
// scenarios load at runtime. The schema mirrors
// ../proto/tales/rpc/v1/test.proto verbatim; the source of truth for the
// schema lives in that .proto file and in the shared e2e/rpc/schema
// package (which the mockserver also imports). Run this tool with
//
//	go run ./e2e/rpc/genbin
//
// any time the .proto changes, and commit the resulting descriptor.bin.
// No `buf` or `protoc` dependency is needed because the schema is built
// from descriptorpb directly.
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/tales-testing/tales/e2e/rpc/schema"
	"google.golang.org/protobuf/proto"
)

func main() {
	out := defaultOutputPath()
	if len(os.Args) > 1 {
		out = os.Args[1]
	}

	bytes, err := proto.Marshal(schema.BuildFileDescriptorSet())
	if err != nil {
		log.Fatalf("marshal descriptor set: %v", err)
	}

	//nolint:gosec // G703: developer-only generator tool; output path comes from runtime.Caller default or argv[1] and is intentionally caller-controlled.
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", filepath.Dir(out), err)
	}

	// The output path is either the file-relative default or argv[1]; this
	// is a developer-only generator tool, not a user-facing surface, so we
	// trust the caller. Clean the path so a taint analyser sees a fixed
	// form and the 0644 mode survives.
	cleanOut := filepath.Clean(out)

	//nolint:gosec // G303/G304/G703: developer-only fixture generator; output path is intentionally caller-controlled.
	if err := os.WriteFile(cleanOut, bytes, 0o644); err != nil {
		log.Fatalf("write %s: %v", cleanOut, err)
	}

	//nolint:gosec // G706: developer-only generator tool; the output path is intentionally echoed back so the user sees where the file landed.
	log.Printf("wrote %d bytes to %s", len(bytes), cleanOut)
}

// defaultOutputPath resolves descriptor.bin relative to this file so the
// tool works from any working directory.
func defaultOutputPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "descriptor.bin"
	}

	return filepath.Join(filepath.Dir(filepath.Dir(file)), "descriptor.bin")
}
