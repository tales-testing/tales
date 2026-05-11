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
	Name      string        `hcl:",label"`
	Tags      []string      `hcl:"tags,optional"`
	Steps     []stepBlock   `hcl:"step,block"`
	Cases     []stepBlock   `hcl:"case,block"`
	Teardowns []teardownDef `hcl:"teardown,block"`
}

type teardownDef struct {
	Steps []stepBlock `hcl:"step,block"`
	Cases []stepBlock `hcl:"case,block"`
}

type stepBlock struct {
	Provider  string         `hcl:",label"`
	Name      string         `hcl:",label"`
	DependsOn []string       `hcl:"depends_on,optional"`
	When      hcl.Expression `hcl:"when,optional"`
	Request   *requestBlock  `hcl:"request,block"`
	Expect    *expectBlock   `hcl:"expect,block"`
	Response  *expectBlock   `hcl:"response,block"`
	Capture   *captureBlock  `hcl:"capture,block"`
	Retry     *retryBlock    `hcl:"retry,block"`
	CallName  hcl.Expression `hcl:"name,optional"`
	Inputs    hcl.Expression `hcl:"inputs,optional"`
}

type retryBlock struct {
	Attempts hcl.Expression `hcl:"attempts,optional"`
	Interval hcl.Expression `hcl:"interval,optional"`
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
	Body hcl.Body `hcl:",remain"`
}

type authBlock struct {
	Basic []basicAuthBlock `hcl:"basic,block"`
}

type basicAuthBlock struct {
	Body hcl.Body `hcl:",remain"`
}

type expectBlock struct {
	Status  hcl.Expression `hcl:"status,optional"`
	Headers hcl.Expression `hcl:"headers,optional"`
	JSON    hcl.Expression `hcl:"json,optional"`
	Body    hcl.Expression `hcl:"body,optional"`
	Strict  hcl.Expression `hcl:"strict,optional"`
}

type captureBlock struct {
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
