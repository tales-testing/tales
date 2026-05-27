package http

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	nethttp "net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

// encodeMultipartBody assembles a multipart/form-data payload from a cty
// tuple of parts produced by the runtime. The order of parts on the wire
// matches their declaration order in the .tales file, but the boundary
// chosen by mime/multipart is random per call — the full byte stream is
// therefore not bit-stable across runs even when inputs are identical.
// File parts using `path` are resolved relative to the .tales file owning
// the step. Callers must use the returned Content-Type header verbatim so
// the receiver can find the boundary.
func encodeMultipartBody(input provider.Input, value cty.Value) ([]byte, string, error) {
	if !value.IsKnown() {
		return nil, "", fmt.Errorf("request.body.multipart must be known")
	}

	if !value.Type().IsTupleType() && !value.Type().IsListType() {
		return nil, "", fmt.Errorf("request.body.multipart must be a list or tuple of parts")
	}

	var buf bytes.Buffer

	writer := multipart.NewWriter(&buf)

	for i, part := range value.AsValueSlice() {
		if err := writeMultipartPart(input, writer, i, part); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}

	return buf.Bytes(), writer.FormDataContentType(), nil
}

func writeMultipartPart(input provider.Input, writer *multipart.Writer, index int, part cty.Value) error {
	if part.IsNull() || !part.Type().IsObjectType() {
		return fmt.Errorf("multipart part %d must be an object", index)
	}

	attrs := part.AsValueMap()

	kindVal, ok := attrs["kind"]
	if !ok || kindVal.Type() != cty.String {
		return fmt.Errorf("multipart part %d has no kind", index)
	}

	switch kindVal.AsString() {
	case "file":
		return writeMultipartFile(input, writer, index, attrs)
	case "field":
		return writeMultipartField(writer, index, attrs)
	}

	return fmt.Errorf("multipart part %d has unknown kind %q", index, kindVal.AsString())
}

func writeMultipartField(writer *multipart.Writer, index int, attrs map[string]cty.Value) error {
	name, err := requiredString(attrs, "name", index)
	if err != nil {
		return err
	}

	value, err := requiredString(attrs, "value", index)
	if err != nil {
		return err
	}

	w, err := writer.CreateFormField(name)
	if err != nil {
		return fmt.Errorf("create multipart field %q: %w", name, err)
	}

	if _, err := io.WriteString(w, value); err != nil {
		return fmt.Errorf("write multipart field %q: %w", name, err)
	}

	return nil
}

func writeMultipartFile(input provider.Input, writer *multipart.Writer, index int, attrs map[string]cty.Value) error {
	field, err := requiredString(attrs, "field", index)
	if err != nil {
		return err
	}

	pathStr, hasPath, err := optionalString(attrs, "path", index)
	if err != nil {
		return err
	}

	contentStr, hasContent, err := optionalString(attrs, "content", index)
	if err != nil {
		return err
	}

	if hasPath == hasContent {
		// load-time validation already rejects this; defensive check in
		// case a programmatic caller bypasses the parser.
		return fmt.Errorf("multipart part %d (field %q) must declare exactly one of path or content", index, field)
	}

	filename, _, err := optionalString(attrs, "filename", index)
	if err != nil {
		return err
	}

	contentType, _, err := optionalString(attrs, "content_type", index)
	if err != nil {
		return err
	}

	payload, fallbackExt, resolvedFilename, err := loadMultipartFileBytes(input, hasPath, pathStr, contentStr, field, filename)
	if err != nil {
		return fmt.Errorf("multipart part %d (field %q): %w", index, field, err)
	}

	filename = resolvedFilename

	if contentType == "" {
		contentType = sniffContentType(fallbackExt, payload)
	}

	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeMultipartParam(field), escapeMultipartParam(filename)))
	header.Set("Content-Type", contentType)

	w, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("create multipart file %q: %w", field, err)
	}

	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("write multipart file %q: %w", field, err)
	}

	return nil
}

