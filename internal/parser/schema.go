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
	Provider   string                `hcl:",label"`
	Name       string                `hcl:",label"`
	DependsOn  []string              `hcl:"depends_on,optional"`
	When       hcl.Expression        `hcl:"when,optional"`
	Request    *requestBlock         `hcl:"request,block"`
	Expect     *expectBlock          `hcl:"expect,block"`
	Response   *expectBlock          `hcl:"response,block"`
	Capture    *captureBlock         `hcl:"capture,block"`
	Retry      *retryBlock           `hcl:"retry,block"`
	CallName   hcl.Expression        `hcl:"name,optional"`
	Inputs     hcl.Expression        `hcl:"inputs,optional"`
	Platform   hcl.Expression        `hcl:"platform,optional"`
	Target     hcl.Expression        `hcl:"target,optional"`
	Launch     *mobileLaunchBlock    `hcl:"launch,block"`
	Terminate  *mobileTerminateBlock `hcl:"terminate,block"`
	Actions    *mobileActionsBlock   `hcl:"actions,block"`
	SkipIf     []skipBlock           `hcl:"skip_if,block"`
	SkipUnless []skipBlock           `hcl:"skip_unless,block"`
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
	Body hcl.Body `hcl:",remain"`
}

type authBlock struct {
	Basic []basicAuthBlock `hcl:"basic,block"`
}

type basicAuthBlock struct {
	Body hcl.Body `hcl:",remain"`
}

type expectBlock struct {
	Status     hcl.Expression  `hcl:"status,optional"`
	Headers    hcl.Expression  `hcl:"headers,optional"`
	JSON       hcl.Expression  `hcl:"json,optional"`
	Body       hcl.Expression  `hcl:"body,optional"`
	Strict     hcl.Expression  `hcl:"strict,optional"`
	Visible    []*visibleBlock `hcl:"visible,block"`
	NotVisible []*visibleBlock `hcl:"not_visible,block"`
	Text       []*valueBlock   `hcl:"text,block"`
	Value      []*valueBlock   `hcl:"value,block"`
	Enabled    []*stateBlock   `hcl:"enabled,block"`
	Disabled   []*stateBlock   `hcl:"disabled,block"`
}

type visibleBlock struct {
	ID       hcl.Expression `hcl:"id,optional"`
	Timeout  hcl.Expression `hcl:"timeout,optional"`
	Interval hcl.Expression `hcl:"interval,optional"`
}

type valueBlock struct {
	ID       hcl.Expression `hcl:"id,optional"`
	Value    hcl.Expression `hcl:"value,optional"`
	Timeout  hcl.Expression `hcl:"timeout,optional"`
	Interval hcl.Expression `hcl:"interval,optional"`
}

type stateBlock struct {
	ID       hcl.Expression `hcl:"id,optional"`
	Timeout  hcl.Expression `hcl:"timeout,optional"`
	Interval hcl.Expression `hcl:"interval,optional"`
}

type mobileLaunchBlock struct {
	ClearState hcl.Expression `hcl:"clear_state,optional"`
}

type mobileTerminateBlock struct{}

type mobileActionsBlock struct {
	Body hcl.Body `hcl:",remain"`
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
