package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// captureProvider records every request it receives so tests can assert that
// vars were rendered consistently across all interpolation sites in the step.
type captureProvider struct {
	mu       sync.Mutex
	requests []map[string]cty.Value
}

func (p *captureProvider) Type() string {
	return "http"
}

func (p *captureProvider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	_ = ctx

	p.mu.Lock()
	p.requests = append(p.requests, input.Request)
	p.mu.Unlock()

	return &provider.Output{
		StatusCode: 200,
		Request:    input.Request,
		Response: map[string]cty.Value{
			"status":  cty.NumberIntVal(200),
			"headers": cty.EmptyObjectVal,
			"body":    cty.StringVal(`{"ok":true}`),
			"json": cty.ObjectVal(map[string]cty.Value{
				"ok": cty.True,
			}),
		},
	}, nil
}

func (p *captureProvider) lastRequest() map[string]cty.Value {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.requests) == 0 {
		return nil
	}

	return p.requests[len(p.requests)-1]
}

func runOneStep(t *testing.T, step *model.Step) (*captureProvider, *report.SuiteResult) {
	t.Helper()

	prov := &captureProvider{}
	runner := NewRunner(provider.NewRegistry(prov))

	result, err := runner.Run(context.Background(), &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "vars",
		File:  "test.tales",
		Steps: []*model.Step{step},
	}}}, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return prov, result
}

func TestStepVarsEvaluatedInOrder(t *testing.T) {
	t.Parallel()

	step := newHTTPStep("send")
	step.Capture = map[string]model.Expression{}
	step.Vars = []model.StepVar{
		{Name: "a", Expr: expr(`"hello"`)},
		{Name: "b", Expr: expr(`"${vars.a} world"`)},
	}
	step.Request.Headers = expr(`{ X-Greeting = vars.b }`)
	step.Expect = nil

	prov, result := runOneStep(t, step)

	if result.Scenarios[0].Status != report.StatusPass {
		t.Fatalf("scenario should pass: %#v", result.Scenarios[0].Failure)
	}

	got := prov.lastRequest()
	if got == nil {
		t.Fatal("no request was captured")
	}

	headers := got["headers"]
	if headers.Type() == cty.NilType {
		t.Fatalf("headers missing in request: %#v", got)
	}

	greeting := headers.GetAttr("X-Greeting")
	if greeting.AsString() != "hello world" {
		t.Fatalf("expected vars.b to interpolate vars.a, got %q", greeting.AsString())
	}
}

func TestStepVarsTimestampReusedConsistently(t *testing.T) {
	t.Parallel()

	step := newHTTPStep("send")
	step.Capture = map[string]model.Expression{}
	step.Vars = []model.StepVar{
		{Name: "ts", Expr: expr(`now_unix()`)},
	}
	step.Request.Headers = expr(`{ X-First = "t=${vars.ts}", X-Second = "t=${vars.ts}" }`)
	step.Expect = nil

	prov, result := runOneStep(t, step)

	if result.Scenarios[0].Status != report.StatusPass {
		t.Fatalf("scenario should pass: %#v", result.Scenarios[0].Failure)
	}

	headers := prov.lastRequest()["headers"]
	first := headers.GetAttr("X-First").AsString()
	second := headers.GetAttr("X-Second").AsString()

	if first != second {
		t.Fatalf("now_unix() captured in a var must yield the same value at every interpolation site: %s vs %s", first, second)
	}
}

func TestStepVarsVisibleInRequestExpectCapture(t *testing.T) {
	t.Parallel()

	step := newHTTPStep("send")
	step.Vars = []model.StepVar{
		{Name: "marker", Expr: expr(`"abc"`)},
	}
	step.Request.Headers = expr(`{ X-Marker = vars.marker }`)
	step.Expect = &model.Expect{
		Status: expr(`200`),
	}
	step.Capture = map[string]model.Expression{
		"echoed": expr(`vars.marker`),
	}

	_, result := runOneStep(t, step)

	if result.Scenarios[0].Status != report.StatusPass {
		t.Fatalf("scenario should pass: %#v", result.Scenarios[0].Failure)
	}

	stepResult := result.Scenarios[0].Steps[0]
	if stepResult.Status != report.StatusPass {
		t.Fatalf("step should pass: %#v", stepResult.Failure)
	}
}

