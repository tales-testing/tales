// Package mail implements the "mail" provider: it injects an email message
// into an application under test over SMTP or LMTP. It is a test-ingestion
// tool, not a mailbox client — verification of the resulting behavior is done
// through the HTTP / SQL / browser / mobile providers.
package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tales-testing/tales/internal/provider"
)

const providerTypeMail = "mail"

// Provider executes step "mail". V1 opens a fresh connection per step and
// closes it inside Execute, so it holds no cached state and does not implement
// io.Closer.
type Provider struct {
	// now is the clock used for the Date header. Overridable in tests for
	// byte-exact MIME assertions; defaults to time.Now.
	now func() time.Time
	// senders maps a protocol to its implementation. Overridable in tests.
	smtpSender Sender
	lmtpSender Sender
}

// Option configures the provider.
type Option func(*Provider)

// New creates a mail provider with the production SMTP / LMTP senders.
func New(opts ...Option) *Provider {
	p := &Provider{
		now:        time.Now,
		smtpSender: smtpSender{},
		lmtpSender: lmtpSender{},
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Type returns the provider type label "mail".
func (p *Provider) Type() string {
	return providerTypeMail
}

// Sender transmits a fully-formed MIME message to a target and reports which
// envelope recipients were accepted or rejected.
type Sender interface {
	Send(ctx context.Context, target Target, envelope Envelope, raw []byte) (*Result, error)
}

// Envelope is the SMTP/LMTP envelope: a single sender and the deduped set of
// recipients (to ∪ cc ∪ bcc).
type Envelope struct {
	From       string
	Recipients []string
}

// Result reports the outcome of a send. Accepted lists recipients delivered to;
// Rejected lists per-recipient negative replies (RCPT, or per-recipient LMTP
// DATA). Transaction carries a transaction-level negative reply (MAIL FROM, the
// DATA command, or the single SMTP final response) that is not tied to one
// recipient. None of these are provider errors: they are assertable protocol
// outcomes.
type Result struct {
	Accepted    []string
	Rejected    []Rejection
	Transaction *Rejection
}

// Rejection is one sanitized SMTP/LMTP negative reply. Address is empty for
// transaction-level stages (mail_from, data, message).
type Rejection struct {
	Address  string
	Stage    string
	Status   int
	Enhanced string
	Message  string
}

// Execute resolves the target, builds the MIME message, sends it, and shapes
// the provider Output.
func (p *Provider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	if input.Mail == nil {
		return nil, fmt.Errorf("mail step is missing execution data")
	}

	exec := input.Mail

	target, err := resolveTarget(input.Config, exec.Target)
	if err != nil {
		return nil, err
	}

	envelope, err := buildEnvelope(exec)
	if err != nil {
		return nil, err
	}

	spec, err := p.buildSpec(input, exec)
	if err != nil {
		return nil, err
	}

	raw, err := buildMessage(spec, p.now())
	if err != nil {
		return nil, fmt.Errorf("build mail message: %w", err)
	}

	sender := p.senderFor(target.Protocol)
	if sender == nil {
		return nil, fmt.Errorf("unsupported mail protocol %q; supported protocols: smtp, lmtp", target.Protocol)
	}

	start := time.Now()

	result, err := sender.Send(ctx, target, envelope, raw)
	if err != nil {
		// Only transport / runtime failures reach here (sanitized so the
		// password never leaks). Protocol-level SMTP/LMTP rejections are not
		// errors: they are carried in result and asserted via expect.
		return nil, sanitizeSendError(target, err)
	}

	output := &provider.Output{
		Request:  buildRequestMeta(target, exec, spec),
		Response: toSendResponse(exec.MessageID, result, target.Protocol),
		Duration: time.Since(start),
	}

	return output, nil
}

func (p *Provider) senderFor(protocol string) Sender {
	switch protocol {
	case protocolSMTP:
		return p.smtpSender
	case protocolLMTP:
		return p.lmtpSender
	default:
		return nil
	}
}

// buildEnvelope parses every address and computes the deduped recipient set in
// to, cc, bcc order. The envelope uses the bare address form (no display name).
func buildEnvelope(exec *provider.MailExecution) (Envelope, error) {
	from, err := mail.ParseAddress(exec.From)
	if err != nil {
		return Envelope{}, fmt.Errorf("invalid sender address %q", exec.From)
	}

	seen := make(map[string]struct{})

	recipients := make([]string, 0, len(exec.To)+len(exec.Cc)+len(exec.Bcc))

	for _, group := range [][]string{exec.To, exec.Cc, exec.Bcc} {
		for _, raw := range group {
			addr, err := mail.ParseAddress(raw)
			if err != nil {
				return Envelope{}, fmt.Errorf("invalid recipient address %q", raw)
			}

			if _, ok := seen[addr.Address]; ok {
				continue
			}

			seen[addr.Address] = struct{}{}

			recipients = append(recipients, addr.Address)
		}
	}

	if len(recipients) == 0 {
		return Envelope{}, fmt.Errorf("mail message has no recipients")
	}

	return Envelope{From: from.Address, Recipients: recipients}, nil
}

// buildSpec renders the header-facing message specification, loading and
// content-typing every attachment.
func (p *Provider) buildSpec(input provider.Input, exec *provider.MailExecution) (messageSpec, error) {
	if exec.Text == "" && exec.HTML == "" && len(exec.Attachments) == 0 {
		return messageSpec{}, errors.New("mail message must define at least one of text, html or attachment")
	}

	spec := messageSpec{
		From:      renderAddress(exec.From),
		To:        renderAddresses(exec.To),
		Cc:        renderAddresses(exec.Cc),
		Subject:   exec.Subject,
		Headers:   exec.Headers,
		Text:      exec.Text,
		HTML:      exec.HTML,
		MessageID: exec.MessageID,
	}

	for i := range exec.Attachments {
		payload, err := p.loadAttachment(input, exec.Attachments[i])
		if err != nil {
			return messageSpec{}, err
		}

		spec.Attachments = append(spec.Attachments, payload)
	}

	return spec, nil
}

func (p *Provider) loadAttachment(input provider.Input, att provider.MailAttachmentData) (attachmentPayload, error) {
	var data []byte

	if att.HasContent {
		data = []byte(att.Content)
	} else {
		resolved, err := resolveAttachmentPath(input, att.Path)
		if err != nil {
			return attachmentPayload{}, err
		}

		//nolint:gosec // G304: attachment paths are author-controlled test fixtures, resolved relative to the .tales file.
		raw, err := os.ReadFile(resolved)
		if err != nil {
			return attachmentPayload{}, fmt.Errorf("read attachment %q: %w", att.Filename, err)
		}

		data = raw
	}

	contentType := att.ContentType
	if contentType == "" {
		contentType = sniffContentType(filepath.Ext(att.Filename), data)
	}

	return attachmentPayload{Filename: att.Filename, ContentType: contentType, Data: data}, nil
}

// resolveAttachmentPath returns the absolute path of an attachment source.
// Absolute paths are accepted as-is; relative paths are joined to the
// directory of the .tales file declaring the step.
func resolveAttachmentPath(input provider.Input, path string) (string, error) {
	if path == "" {
		return "", errors.New("attachment path is empty")
	}

	if filepath.IsAbs(path) {
		return path, nil
	}

	if input.Step == nil || input.Step.File == "" {
		return "", fmt.Errorf("cannot resolve relative attachment path %q: step file is unknown", path)
	}

	return filepath.Join(filepath.Dir(input.Step.File), path), nil
}

// renderAddress canonicalizes one address for header rendering, falling back to
// the raw value when it cannot be parsed (the envelope step already validated
// it, so this is defensive).
func renderAddress(raw string) string {
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return strings.TrimSpace(raw)
	}

	return addr.String()
}

func renderAddresses(raws []string) []string {
	out := make([]string, 0, len(raws))
	for _, raw := range raws {
		out = append(out, renderAddress(raw))
	}

	return out
}
