package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

// TestHTTPProviderMultipartSendsExactFileBytes sends one file part from disk
// alongside an inline content part and a plain field, and asserts that the
// server received exactly the bytes the test wrote. The hash equality
// guarantees no re-encoding happens in the multipart serialization path —
// load-bearing for any signature-over-body scheme built on top.
func TestHTTPProviderMultipartSendsExactFileBytes(t *testing.T) {
	t.Parallel()

	fileBytes := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0xAB, 0xCD, 0xEF, 0x00, 0x01, 0x02, 0x03, 0x04}
	dir := t.TempDir()
	talesPath := filepath.Join(dir, "upload.tales")

	if err := os.WriteFile(talesPath, []byte("// stub\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fixturePath := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(fixturePath, fileBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	type received struct {
		field       string
		filename    string
		contentType string
		sha         string
		isFile      bool
		value       string
	}

	var parts []received

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("invalid content-type: %v", err)
		}

		if mediaType != "multipart/form-data" {
			t.Fatalf("expected multipart/form-data, got %s", mediaType)
		}

		if params["boundary"] == "" {
			t.Fatalf("missing boundary parameter")
		}

		if err := req.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}

		for fieldName, headers := range req.MultipartForm.File {
			for _, hdr := range headers {
				f, openErr := hdr.Open()
				if openErr != nil {
					t.Fatalf("open part: %v", openErr)
				}

				body, readErr := io.ReadAll(f)
				_ = f.Close()

				if readErr != nil {
					t.Fatalf("read part: %v", readErr)
				}

				sum := sha256.Sum256(body)
				parts = append(parts, received{
					field:       fieldName,
					filename:    hdr.Filename,
					contentType: hdr.Header.Get("Content-Type"),
					sha:         hex.EncodeToString(sum[:]),
					isFile:      true,
				})
			}
		}

		for fieldName, values := range req.MultipartForm.Value {
			for _, value := range values {
				parts = append(parts, received{field: fieldName, value: value})
			}
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	wantSha := sha256.Sum256(fileBytes)
	wantSha2 := sha256.Sum256([]byte("inline-bytes"))

	p := New()
	_, err := p.Execute(context.Background(), provider.Input{
		Step: &model.Step{Name: "send", File: talesPath},
		Request: map[string]cty.Value{
			"method": cty.StringVal("POST"),
			"url":    cty.StringVal(ts.URL),
			"body": cty.ObjectVal(map[string]cty.Value{
				"multipart": cty.TupleVal([]cty.Value{
					cty.ObjectVal(map[string]cty.Value{
						"kind":         cty.StringVal("file"),
						"field":        cty.StringVal("blob"),
						"path":         cty.StringVal("./blob.bin"),
						"filename":     cty.StringVal("blob.bin"),
						"content_type": cty.StringVal("application/octet-stream"),
					}),
					cty.ObjectVal(map[string]cty.Value{
						"kind":         cty.StringVal("file"),
						"field":        cty.StringVal("inline"),
						"content":      cty.StringVal("inline-bytes"),
						"filename":     cty.StringVal("inline.bin"),
						"content_type": cty.StringVal("application/octet-stream"),
					}),
					cty.ObjectVal(map[string]cty.Value{
						"kind":  cty.StringVal("field"),
						"name":  cty.StringVal("description"),
						"value": cty.StringVal("user upload"),
					}),
				}),
			}),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(parts) != 3 {
		t.Fatalf("want 3 parts received, got %d: %#v", len(parts), parts)
	}

	var blob, inline, desc *received

	for i := range parts {
		switch parts[i].field {
		case "blob":
			blob = &parts[i]
		case "inline":
			inline = &parts[i]
		case "description":
			desc = &parts[i]
		}
	}

	if blob == nil || !blob.isFile || blob.filename != "blob.bin" {
		t.Fatalf("blob part not received correctly: %#v", blob)
	}

	if blob.sha != hex.EncodeToString(wantSha[:]) {
		t.Fatalf("blob bytes mangled: got sha %s want %s", blob.sha, hex.EncodeToString(wantSha[:]))
	}

	if blob.contentType != "application/octet-stream" {
		t.Fatalf("blob content-type lost: %s", blob.contentType)
	}

	if inline == nil || !inline.isFile || inline.sha != hex.EncodeToString(wantSha2[:]) {
		t.Fatalf("inline part not received correctly: %#v", inline)
	}

	if desc == nil || desc.isFile || desc.value != "user upload" {
		t.Fatalf("description field not received correctly: %#v", desc)
	}
}

func TestHTTPProviderMultipartRejectsFileWithoutSource(t *testing.T) {
	t.Parallel()

	p := New()
	_, err := p.Execute(context.Background(), provider.Input{
		Step: &model.Step{Name: "send", File: "/tmp/x.tales"},
		Request: map[string]cty.Value{
			"method": cty.StringVal("POST"),
			"url":    cty.StringVal("http://example.test"),
			"body": cty.ObjectVal(map[string]cty.Value{
				"multipart": cty.TupleVal([]cty.Value{
					cty.ObjectVal(map[string]cty.Value{
						"kind":  cty.StringVal("file"),
						"field": cty.StringVal("blob"),
					}),
				}),
			}),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one of path or content") {
		t.Fatalf("expected file source error, got %v", err)
	}
}
