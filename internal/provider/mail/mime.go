package mail

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	nethttp "net/http"
	"net/textproto"
	"sort"
	"strings"
	"time"
)

// messageSpec is the resolved, header-facing message used to build MIME.
// Address fields carry their header rendering (display name + address); the
// envelope is computed separately. Bcc is intentionally absent — it must never
// appear in the message headers.
type messageSpec struct {
	From        string
	To          []string
	Cc          []string
	Subject     string
	Headers     map[string]string
	Text        string
	HTML        string
	Attachments []attachmentPayload
	MessageID   string
}

// attachmentPayload is one attachment with its bytes already loaded.
type attachmentPayload struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Lower-case field names shared between the reserved-header set and the report
// metadata, plus content/header constants used across the MIME builder.
const (
	fieldFrom    = "from"
	fieldBcc     = "bcc"
	fieldSubject = "subject"
	fieldDate    = "date"

	headerContentType = "Content-Type"
	headerCTE         = "Content-Transfer-Encoding"

	ctTextPlainUTF8 = "text/plain; charset=utf-8"
	ctTextHTMLUTF8  = "text/html; charset=utf-8"

	cteQuotedPrintable = "quoted-printable"
	cteBase64          = "base64"

	boundaryAlt   = "alt"
	boundaryMixed = "mixed"
)

// reservedHeaders are generated from explicit fields and must not be duplicated
// from the user headers map. Date is handled separately (user may override it).
var reservedHeaders = map[string]struct{}{
	fieldFrom: {}, "to": {}, "cc": {}, fieldBcc: {}, fieldSubject: {},
	"mime-version": {}, "content-type": {}, "content-transfer-encoding": {},
	"message-id": {}, fieldDate: {},
}

// buildMessage assembles a valid MIME message with CRLF line endings. now feeds
// the Date header (injectable for byte-exact tests).
func buildMessage(spec messageSpec, now time.Time) ([]byte, error) {
	bodyHeaders, body, err := buildBody(spec)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer

	writeHeader(&buf, "From", spec.From)

	if len(spec.To) > 0 {
		writeHeader(&buf, "To", strings.Join(spec.To, ", "))
	}

	if len(spec.Cc) > 0 {
		writeHeader(&buf, "Cc", strings.Join(spec.Cc, ", "))
	}

	writeHeader(&buf, "Subject", mime.QEncoding.Encode("utf-8", spec.Subject))
	writeHeader(&buf, "Date", resolveDate(spec.Headers, now))
	writeHeader(&buf, "Message-ID", spec.MessageID)
	writeHeader(&buf, "MIME-Version", "1.0")

	if err := writeCustomHeaders(&buf, spec.Headers); err != nil {
		return nil, err
	}

	for _, h := range bodyHeaders {
		writeHeader(&buf, h.name, h.value)
	}

	buf.WriteString("\r\n")
	buf.Write(body)

	return ensureTrailingCRLF(buf.Bytes()), nil
}

type headerField struct {
	name  string
	value string
}

// buildBody returns the content-level headers (Content-Type and optional CTE)
// plus the body bytes. For multipart bodies the CTE lives in each part.
func buildBody(spec messageSpec) ([]headerField, []byte, error) {
	hasText := spec.Text != ""
	hasHTML := spec.HTML != ""

	if len(spec.Attachments) == 0 {
		switch {
		case hasText && hasHTML:
			return alternativeBody(spec)
		case hasHTML:
			cte, body := encodeText(spec.HTML)

			return textHeaders(ctTextHTMLUTF8, cte), body, nil
		default:
			cte, body := encodeText(spec.Text)

			return textHeaders(ctTextPlainUTF8, cte), body, nil
		}
	}

	return mixedBody(spec)
}

// alternativeContent builds the multipart/alternative content type and body
// (text/plain then text/html), shared by the standalone and nested forms.
func alternativeContent(spec messageSpec) (string, []byte, error) {
	boundary := deriveBoundary(spec.MessageID, boundaryAlt)

	body, err := renderMultipart(boundary, []partSpec{
		textPart(ctTextPlainUTF8, spec.Text),
		textPart(ctTextHTMLUTF8, spec.HTML),
	})
	if err != nil {
		return "", nil, err
	}

	return fmt.Sprintf("multipart/alternative; boundary=%q", boundary), body, nil
}

// alternativeBody builds a standalone multipart/alternative body.
func alternativeBody(spec messageSpec) ([]headerField, []byte, error) {
	contentType, body, err := alternativeContent(spec)
	if err != nil {
		return nil, nil, err
	}

	return []headerField{{headerContentType, contentType}}, body, nil
}

// mixedBody builds a multipart/mixed body: an optional body part (text / html /
// alternative) followed by every attachment.
func mixedBody(spec messageSpec) ([]headerField, []byte, error) {
	boundary := deriveBoundary(spec.MessageID, boundaryMixed)

	parts := make([]partSpec, 0, len(spec.Attachments)+1)

	if bodyPart, ok, err := bodyAsPart(spec); err != nil {
		return nil, nil, err
	} else if ok {
		parts = append(parts, bodyPart)
	}

	for _, att := range spec.Attachments {
		parts = append(parts, attachmentPart(att))
	}

	body, err := renderMultipart(boundary, parts)
	if err != nil {
		return nil, nil, err
	}

	return []headerField{{headerContentType, fmt.Sprintf("multipart/mixed; boundary=%q", boundary)}}, body, nil
}

