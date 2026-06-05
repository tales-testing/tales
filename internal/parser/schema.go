package parser

import "github.com/hashicorp/hcl/v2"

type fileSchema struct {
	Version    int              `hcl:"version,optional"`
	Config     []configBlock    `hcl:"config,block"`
	Generators []generatorBlock `hcl:"generator,block"`
	Scenarios  []scenarioBlock  `hcl:"scenario,block"`
	Keywords   []keywordBlock   `hcl:"keyword,block"`
}

type configBlock struct {
	Body hcl.Body `hcl:",remain"`
}

type generatorBlock struct {
	Type string   `hcl:",label"`
	Name string   `hcl:",label"`
	Body hcl.Body `hcl:",remain"`
}

type scenarioBlock struct {
	Name       string        `hcl:",label"`
	Tags       []string      `hcl:"tags,optional"`
	Steps      []stepBlock   `hcl:"step,block"`
	Cases      []stepBlock   `hcl:"case,block"`
	Teardowns  []teardownDef `hcl:"teardown,block"`
	SkipIf     []skipBlock   `hcl:"skip_if,block"`
	SkipUnless []skipBlock   `hcl:"skip_unless,block"`
}

type teardownDef struct {
	Steps []stepBlock `hcl:"step,block"`
	Cases []stepBlock `hcl:"case,block"`
}

type stepBlock struct {
	Provider     string                  `hcl:",label"`
	Name         string                  `hcl:",label"`
	DependsOn    []string                `hcl:"depends_on,optional"`
	When         hcl.Expression          `hcl:"when,optional"`
	Vars         *varsBlock              `hcl:"vars,block"`
	Request      *requestBlock           `hcl:"request,block"`
	Expect       *expectBlock            `hcl:"expect,block"`
	Response     *expectBlock            `hcl:"response,block"`
	Capture      *captureBlock           `hcl:"capture,block"`
	Save         *saveBlock              `hcl:"save,block"`
	Retry        *retryBlock             `hcl:"retry,block"`
	CallName     hcl.Expression          `hcl:"name,optional"`
	Inputs       hcl.Expression          `hcl:"inputs,optional"`
	Platform     hcl.Expression          `hcl:"platform,optional"`
	Target       hcl.Expression          `hcl:"target,optional"`
	Launch       *mobileLaunchBlock      `hcl:"launch,block"`
	Terminate    *mobileTerminateBlock   `hcl:"terminate,block"`
	Actions      *actionsBlock           `hcl:"actions,block"`
	Permissions  *mobilePermissionsBlock `hcl:"permissions,block"`
	Connection   hcl.Expression          `hcl:"connection,optional"`
	Exec         *sqlOpBlock             `hcl:"exec,block"`
	Query        *sqlOpBlock             `hcl:"query,block"`
	Message      *messageBlock           `hcl:"message,block"`
	HTTPReq      *requestBlock           `hcl:"http,block"`
	Run          *runBlock               `hcl:"run,block"`
	WebhookStart *webhookStartBlock      `hcl:"start,block"`
	WebhookWait  *webhookWaitBlock       `hcl:"wait,block"`
	WebhookStop  *webhookStopBlock       `hcl:"stop,block"`
	SkipIf       []skipBlock             `hcl:"skip_if,block"`
	SkipUnless   []skipBlock             `hcl:"skip_unless,block"`
}

// webhookStartBlock boots a temporary local HTTP receiver. Only path is
// required; the rest carry sensible defaults resolved at runtime.
type webhookStartBlock struct {
	Address      hcl.Expression `hcl:"address,optional"`
	Path         hcl.Expression `hcl:"path,optional"`
	PublicURL    hcl.Expression `hcl:"public_url,optional"`
	PublicScheme hcl.Expression `hcl:"public_scheme,optional"`
	PublicHost   hcl.Expression `hcl:"public_host,optional"`
	PublicPort   hcl.Expression `hcl:"public_port,optional"`
	MaxBodySize  hcl.Expression `hcl:"max_body_size,optional"`
}

// webhookWaitBlock blocks until enough requests reach the targeted receiver.
type webhookWaitBlock struct {
	Timeout hcl.Expression `hcl:"timeout,optional"`
	Count   hcl.Expression `hcl:"count,optional"`
}

