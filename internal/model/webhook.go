package model

// WebhookCall is the parsed payload of a webhook provider step. Exactly one of
// Start, Wait, or Stop is set (validated at decode time). Target is the
// step-level `target = ...` expression consumed by the wait operation; the stop
// operation carries its own target inside the Stop block.
type WebhookCall struct {
	Start  *WebhookStart
	Wait   *WebhookWait
	Stop   *WebhookStop
	Target Expression
	Expect *WebhookExpect
}

// WebhookStart configures a temporary local HTTP receiver. Address binds the
// listener (default 127.0.0.1:0, a free port). Path is the route that records
// incoming requests. The Public* fields control how the externally reachable
// callback URL is built (see runtime/webhook.go buildWebhookURL).
type WebhookStart struct {
	Address      Expression
	Path         Expression
	PublicURL    Expression
	PublicScheme Expression
	PublicHost   Expression
	PublicPort   Expression
	MaxBodySize  Expression
}

// WebhookWait blocks until at least Count requests have reached the receiver
// referenced by the step-level target, or Timeout elapses.
type WebhookWait struct {
	Timeout Expression
	Count   Expression
}

// WebhookStop shuts down the receiver referenced by Target.
type WebhookStop struct {
	Target Expression
}

// WebhookExpect holds the webhook-specific assertions evaluated by the runtime
// against the received request. Both blocks are optional.
type WebhookExpect struct {
	Request *WebhookRequestExpect
	HMAC    *WebhookHMACExpect
}

// WebhookRequestExpect asserts on the received request. Absent fields are not
// asserted; headers/query/json match partially (only declared keys).
type WebhookRequestExpect struct {
	Method  Expression
	Path    Expression
	Headers Expression
	Query   Expression
	JSON    Expression
	Body    Expression
}

// WebhookHMACExpect verifies a signature header on the received request. The
// signature material is parsed out of Header per Format (a literal pattern with
// {timestamp} / {signature} placeholders), the Payload expression is evaluated
// with a `timestamp` variable in scope, and an HMAC over it with Secret and
// Algorithm is compared in constant time against the parsed signature.
type WebhookHMACExpect struct {
	Header             Expression
	Secret             Expression
	Algorithm          Expression
	Format             Expression
	Payload            Expression
	TimestampTolerance Expression
	TimestampRequired  Expression
}
