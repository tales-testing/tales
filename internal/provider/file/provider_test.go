package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/tales-testing/tales/internal/provider"
)

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}

func execute(t *testing.T, fe provider.FileExecution) (*provider.Output, error) {
	t.Helper()

	return New().Execute(context.Background(), provider.Input{File: &fe})
}

func TestExistingFileHashesAndSize(t *testing.T) {
	t.Parallel()

	data := []byte("hello tales")
	path := writeFile(t, "a.bin", data)

	out, err := execute(t, provider.FileExecution{Path: path, NeedHash: true, NeedSize: true})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !out.Response["exists"].True() {
		t.Fatal("expected exists = true")
	}

	if got, _ := out.Response["size_bytes"].AsBigFloat().Int64(); got != int64(len(data)) {
		t.Fatalf("size_bytes = %d, want %d", got, len(data))
	}

	want := sha256.Sum256(data)
	if got := out.Response["sha256"].AsString(); got != hex.EncodeToString(want[:]) {
		t.Fatalf("sha256 = %q, want %q", got, hex.EncodeToString(want[:]))
	}
}

func TestMissingFileExistsFalse(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nope.bin")

	out, err := execute(t, provider.FileExecution{Path: path})
	if err != nil {
		t.Fatalf("missing file with no required read must not error: %v", err)
	}

	if out.Response["exists"].True() {
		t.Fatal("expected exists = false")
	}
}

func TestMissingFileErrorsWhenReadRequired(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nope.bin")

	if _, err := execute(t, provider.FileExecution{Path: path, NeedHash: true}); err == nil {
		t.Fatal("expected error when a missing file must be hashed")
	}
}

func TestJSONParsing(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "v.json", []byte(`{"valid":true,"merkle":{"root":"abc"}}`))

	out, err := execute(t, provider.FileExecution{Path: path, NeedJSON: true})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	root := out.Response["json"].GetAttr("merkle").GetAttr("root")
	if root.AsString() != "abc" {
		t.Fatalf("json.merkle.root = %q, want abc", root.AsString())
	}
}

func TestInvalidJSONFailsWhenRequired(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "bad.json", []byte(`{not json`))

	if _, err := execute(t, provider.FileExecution{Path: path, NeedJSON: true}); err == nil {
		t.Fatal("expected invalid JSON to error when json is required")
	}
}

func TestTextRead(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "log.txt", []byte("warning: low disk"))

	out, err := execute(t, provider.FileExecution{Path: path, NeedText: true})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if out.Response["text"].AsString() != "warning: low disk" {
		t.Fatalf("unexpected text %q", out.Response["text"].AsString())
	}
}

func TestInvalidUTF8FailsWhenTextRequired(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "bin.dat", []byte{0xff, 0xfe, 0x00, 0x01})

	if _, err := execute(t, provider.FileExecution{Path: path, NeedText: true}); err == nil {
		t.Fatal("expected invalid UTF-8 to error when text is required")
	}
}

func TestBinaryHashableButNotText(t *testing.T) {
	t.Parallel()

	data := []byte{0x25, 0x50, 0x44, 0x46, 0xff, 0xfe}
	path := writeFile(t, "doc.pdf", data)

	// Hashing binary is fine; text is not requested, so no error and text stays null.
	out, err := execute(t, provider.FileExecution{Path: path, NeedHash: true})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !out.Response["text"].IsNull() {
		t.Fatal("expected text to be null when not requested")
	}

	want := sha256.Sum256(data)
	if out.Response["sha256"].AsString() != hex.EncodeToString(want[:]) {
		t.Fatal("binary sha256 mismatch")
	}
}

func TestNilExecutionErrors(t *testing.T) {
	t.Parallel()

	if _, err := New().Execute(context.Background(), provider.Input{}); err == nil {
		t.Fatal("expected error when File execution is nil")
	}
}

// TestBaseAttrsAlwaysPresent guards that every documented file.* attribute is
// present (null when unread) so expressions never hit an unknown attribute.
func TestBaseAttrsAlwaysPresent(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "x.bin", []byte("x"))

	out, err := execute(t, provider.FileExecution{Path: path})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	for _, key := range []string{"path", "exists", "size_bytes", "text", "json", "sha1", "sha256", "sha512", "sha512_256"} {
		if _, ok := out.Response[key]; !ok {
			t.Fatalf("response missing attribute %q", key)
		}
	}
}