// webhookStopBlock shuts down the targeted receiver.
type webhookStopBlock struct {
	Target hcl.Expression `hcl:"target,optional"`
}

// runBlock describes how a load step should drive its request. Exactly
// one of duration / requests must be set; the parser validates it.
type runBlock struct {
	Duration    hcl.Expression `hcl:"duration,optional"`
	Requests    hcl.Expression `hcl:"requests,optional"`
	Concurrency hcl.Expression `hcl:"concurrency,optional"`
	Rate        hcl.Expression `hcl:"rate,optional"`
	Warmup      hcl.Expression `hcl:"warmup,optional"`
}

type sqlOpBlock struct {
	SQL  hcl.Expression `hcl:"sql,optional"`
	Args hcl.Expression `hcl:"args,optional"`
}

// messageBlock is the message body of a mail step. Repeated same-type
// attachment blocks decode into the slice in declaration order (gohcl), so no
// hclsyntax walk is needed to preserve attachment ordering.
type messageBlock struct {
	From    hcl.Expression `hcl:"from,optional"`
	To      hcl.Expression `hcl:"to,optional"`
	Cc      hcl.Expression `hcl:"cc,optional"`
	Bcc     hcl.Expression `hcl:"bcc,optional"`
	Subject hcl.Expression `hcl:"subject,optional"`
	Headers hcl.Expression `hcl:"headers,optional"`
	Text    hcl.Expression `hcl:"text,optional"`
	HTML    hcl.Expression `hcl:"html,optional"`

	Attachments []attachmentBlock `hcl:"attachment,block"`
}

type attachmentBlock struct {
	Filename    hcl.Expression `hcl:"filename,optional"`
	ContentType hcl.Expression `hcl:"content_type,optional"`
	Path        hcl.Expression `hcl:"path,optional"`
	Content     hcl.Expression `hcl:"content,optional"`
}

type retryBlock struct {
	Attempts hcl.Expression `hcl:"attempts,optional"`
	Interval hcl.Expression `hcl:"interval,optional"`
}

type skipBlock struct {
	Body hcl.Body `hcl:",remain"`
}

type requestBlock struct {
	Method  hcl.Expression `hcl:"method,optional"`
	URL     hcl.Expression `hcl:"url,optional"`
	Headers hcl.Expression `hcl:"headers,optional"`
	Query   hcl.Expression `hcl:"query,optional"`
	Body    []bodyBlock    `hcl:"body,block"`
	Timeout hcl.Expression `hcl:"timeout,optional"`
	Auth    []authBlock    `hcl:"auth,block"`
}

type bodyBlock struct {
	JSON      hcl.Expression  `hcl:"json,optional"`
	Form      hcl.Expression  `hcl:"form,optional"`
	Raw       hcl.Expression  `hcl:"raw,optional"`
	Multipart *multipartBlock `hcl:"multipart,block"`
}

// multipartBlock is decoded manually below (decodeMultipartBlock) by walking
// hclsyntax.Body.Blocks so file / field children keep their declaration
// order on the wire — the deterministic ordering matters for callers that
// sign or hash the multipart payload.
type multipartBlock struct {
	Body hcl.Body `hcl:",remain"`
}

type authBlock struct {
	Basic []basicAuthBlock `hcl:"basic,block"`
}

type basicAuthBlock struct {
	Body hcl.Body `hcl:",remain"`
}

type expectBlock struct {
	Status     hcl.Expression    `hcl:"status,optional"`
	Headers    hcl.Expression    `hcl:"headers,optional"`
	JSON       hcl.Expression    `hcl:"json,optional"`
	Body       hcl.Expression    `hcl:"body,optional"`
	Strict     hcl.Expression    `hcl:"strict,optional"`
	Visible    []*visibleBlock   `hcl:"visible,block"`
	NotVisible []*visibleBlock   `hcl:"not_visible,block"`
	Text       []*valueBlock     `hcl:"text,block"`
	Value      []*valueBlock     `hcl:"value,block"`
	Enabled    []*stateBlock     `hcl:"enabled,block"`
	Disabled   []*stateBlock     `hcl:"disabled,block"`
	Attribute  []*attributeBlock `hcl:"attribute,block"`
	URL        []*urlBlock       `hcl:"url,block"`
	Title      []*titleBlock     `hcl:"title,block"`
	WebPerf    []*webPerfBlock   `hcl:"web_perf,block"`
	// Remainder carries any attribute not matched by a typed field above.
	// Non-load providers must verify it is empty (validateExpectExtras);
	// the load provider walks it to resolve shortcut attributes such as
	// `p95 = lt("200ms")` or `error_ratio = lte(0.01)`.
	Remainder hcl.Body `hcl:",remain"`
}