// bodyAsPart renders the message body as a single multipart part for inclusion
// inside multipart/mixed. ok is false when there is no text or html body.
func bodyAsPart(spec messageSpec) (partSpec, bool, error) {
	hasText := spec.Text != ""
	hasHTML := spec.HTML != ""

	switch {
	case hasText && hasHTML:
		contentType, body, err := alternativeContent(spec)
		if err != nil {
			return partSpec{}, false, err
		}

		return partSpec{header: textproto.MIMEHeader{headerContentType: {contentType}}, body: body}, true, nil
	case hasHTML:
		return textPart(ctTextHTMLUTF8, spec.HTML), true, nil
	case hasText:
		return textPart(ctTextPlainUTF8, spec.Text), true, nil
	default:
		return partSpec{}, false, nil
	}
}

type partSpec struct {
	header textproto.MIMEHeader
	body   []byte
}

// textPart builds a single text part, choosing a 7bit or quoted-printable
// transfer encoding based on the content.
func textPart(contentType, body string) partSpec {
	cte, encoded := encodeText(body)

	header := textproto.MIMEHeader{headerContentType: {contentType}}
	if cte != "" {
		header.Set(headerCTE, cte)
	}

	return partSpec{header: header, body: encoded}
}

// attachmentPart builds a base64-encoded attachment part with a
// Content-Disposition.
func attachmentPart(att attachmentPayload) partSpec {
	header := textproto.MIMEHeader{
		headerContentType:     {fmt.Sprintf("%s; name=%q", att.ContentType, att.Filename)},
		headerCTE:             {cteBase64},
		"Content-Disposition": {fmt.Sprintf("attachment; filename=%q", att.Filename)},
	}

	return partSpec{header: header, body: base64Wrap(att.Data)}
}

// renderMultipart writes parts under a fixed boundary using the stdlib writer
// (CRLF endings, sorted part-header keys, deterministic given the boundary).
func renderMultipart(boundary string, parts []partSpec) ([]byte, error) {
	var buf bytes.Buffer

	mw := multipart.NewWriter(&buf)
	if err := mw.SetBoundary(boundary); err != nil {
		return nil, fmt.Errorf("set multipart boundary: %w", err)
	}

	for _, p := range parts {
		pw, err := mw.CreatePart(p.header)
		if err != nil {
			return nil, fmt.Errorf("create mime part: %w", err)
		}

		if _, err := pw.Write(p.body); err != nil {
			return nil, fmt.Errorf("write mime part: %w", err)
		}
	}

	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}

	return buf.Bytes(), nil
}

func textHeaders(contentType, cte string) []headerField {
	headers := []headerField{{"Content-Type", contentType}}
	if cte != "" {
		headers = append(headers, headerField{"Content-Transfer-Encoding", cte})
	}

	return headers
}

// encodeText normalizes line endings to CRLF and returns the transfer encoding
// and encoded bytes: empty CTE (7bit) for ASCII, quoted-printable otherwise.
func encodeText(body string) (string, []byte) {
	if isASCII(body) {
		return "", []byte(normalizeCRLF(body))
	}

	var buf bytes.Buffer

	qw := quotedprintable.NewWriter(&buf)
	_, _ = qw.Write([]byte(normalizeCRLF(body)))
	_ = qw.Close()

	return cteQuotedPrintable, buf.Bytes()
}

// base64Wrap base64-encodes data with 76-column CRLF line wrapping.
func base64Wrap(data []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(data)

	const lineLen = 76

	var buf bytes.Buffer

	for len(encoded) > lineLen {
		buf.WriteString(encoded[:lineLen])
		buf.WriteString("\r\n")

		encoded = encoded[lineLen:]
	}

	buf.WriteString(encoded)
	buf.WriteString("\r\n")

	return buf.Bytes()
}

// writeCustomHeaders writes user headers in sorted key order, skipping reserved
// ones and rejecting header injection.
func writeCustomHeaders(buf *bytes.Buffer, headers map[string]string) error {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, key := range keys {
		if _, reserved := reservedHeaders[strings.ToLower(key)]; reserved {
			continue
		}

		value := headers[key]
		if strings.ContainsAny(key, "\r\n") || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("invalid header %q: contains a line break", key)
		}

		writeHeader(buf, textproto.CanonicalMIMEHeaderKey(key), value)
	}

	return nil
}

// resolveDate returns the user-supplied Date header (case-insensitive) or the
// generated one.
func resolveDate(headers map[string]string, now time.Time) string {
	for key, value := range headers {
		if strings.EqualFold(key, fieldDate) {
			return value
		}
	}

	return now.Format("Mon, 02 Jan 2006 15:04:05 -0700")
}

func writeHeader(buf *bytes.Buffer, name, value string) {
	buf.WriteString(name)
	buf.WriteString(": ")
	buf.WriteString(value)
	buf.WriteString("\r\n")
}

// deriveBoundary returns a deterministic MIME boundary from the Message-ID so
// the wire output is stable across runs with the same seed.
func deriveBoundary(messageID, kind string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + messageID))

	return fmt.Sprintf("tales-%s-%x", kind, sum[:8])
}

func normalizeCRLF(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

func ensureTrailingCRLF(data []byte) []byte {
	if bytes.HasSuffix(data, []byte("\r\n")) {
		return data
	}

	return append(data, '\r', '\n')
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}

	return true
}

// sniffContentType returns a best-effort content type for an attachment. The
// extension wins when available; otherwise the bytes are sniffed, defaulting to
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
