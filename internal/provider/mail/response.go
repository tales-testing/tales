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
	fieldStage     = "stage"
	fieldStatus    = "status_code"
	fieldEnhanced  = "enhanced_status_code"
	fieldMessage   = "message"
	fieldAddress   = "address"
)

// toSendResponse builds the cty response map exposed to expect/capture under
// the "json" key, mirroring the SQL provider's response shape. SMTP/LMTP
// rejections are first-class assertable fields, not provider errors.
func toSendResponse(messageID string, result *Result, protocol string) map[string]cty.Value {
	recipients := cty.ObjectVal(map[string]cty.Value{
		fieldAccepted: stringList(result.Accepted),
		fieldRejected: rejectionList(result.Rejected),
	})

	jsonValue := map[string]cty.Value{
		fieldAccepted:  cty.BoolVal(len(result.Accepted) > 0),
		fieldRejected:  cty.BoolVal(result.Transaction != nil || len(result.Rejected) > 0),
		fieldMessageID: cty.StringVal(messageID),
		fieldProtocol:  cty.StringVal(protocol),
		"recipients":   recipients,
	}

	primary := primaryRejection(result)
	if primary != nil {
		jsonValue[fieldStage] = cty.StringVal(primary.Stage)
		jsonValue[fieldStatus] = numberOrNull(primary.Status)
		jsonValue[fieldEnhanced] = cty.StringVal(primary.Enhanced)
		jsonValue[fieldMessage] = cty.StringVal(primary.Message)
	} else {
		jsonValue[fieldStage] = cty.StringVal(stageAccepted)
		jsonValue[fieldStatus] = cty.NullVal(cty.Number)
		jsonValue[fieldEnhanced] = cty.StringVal("")
		jsonValue[fieldMessage] = cty.StringVal("")
	}

	return map[string]cty.Value{"json": cty.ObjectVal(jsonValue)}
}

// primaryRejection picks the representative top-level rejection: a
// transaction-level reply (MAIL FROM / DATA / SMTP final) wins, otherwise the
// first per-recipient rejection, otherwise nil (fully accepted).
func primaryRejection(result *Result) *Rejection {
	if result.Transaction != nil {
		return result.Transaction
	}

	if len(result.Rejected) > 0 {
		return &result.Rejected[0]
	}

	return nil
}

func rejectionList(rejections []Rejection) cty.Value {
	if len(rejections) == 0 {
		return cty.EmptyTupleVal
	}

	out := make([]cty.Value, 0, len(rejections))
	for _, r := range rejections {
		out = append(out, cty.ObjectVal(map[string]cty.Value{
			fieldAddress:  cty.StringVal(r.Address),
			fieldStage:    cty.StringVal(r.Stage),
			fieldStatus:   numberOrNull(r.Status),
			fieldEnhanced: cty.StringVal(r.Enhanced),
			fieldMessage:  cty.StringVal(r.Message),
		}))
	}

	return cty.TupleVal(out)
}

// numberOrNull renders a status code, using null for the zero (absent) value.
func numberOrNull(status int) cty.Value {
	if status == 0 {
		return cty.NullVal(cty.Number)
	}

	return cty.NumberIntVal(int64(status))
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
