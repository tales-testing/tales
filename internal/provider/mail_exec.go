package provider

// MailExecution carries the resolved data for one mail step ready to be sent
// by the mail provider. The runner evaluates the step's message-block
// expressions into these concrete Go values (and derives the Message-ID)
// before invoking the provider, mirroring SQLExecution.
type MailExecution struct {
	Target  string
	From    string
	To      []string
	Cc      []string
	Bcc     []string
	Subject string
	// Headers carries user-supplied custom headers. Keys are canonicalized
	// and the map is rendered in sorted key order so the wire representation
	// is deterministic.
	Headers map[string]string
	Text    string
	HTML    string

	Attachments []MailAttachmentData

	// MessageID is the fully-formed Message-ID header value (including angle
	// brackets), derived deterministically by the runtime from the seed unless
	// the user supplied one explicitly.
	MessageID string
}

// MailAttachmentData is one resolved attachment. Exactly one of Path /
// Content provides the payload: Path is resolved by the provider relative to
// the owning .tales file, Content is the already-evaluated inline string.
// Filename is required; ContentType is optional and inferred from the filename
// when empty.
type MailAttachmentData struct {
	Filename    string
	ContentType string
	Path        string
	Content     string
	HasContent  bool
}
