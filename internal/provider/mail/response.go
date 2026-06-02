package mail

import (
	"github.com/tales-testing/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
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
		"accepted": stringList(result.Accepted),
		"rejected": rejectedList,
	})

	jsonValue := cty.ObjectVal(map[string]cty.Value{
		"accepted":   cty.BoolVal(len(result.Accepted) > 0),
		"message_id": cty.StringVal(messageID),
		"recipients": recipients,
		"protocol":   cty.StringVal(protocol),
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
		"protocol":    cty.StringVal(target.Protocol),
		"target":      cty.StringVal(target.Name),
		"from":        cty.StringVal(exec.From),
		"to":          stringList(exec.To),
		"cc":          stringList(exec.Cc),
		"bcc":         stringList(exec.Bcc),
		"subject":     cty.StringVal(exec.Subject),
		"message_id":  cty.StringVal(exec.MessageID),
		"headers":     stringMap(exec.Headers),
		"attachments": attachmentsList,
	}
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
