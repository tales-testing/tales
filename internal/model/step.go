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
	Request   *Request
	Expect    *Expect
	Capture   map[string]Expression
	Keyword   *KeywordCall
	Mobile    *MobileStep
	Retry     *Retry
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
	JSON Expression
	Form Expression
	Raw  Expression
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
	Status  Expression
	Headers Expression
	JSON    Expression
	Body    Expression
	Strict  Expression
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