// webPerfBlock holds the per-metric assertions inside an
// `expect { web_perf { ... } }` declaration. Attributes are decoded
// dynamically by the browser parser so a single block carries any
// supported metric alias (fcp, lcp, cls, load, dom_content_loaded,
// resources_count, transfer_size, …).
type webPerfBlock struct {
	Body hcl.Body `hcl:",remain"`
}

// visibleBlock describes a visibility expectation. ID is used by the mobile
// provider; Selector is used by the browser provider. The decoder validates
// that exactly the expected locator is set per provider.
type visibleBlock struct {
	ID       hcl.Expression `hcl:"id,optional"`
	Selector hcl.Expression `hcl:"selector,optional"`
	Timeout  hcl.Expression `hcl:"timeout,optional"`
	Interval hcl.Expression `hcl:"interval,optional"`
}

type valueBlock struct {
	ID       hcl.Expression `hcl:"id,optional"`
	Selector hcl.Expression `hcl:"selector,optional"`
	Value    hcl.Expression `hcl:"value,optional"`
	Timeout  hcl.Expression `hcl:"timeout,optional"`
	Interval hcl.Expression `hcl:"interval,optional"`
}

type stateBlock struct {
	ID       hcl.Expression `hcl:"id,optional"`
	Selector hcl.Expression `hcl:"selector,optional"`
	Timeout  hcl.Expression `hcl:"timeout,optional"`
	Interval hcl.Expression `hcl:"interval,optional"`
}

// attributeBlock is the browser-specific expect block matching an element's
// DOM attribute value. Mobile rejects it at decode time.
type attributeBlock struct {
	Selector hcl.Expression `hcl:"selector,optional"`
	Name     hcl.Expression `hcl:"name,optional"`
	Value    hcl.Expression `hcl:"value,optional"`
	Timeout  hcl.Expression `hcl:"timeout,optional"`
	Interval hcl.Expression `hcl:"interval,optional"`
}

// urlBlock is the browser-specific expect block matching the document URL.
type urlBlock struct {
	Value    hcl.Expression `hcl:"value,optional"`
	Timeout  hcl.Expression `hcl:"timeout,optional"`
	Interval hcl.Expression `hcl:"interval,optional"`
}

// titleBlock is the browser-specific expect block matching the document title.
type titleBlock struct {
	Value    hcl.Expression `hcl:"value,optional"`
	Timeout  hcl.Expression `hcl:"timeout,optional"`
	Interval hcl.Expression `hcl:"interval,optional"`
}

type mobileLaunchBlock struct {
	ClearState hcl.Expression `hcl:"clear_state,optional"`
}

type mobileTerminateBlock struct{}

// actionsBlock holds the raw body of a step's `actions { ... }` block. The
// provider-specific decoder (mobile / browser) walks the body in source order
// to preserve declaration order across action kinds.
type actionsBlock struct {
	Body hcl.Body `hcl:",remain"`
}

type mobilePermissionsBlock struct {
	Body hcl.Body `hcl:",remain"`
}

type captureBlock struct {
	Body hcl.Body `hcl:",remain"`
}

// saveBlock persists part of an HTTP response to the scenario workspace.
// V1 exposes body = "<path>"; gohcl rejects unknown attributes.
type saveBlock struct {
	Body hcl.Expression `hcl:"body,optional"`
}

type varsBlock struct {
	Body hcl.Body `hcl:",remain"`
}

type keywordBlock struct {
	Name        string         `hcl:",label"`
	InputsBlock *namedExprBody `hcl:"inputs,block"`
	Steps       []stepBlock    `hcl:"step,block"`
	Cases       []stepBlock    `hcl:"case,block"`
	Outputs     *namedExprBody `hcl:"outputs,block"`
}

type namedExprBody struct {
	Body hcl.Body `hcl:",remain"`
}
