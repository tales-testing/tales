package model

// LoadCall is the parsed payload of a load step. The HTTP request to
// replay is captured in Request (reuses the standard HTTP Request
// shape minus multipart, which the load provider rejects). Run carries
// the execution-mode parameters (duration / requests, concurrency,
// rate, warmup).
type LoadCall struct {
	Request *Request
	Run     *LoadRun
}

// LoadRun describes how the load runner should drive the request.
// Exactly one of Duration / Requests is set after decoding.
// Concurrency defaults to 1 when absent; Rate (RPS limit) and Warmup
// stay zero / Empty when not provided.
type LoadRun struct {
	Duration    Expression
	Requests    Expression
	Concurrency Expression
	Rate        Expression
	Warmup      Expression
}
