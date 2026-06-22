package provider

import (
	"context"
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
)

// Input is provider execution input.
type Input struct {
	Scenario string
	Step     *model.Step
	Phase    string
	Attempt  int
	Config   map[string]cty.Value
	Request  map[string]cty.Value
	Expect   map[string]cty.Value
	Mobile   *MobileExecution
	SQL      *SQLExecution
	Mail     *MailExecution
	Browser  *BrowserExecution
	Load     *LoadExecution
	Webhook  *WebhookExecution
	File     *FileExecution
	Exec     *ExecExecution
	RPC      *RPCExecution
	Timeout  time.Duration
}

// ExecExecution carries the resolved data for one exec step. The runtime
// evaluates the step's expressions and resolves the working directory; the
// provider resolves the command against the roots, builds the process
// environment, runs the program directly (never via a shell) and captures the
// streams. AllowExec is checked by the provider before any spawn.
type ExecExecution struct {
	// Command is the program as written (bare name, ./relative, or absolute);
	// the provider resolves it against Workdir / ProjectDir per the command
	// resolution policy.
	Command string
	Args    []string
	// Env holds the user-supplied environment overlaid on the base
	// environment selected by EnvMode ("minimal" or "inherit").
	Env     map[string]string
	EnvMode string
	Stdin   string
	Timeout time.Duration
	// SandboxMode is "process" (soft sandbox) or "docker" (reserved, not
	// implemented). Network is advisory in process mode.
	SandboxMode string
	Network     bool
	// Workdir is the absolute working directory the process runs in.
	// ProjectDir is the project root used only for command resolution.
	Workdir    string
	ProjectDir string
	// ArtifactsDir is where stdout.txt / stderr.txt / metadata.json /
	// stdout.json are written. MaxOutput caps each captured stream in bytes.
	ArtifactsDir string
	MaxOutput    int64
}

// FileExecution carries the resolved data for one file step. Path is absolute
// and already resolved against the scenario's allowed roots by the runtime.
// The Need* flags tell the provider which reads are required so a missing file
// or an unreadable form (binary when text is wanted, invalid JSON) only fails
// when the step actually depends on it.
type FileExecution struct {
	Path     string
	NeedSize bool
	NeedHash bool
	NeedText bool
	NeedJSON bool
}

// WebhookExecution carries the resolved parameters for one webhook step ready
// to be executed by the webhook provider. The runner evaluates the step's
// start / wait / stop expressions into these concrete Go values before invoking
// the provider. Operation is one of "start", "wait", or "stop"; only the fields
// relevant to that operation are populated.
type WebhookExecution struct {
	Operation string

	// start
	ID           string
	Address      string
	Path         string
	PublicURL    string
	PublicScheme string
	PublicHost   string
	PublicPort   int
	MaxBodySize  int64

	// wait / stop
	Target  string
	Timeout time.Duration
	Count   int
}

// LoadExecution carries the resolved execution parameters for a load
// step. Mode is one of "duration" or "requests"; the field matching
// the mode is populated, the other stays zero. Rate is requests per
// second (0 = unlimited). Warmup is the duration spent priming the
// transport before measurement begins.
type LoadExecution struct {
	Mode        string
	Duration    time.Duration
	Requests    int
	Concurrency int
	Rate        float64
	Warmup      time.Duration
}

// SQLExecution carries the resolved data for one sql step ready to be
// executed by the SQL provider. The runner evaluates step expressions into
// these concrete Go values before invoking the provider.
type SQLExecution struct {
	Connection string
	Mode       string // "exec" or "query"
	SQL        string
	Args       []any
}

// RPCExecution carries the resolved data for one rpc step ready to be
// executed by the rpc provider. The runner evaluates step expressions into
// these concrete Go values; the provider does descriptor resolution and
// dynamic Protobuf encoding internally so no protobuf types leak across this
// boundary. HeadersOverride / MetadataOverride hold the step-level overlay
// merged on top of the target's defaults inside the provider.
type RPCExecution struct {
	Target           string
	Service          string
	Method           string
	Message          map[string]cty.Value
	HeadersOverride  map[string]string
	MetadataOverride map[string]string
	Timeout          time.Duration
	ArtifactsDir     string
}

// Output is provider execution output.
type Output struct {
	Request  map[string]cty.Value
	Response map[string]cty.Value
	// RawBody carries the exact, unmodified response body bytes. The HTTP
	// provider sets it so binary downloads (HTTP `save` / response.download)
	// bypass go-cty's NFC string normalization, which would otherwise mutate
	// binary payloads exposed through Response["body"]. Providers without a
	// byte body leave it nil.
	RawBody       []byte
	Duration      time.Duration
	StatusCode    int
	ActionResults []ActionResult
}

// ActionResult is a provider-agnostic record of one UI action executed
// within a step. The runtime converts it into a report.ActionResult after
// the provider returns. Providers that do not emit actions leave this slice
// nil; HTTP and keyword providers are unaffected.
//
// Secure actions MUST carry Value == "***" — providers mask before
// constructing the result.
type ActionResult struct {
	Index      int
	Kind       string
	Label      string
	SelectorID string
	Secure     bool
	Value      string
	Status     string
	Duration   time.Duration
	Screenshot string
	Hierarchy  string
	Err        error
	StartedAt  time.Time
}

// Provider executes one step.
type Provider interface {
	Type() string
	Execute(ctx context.Context, input Input) (*Output, error)
}

// Registry maps provider type to implementation.
type Registry struct {
	items map[string]Provider
}

// NewRegistry creates registry.
func NewRegistry(providers ...Provider) *Registry {
	items := make(map[string]Provider, len(providers))
	for _, p := range providers {
		items[p.Type()] = p
	}

	return &Registry{items: items}
}

// Get provider by type.
func (r *Registry) Get(providerType string) (Provider, bool) {
	p, ok := r.items[providerType]

	return p, ok
}

// All returns every registered provider. The order is not stable.
func (r *Registry) All() []Provider {
	out := make([]Provider, 0, len(r.items))
	for _, p := range r.items {
		out = append(out, p)
	}

	return out
}
