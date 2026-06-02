package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/tales-testing/tales/internal/model"
)

// mailProviderType is the provider label that triggers mail step decoding.
const mailProviderType = "mail"

// decodeMailStep builds a model.MailCall from a parsed step block whenever the
// step uses the mail provider. It validates the envelope (from + at least one
// recipient) and every attachment (filename + exactly one of path/content).
func decodeMailStep(path string, rs stepBlock) (*model.MailCall, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	if !exprIsSet(rs.Target) {
		diags = append(diags, diagError(
			"Missing mail target",
			"mail step must declare target = \"<name>\" referencing config.mail.targets.<name>.",
			nil,
		))
	}

	call := &model.MailCall{Target: expr(path, rs.Target)}

	if rs.Message == nil {
		diags = append(diags, diagError(
			"Missing mail message",
			"mail step must define a message { ... } block.",
			nil,
		))

		return call, diags
	}

	message, msgDiags := decodeMailMessage(path, rs.Message)
	diags = append(diags, msgDiags...)
	call.Message = message

	return call, diags
}

// decodeMailMessage converts the message schema block into the model and
// validates required fields.
func decodeMailMessage(path string, mb *messageBlock) (*model.MailMessage, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	if !exprIsSet(mb.From) {
		diags = append(diags, diagError(
			"Missing mail sender",
			"mail message must define from = \"<address>\".",
			nil,
		))
	}

	if !exprIsSet(mb.To) && !exprIsSet(mb.Cc) && !exprIsSet(mb.Bcc) {
		diags = append(diags, diagError(
			"Missing mail recipient",
			"mail message must define at least one recipient via to, cc or bcc.",
			nil,
		))
	}

	message := &model.MailMessage{
		From:    expr(path, mb.From),
		To:      expr(path, mb.To),
		Cc:      expr(path, mb.Cc),
		Bcc:     expr(path, mb.Bcc),
		Subject: expr(path, mb.Subject),
		Headers: expr(path, mb.Headers),
		Text:    expr(path, mb.Text),
		HTML:    expr(path, mb.HTML),
	}

	for i := range mb.Attachments {
		attachment, attDiags := decodeMailAttachment(path, mb.Attachments[i])
		diags = append(diags, attDiags...)
		message.Attachments = append(message.Attachments, attachment)
	}

	return message, diags
}

// decodeMailAttachment validates one attachment: filename is required and
// exactly one of path / content must be set.
func decodeMailAttachment(path string, ab attachmentBlock) (model.MailAttachment, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	if !exprIsSet(ab.Filename) {
		diags = append(diags, diagError(
			"Missing attachment filename",
			"mail attachment must define filename = \"<name>\".",
			nil,
		))
	}

	hasPath := exprIsSet(ab.Path)
	hasContent := exprIsSet(ab.Content)

	switch {
	case hasPath && hasContent:
		diags = append(diags, diagError(
			"Conflicting attachment source",
			"mail attachment must define exactly one of path or content.",
			nil,
		))
	case !hasPath && !hasContent:
		diags = append(diags, diagError(
			"Missing attachment source",
			"mail attachment must define exactly one of path or content.",
			nil,
		))
	}

	return model.MailAttachment{
		Filename:    expr(path, ab.Filename),
		ContentType: expr(path, ab.ContentType),
		Path:        expr(path, ab.Path),
		Content:     expr(path, ab.Content),
	}, diags
}

// looksLikeMailStep reports whether a step block carries any mail-specific
// block. Used to flag mail-only fields appearing on a non-mail provider step.
func looksLikeMailStep(rs stepBlock) bool {
	if rs.Provider == mailProviderType {
		return true
	}

	return rs.Message != nil
}

// decodeMailStepIfNeeded routes mail decoding similarly to
// decodeSQLStepIfNeeded: a mail provider step is fully decoded; a non-mail
// step that nonetheless carries a message block is rejected with a clear hint.
func decodeMailStepIfNeeded(path string, rs stepBlock, stepName string) (*model.MailCall, hcl.Diagnostics) {
	if rs.Provider == mailProviderType {
		return decodeMailStep(path, rs)
	}

	if !looksLikeMailStep(rs) {
		return nil, nil
	}

	return nil, hcl.Diagnostics{diagError(
		"Mail fields on non-mail step",
		fmt.Sprintf("Step %q uses a message block but its provider is %q; use provider \"mail\".", stepName, rs.Provider),
		nil,
	)}
}
