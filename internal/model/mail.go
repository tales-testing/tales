package model

// MailCall holds parsed data for a mail provider step. It mirrors the lazy,
// expression-bearing shape of SQLCall: every field is an unevaluated
// Expression resolved by the runtime before the provider runs.
type MailCall struct {
	Target  Expression
	Message *MailMessage
}

// MailMessage is the message block of a mail step. From and the recipient
// lists drive the envelope; Headers carries arbitrary custom headers; Text and
// HTML select the MIME body shape; Attachments preserve declaration order.
type MailMessage struct {
	From    Expression
	To      Expression // list(string)
	Cc      Expression // list(string)
	Bcc     Expression // list(string)
	Subject Expression
	Headers Expression // map(string)
	Text    Expression
	HTML    Expression

	Attachments []MailAttachment
}

// MailAttachment is one attachment block. Exactly one of Path / Content is set
// (enforced at parse time); Filename is required; ContentType is optional and
// inferred from the filename when omitted.
type MailAttachment struct {
	Filename    Expression
	ContentType Expression
	Path        Expression
	Content     Expression
}