// loadMultipartFileBytes returns the payload bytes for a multipart file
// part along with a defaulted filename and (when known) the extension used
// for Content-Type sniffing. When hasPath is true the file is read from
// disk; otherwise the inline content is used verbatim.
func loadMultipartFileBytes(input provider.Input, hasPath bool, pathStr, contentStr, field, filename string) ([]byte, string, string, error) {
	if !hasPath {
		if filename == "" {
			filename = field
		}

		return []byte(contentStr), "", filename, nil
	}

	resolved, err := resolveMultipartPath(input, pathStr)
	if err != nil {
		return nil, "", "", err
	}

	data, err := os.ReadFile(resolved) //nolint:gosec // G304: path is resolved against the .tales file's directory; user-supplied by design
	if err != nil {
		return nil, "", "", fmt.Errorf("read %q: %w", pathStr, err)
	}

	if filename == "" {
		filename = filepath.Base(resolved)
	}

	return data, filepath.Ext(resolved), filename, nil
}

// resolveMultipartPath returns the absolute path of a multipart file source.
// Absolute paths are accepted as-is; relative paths are joined to the
// directory of the .tales file declaring the step, matching the user's
// mental model of fixture paths sitting next to the suite.
func resolveMultipartPath(input provider.Input, path string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}

	if filepath.IsAbs(path) {
		return path, nil
	}

	if input.Step == nil || input.Step.File == "" {
		return "", fmt.Errorf("cannot resolve relative path %q: step file is unknown", path)
	}

	baseDir := filepath.Dir(input.Step.File)

	return filepath.Join(baseDir, path), nil
}

// sniffContentType returns a best-effort content type for a file part. The
// extension wins when available (so `cat.png` keeps `image/png` even if the
// bytes happen to look text-y); otherwise we fall back to http.DetectContentType
// via mime.TypeByExtension's defaults, ultimately defaulting to
// application/octet-stream.
func sniffContentType(ext string, payload []byte) string {
	if ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}

	if len(payload) > 0 {
		if t := detectContentTypeFromBytes(payload); t != "" {
			return t
		}
	}

	return "application/octet-stream"
}

// detectContentTypeFromBytes wraps http.DetectContentType so the caller gets
// a bare MIME type without the trailing "; charset=..." suffix the sniffer
// appends for text/*.
func detectContentTypeFromBytes(payload []byte) string {
	const maxSample = 512

	sample := payload
	if len(sample) > maxSample {
		sample = sample[:maxSample]
	}

	t := nethttp.DetectContentType(sample)
	if idx := strings.IndexByte(t, ';'); idx > 0 {
		t = t[:idx]
	}

	return strings.TrimSpace(t)
}

// multipartParamEscaper mirrors the stdlib mime/multipart writer's
// escapeQuotes helper (see Go's mime/multipart/writer.go): only backslashes
// and double quotes are escaped inside the quoted-string parameter form
// used by Content-Disposition per RFC 2183 / RFC 7578. Using fmt's %q
// verb here would emit Go-string escapes (e.g. \xNN, \n) that are not
// valid quoted-pair sequences and confuse strict HTTP servers.
var multipartParamEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeMultipartParam(s string) string {
	return multipartParamEscaper.Replace(s)
}

func requiredString(attrs map[string]cty.Value, key string, index int) (string, error) {
	val, ok := attrs[key]
	if !ok || val.IsNull() {
		return "", fmt.Errorf("multipart part %d is missing required attribute %q", index, key)
	}

	if val.Type() != cty.String {
		return "", fmt.Errorf("multipart part %d attribute %q must be a string", index, key)
	}

	return val.AsString(), nil
}

func optionalString(attrs map[string]cty.Value, key string, index int) (string, bool, error) {
	val, ok := attrs[key]
	if !ok || val.IsNull() {
		return "", false, nil
	}

	if val.Type() != cty.String {
		return "", false, fmt.Errorf("multipart part %d attribute %q must be a string", index, key)
	}

	return val.AsString(), true, nil
}
