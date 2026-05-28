package model

import "time"

// Step is one execution unit handled by a provider.
type Step struct {
	Provider  string
	Name      string
	File      string
	Line      int
	DependsOn []string
	When      Expression
	Vars      []StepVar
	Request   *Request
	Expect    *Expect
	Capture   map[string]Expression
	Keyword   *KeywordCall
	Mobile    *MobileStep
	SQL       *SQLCall
	Browser   *BrowserStep
	Load      *LoadCall
	Retry     *Retry
	SkipRules []SkipRule
}

// StepVar is one step-local variable declared in a vars block. Vars are
// evaluated in declaration order before the provider runs and are scoped to
// the current step only — later vars and the request/expect/capture
// expressions can read them via vars.<name>.
type StepVar struct {
	Name string
	Expr Expression
}

// Request holds provider-agnostic request inputs.
type Request struct {
	Method  Expression
	URL     Expression
	Headers Expression
	Query   Expression
	Body    *RequestBody
	Timeout Expression
	Auth    *RequestAuth
}

// RequestBody holds one HTTP request body representation.
type RequestBody struct {
	JSON      Expression
	Form      Expression
	Raw       Expression
	Multipart *MultipartBody
}

// MultipartBody is a multipart/form-data payload. Parts preserve the
// declaration order from the .tales file so the wire representation is
// deterministic regardless of map iteration.
type MultipartBody struct {
	Parts []MultipartPart
}

// MultipartPart is one part in a multipart body. Exactly one of File / Field
// is set; the runtime serializes each part via mime/multipart.
type MultipartPart struct {
	File  *MultipartFilePart
	Field *MultipartFieldPart
}

// MultipartFilePart describes one file part. Field is the form name on the
// wire. Exactly one of Path / Content provides the payload bytes — Path is
// resolved at execution time relative to the .tales file, Content is
// evaluated as a string expression. Filename and ContentType are optional;
// when omitted, the provider derives them from Path / sniffs the content.
type MultipartFilePart struct {
	Field       Expression
	Path        Expression
	Content     Expression
	Filename    Expression
	ContentType Expression
}

// MultipartFieldPart describes a non-file form field.
type MultipartFieldPart struct {
	Name  Expression
	Value Expression
}

// RequestAuth holds authentication configuration for a request.
type RequestAuth struct {
	Basic *BasicAuth
}

// BasicAuth holds HTTP Basic Authentication expressions.
type BasicAuth struct {
	Username Expression
	Password Expression
}

// Expect holds provider-agnostic assertions.
type Expect struct {
	Status    Expression
	Headers   Expression
	JSON      Expression
	Body      Expression
	Strict    Expression
	Shortcuts []ExpectShortcut
}

// ExpectShortcut is a flat `key = matcher_or_value` assertion. The
// runtime resolves Name against the provider's Response map and runs
// MatchJSON. Load steps populate this with metric names like p95 or
// status_2xx_ratio so users can write the friendly form documented in
// the provider reference.
type ExpectShortcut struct {
	Name     string
	Expected Expression
}

// Retry controls repeated execution of a step until it passes.
type Retry struct {
	Attempts int
	Interval time.Duration
}

// KeywordCall is data for a keyword provider step.
type KeywordCall struct {
	Name   Expression
	Inputs Expression
}

// SQLCall holds parsed data for a sql provider step.
type SQLCall struct {
	Connection Expression
	Exec       *SQLOp
	Query      *SQLOp
}

// SQLOp is one SQL operation (exec or query) declared by a sql step.
type SQLOp struct {
	SQL  Expression
	Args Expression
}
