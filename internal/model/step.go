package model

// Step is one execution unit handled by a provider.
type Step struct {
	Provider  string
	Name      string
	File      string
	Line      int
	DependsOn []string
	When      Expression
	Request   *Request
	Expect    *Expect
	Capture   map[string]Expression
	Keyword   *KeywordCall
}

// Request holds provider-agnostic request inputs.
type Request struct {
	Method  Expression
	URL     Expression
	Headers Expression
	Query   Expression
	JSON    Expression
	Body    Expression
	Timeout Expression
}

// Expect holds provider-agnostic assertions.
type Expect struct {
	Status  Expression
	Headers Expression
	JSON    Expression
	Strict  Expression
}

// KeywordCall is data for a keyword provider step.
type KeywordCall struct {
	Name   Expression
	Inputs Expression
}