func TestStepVarsNotVisibleAcrossSteps(t *testing.T) {
	t.Parallel()

	first := newHTTPStep("first")
	first.Capture = map[string]model.Expression{}
	first.Vars = []model.StepVar{
		{Name: "shared", Expr: expr(`"value"`)},
	}

	second := newHTTPStep("second")
	second.Capture = map[string]model.Expression{}
	second.Request.Headers = expr(`{ X-Shared = vars.shared }`)

	prov := &captureProvider{}
	runner := NewRunner(provider.NewRegistry(prov))

	result, err := runner.Run(context.Background(), &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "cross",
		File:  "test.tales",
		Steps: []*model.Step{first, second},
	}}}, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Scenarios[0].Steps[0].Status != report.StatusPass {
		t.Fatalf("first step should pass: %#v", result.Scenarios[0].Steps[0].Failure)
	}

	secondResult := result.Scenarios[0].Steps[1]
	if secondResult.Status != report.StatusFail {
		t.Fatalf("second step should fail because vars.shared is not declared, got %s", secondResult.Status)
	}
}

func TestStepVarsEvaluationFailureSurfacesAsVarsError(t *testing.T) {
	t.Parallel()

	step := newHTTPStep("send")
	step.Capture = map[string]model.Expression{}
	step.Vars = []model.StepVar{
		{Name: "bad", Expr: expr(`hmac_sha256_hex("only-one-arg")`)},
	}
	step.Request.Headers = expr(`{ X-Bad = vars.bad }`)
	step.Expect = nil

	_, result := runOneStep(t, step)

	failure := result.Scenarios[0].Steps[0].Failure
	if failure == nil {
		t.Fatal("expected failure")
	}

	if failure.Kind != "vars" {
		t.Fatalf("expected vars-kind failure, got %q", failure.Kind)
	}

	if failure.Path != "bad" {
		t.Fatalf("expected failure path 'bad', got %q", failure.Path)
	}
}

func TestStepVarsVisibleInKeywordNameAndInputs(t *testing.T) {
	t.Parallel()

	httpProv := &keywordFlowProvider{}
	runner := NewRunner(provider.NewRegistry(httpProv))

	suite := &model.Suite{
		Keywords: map[string]*model.Keyword{
			"authenticate": {
				Name: "authenticate",
				Inputs: map[string]model.Expression{
					"email":    expr("string"),
					"password": expr("string"),
				},
				Steps: []*model.Step{{
					Provider: "http",
					Name:     "auth_user",
					Request: &model.Request{
						Method: expr(`"POST"`),
						URL:    expr(`"http://example.test/auth"`),
						Body:   bodyJSONExpr(`{ email = input.email, password = input.password }`),
					},
					Expect: &model.Expect{Status: expr("200")},
				}},
				Outputs: map[string]model.Expression{
					"token": expr("result.auth_user.response.json.access_token"),
				},
			},
		},
		Scenarios: []*model.Scenario{{
			Name: "vars in keyword call",
			File: "test.tales",
			Steps: []*model.Step{{
				Provider: "keyword",
				Name:     "auth",
				Vars: []model.StepVar{
					{Name: "kw", Expr: expr(`"authenticate"`)},
					{Name: "creds", Expr: expr(`{ email = "user@example.com", password = "Passw0rd!" }`)},
				},
				Keyword: &model.KeywordCall{
					Name:   expr(`vars.kw`),
					Inputs: expr(`vars.creds`),
				},
			}},
		}},
	}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}

	scenario := result.Scenarios[0]
	if scenario.Status != report.StatusPass {
		t.Fatalf("scenario should pass, got %s, failure=%v", scenario.Status, scenario.Failure)
	}

	if scenario.Steps[0].Status != report.StatusPass {
		t.Fatalf("keyword step should pass, got %s, failure=%#v", scenario.Steps[0].Status, scenario.Steps[0].Failure)
	}
}

func TestStepVarsHMACOverJSONBodyIsConsistent(t *testing.T) {
	t.Parallel()

	step := newHTTPStep("send")
	step.Capture = map[string]model.Expression{}
	step.Vars = []model.StepVar{
		{Name: "body", Expr: expr(`jsonencode({ id = "evt-1", type = "test" })`)},
		{Name: "sig", Expr: expr(`hmac_sha256_hex("topsecret", vars.body)`)},
	}
	step.Request.Headers = expr(`{ X-Signature = vars.sig }`)
	step.Request.Body = &model.RequestBody{
		Raw: expr(`vars.body`),
	}
	step.Expect = nil

	prov, result := runOneStep(t, step)

	if result.Scenarios[0].Status != report.StatusPass {
		t.Fatalf("scenario should pass: %#v", result.Scenarios[0].Failure)
	}

	got := prov.lastRequest()
	body := got["body"]

	if body.Type() == cty.NilType {
		t.Fatalf("request body missing: %#v", got)
	}

	rawBody := body.GetAttr("raw").AsString()
	if !strings.Contains(rawBody, `"id":"evt-1"`) {
		t.Fatalf("expected canonical JSON body, got %q", rawBody)
	}

	sig := got["headers"].GetAttr("X-Signature").AsString()
	if len(sig) != 64 {
		t.Fatalf("expected 64-char hex signature, got %d chars: %q", len(sig), sig)
	}
}
