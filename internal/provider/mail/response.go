package mail

import (
	"strings"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

// Response/metadata field names reused across the two builders.
const (
	fieldProtocol  = "protocol"
	fieldMessageID = "message_id"
	fieldAccepted  = "accepted"
	fieldRejected  = "rejected"
)

// toSendResponse builds the cty response map exposed to expect/capture under
// the "json" key, mirroring the SQL provider's response shape.
func toSendResponse(messageID string, result *Result, protocol string) map[string]cty.Value {
	rejected := make([]cty.Value, 0, len(result.Rejected))
	for _, r := range result.Rejected {
		rejected = append(rejected, cty.ObjectVal(map[string]cty.Value{
			"address": cty.StringVal(r.Address),
			"status":  cty.NumberIntVal(int64(r.Status)),
			"message": cty.StringVal(r.Message),
		}))
	}

	rejectedList := cty.EmptyTupleVal
	if len(rejected) > 0 {
		rejectedList = cty.TupleVal(rejected)
	}

	recipients := cty.ObjectVal(map[string]cty.Value{
		fieldAccepted: stringList(result.Accepted),
		fieldRejected: rejectedList,
	})

	jsonValue := cty.ObjectVal(map[string]cty.Value{
		fieldAccepted:  cty.BoolVal(len(result.Accepted) > 0),
		fieldMessageID: cty.StringVal(messageID),
		"recipients":   recipients,
		fieldProtocol:  cty.StringVal(protocol),
	})

	return map[string]cty.Value{"json": jsonValue}
}

// buildRequestMeta builds the report-facing request map. It carries only
// metadata: never the message body, the attachment bytes, or the password.
// Custom headers travel under the "headers" key so the runtime's masking
// redacts Authorization / Cookie / signature headers.
func buildRequestMeta(target Target, exec *provider.MailExecution, spec messageSpec) map[string]cty.Value {
	attachments := make([]cty.Value, 0, len(spec.Attachments))
	for _, att := range spec.Attachments {
		attachments = append(attachments, cty.ObjectVal(map[string]cty.Value{
			"filename":     cty.StringVal(att.Filename),
			"content_type": cty.StringVal(att.ContentType),
			"size":         cty.NumberIntVal(int64(len(att.Data))),
		}))
	}

	attachmentsList := cty.EmptyTupleVal
	if len(attachments) > 0 {
		attachmentsList = cty.TupleVal(attachments)
	}

	return map[string]cty.Value{
		fieldProtocol:  cty.StringVal(target.Protocol),
		"target":       cty.StringVal(target.Name),
		fieldFrom:      cty.StringVal(exec.From),
		"to":           stringList(exec.To),
		"cc":           stringList(exec.Cc),
		fieldBcc:       stringList(exec.Bcc),
		fieldSubject:   cty.StringVal(exec.Subject),
		fieldMessageID: cty.StringVal(exec.MessageID),
		"headers":      stringMap(nonReservedHeaders(exec.Headers)),
		"attachments":  attachmentsList,
	}
}

// nonReservedHeaders drops the headers that the MIME builder generates from
// explicit fields (From/To/Subject/Message-ID/...), so the report metadata
// matches the headers actually put on the wire rather than the user's raw map.
func nonReservedHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return headers
	}

	filtered := make(map[string]string, len(headers))

	for key, value := range headers {
		if _, reserved := reservedHeaders[strings.ToLower(key)]; reserved {
			continue
		}

		filtered[key] = value
	}

	return filtered
}

func stringList(values []string) cty.Value {
	if len(values) == 0 {
		return cty.ListValEmpty(cty.String)
	}

	out := make([]cty.Value, 0, len(values))
	for _, v := range values {
		out = append(out, cty.StringVal(v))
	}

	return cty.ListVal(out)
}

func stringMap(values map[string]string) cty.Value {
	if len(values) == 0 {
		return cty.MapValEmpty(cty.String)
	}

	out := make(map[string]cty.Value, len(values))
	for k, v := range values {
		out[k] = cty.StringVal(v)
	}

	return cty.MapVal(out)
}
