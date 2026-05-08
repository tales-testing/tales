package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

type keywordFlowProvider struct {
	mu        sync.Mutex
	calls     []string
	userEmail string
}

func (p *keywordFlowProvider) Type() string {
	return "http"
}

func (p *keywordFlowProvider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	_ = ctx

	p.mu.Lock()
	p.calls = append(p.calls, input.Step.Name)
	p.mu.Unlock()

	switch input.Step.Name {
	case "create_user":
		email, err := nestedString(input.Request, "json", "email")
		if err != nil {
			return nil, err
		}

		p.mu.Lock()
		p.userEmail = email
		p.mu.Unlock()

		return okOutput(201, input.Request, map[string]cty.Value{
			"status":  cty.NumberIntVal(201),
			"headers": cty.ObjectVal(map[string]cty.Value{"Content-Type": cty.StringVal("application/json")}),
			"body":    cty.StringVal(`{"id":"u1","email":"` + email + `"}`),
			"json": cty.ObjectVal(map[string]cty.Value{
				"id":    cty.StringVal("u1"),
				"email": cty.StringVal(email),
			}),
		}), nil
	case "auth_user":
		email, err := nestedString(input.Request, "json", "email")
		if err != nil {
			return nil, err
		}

		token := "token-" + email

		return okOutput(200, input.Request, map[string]cty.Value{
			"status":  cty.NumberIntVal(200),
			"headers": cty.ObjectVal(map[string]cty.Value{"Content-Type": cty.StringVal("application/json")}),
			"body":    cty.StringVal(`{"access_token":"` + token + `"}`),
			"json": cty.ObjectVal(map[string]cty.Value{
				"access_token": cty.StringVal(token),
			}),
		}), nil
	case "create_blog_post":
		auth, err := nestedString(input.Request, "headers", "Authorization")
		if err != nil {
			return nil, err
		}

		p.mu.Lock()
		expected := "Bearer token-" + p.userEmail
		p.mu.Unlock()

		if auth != expected {
			return okOutput(401, input.Request, map[string]cty.Value{
				"status":  cty.NumberIntVal(401),
				"headers": cty.EmptyObjectVal,
				"body":    cty.StringVal(`{"error":"invalid token"}`),
				"json":    cty.ObjectVal(map[string]cty.Value{"error": cty.StringVal("invalid token")}),
			}), nil
		}

		return okOutput(201, input.Request, map[string]cty.Value{
			"status":  cty.NumberIntVal(201),
			"headers": cty.ObjectVal(map[string]cty.Value{"Content-Type": cty.StringVal("application/json")}),
			"body":    cty.StringVal(`{"id":"p1"}`),
			"json": cty.ObjectVal(map[string]cty.Value{
				"id": cty.StringVal("p1"),
			}),
		}), nil
	default:
		return nil, fmt.Errorf("unexpected step %q", input.Step.Name)
	}
}

func okOutput(status int, request map[string]cty.Value, response map[string]cty.Value) *provider.Output {
	return &provider.Output{
		Duration:   0,
		StatusCode: status,
		Request:    request,
		Response:   response,
	}
}

func nestedString(container map[string]cty.Value, root, key string) (string, error) {
	rootValue, ok := container[root]
	if !ok {
		return "", fmt.Errorf("missing %s", root)
	}

	if !rootValue.Type().IsObjectType() && !rootValue.Type().IsMapType() {
		return "", fmt.Errorf("%s must be an object", root)
	}

	valueMap := rootValue.AsValueMap()
	value, ok := valueMap[key]
	if !ok {
		return "", fmt.Errorf("missing %s.%s", root, key)
	}

	if value.Type() != cty.String {
		return "", fmt.Errorf("%s.%s must be string", root, key)
	}

	return value.AsString(), nil
}

func TestKeywordStepProducesOutputUsableByNextStep(t *testing.T) {
	t.Parallel()

	httpProvider := &keywordFlowProvider{}
	runner := NewRunner(provider.NewRegistry(httpProvider))

	suite := &model.Suite{
		Keywords: map[string]*model.Keyword{
			"authenticate": {
				Name: "authenticate",
				Inputs: map[string]model.Expression{
					"email":    expr("string"),
					"password": expr("string"),
				},
				Steps: []*model.Step{
					{
						Provider: "http",
						Name:     "auth_user",
						Request: &model.Request{
							Method: expr(`"POST"`),
							URL:    expr(`"http://example.test/auth"`),
							JSON:   expr(`{ email = input.email, password = input.password }`),
						},
						Expect: &model.Expect{Status: expr("200")},
					},
				},
				Outputs: map[string]model.Expression{
					"token": expr("result.auth_user.response.json.access_token"),
				},
			},
		},
		Scenarios: []*model.Scenario{buildKeywordScenario()},
	}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1234, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}

	scenarioResult := result.Scenarios[0]
	if scenarioResult.Status != report.StatusPass {
		t.Fatalf("expected pass, got %s, failure=%v", scenarioResult.Status, scenarioResult.Failure)
	}

	if len(scenarioResult.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(scenarioResult.Steps))
	}

	var foundKeywordStep bool

	for _, stepResult := range scenarioResult.Steps {
		if stepResult.Name == "auth" {
			foundKeywordStep = true
			if stepResult.Status != report.StatusPass {
				t.Fatalf("keyword step must pass, got %s", stepResult.Status)
			}
		}
	}

	if !foundKeywordStep {
		t.Fatalf("keyword step result not found")
	}
}

func TestKeywordStepUnknownKeywordFailsScenario(t *testing.T) {
	t.Parallel()

	httpProvider := &keywordFlowProvider{}
	runner := NewRunner(provider.NewRegistry(httpProvider))
	suite := &model.Suite{
		Keywords: map[string]*model.Keyword{},
		Scenarios: []*model.Scenario{{
			Name: "missing keyword",
			File: "test.tales",
			Steps: []*model.Step{{
				Provider: "keyword",
				Name:     "auth",
				Keyword: &model.KeywordCall{
					Name:   expr(`"does_not_exist"`),
					Inputs: expr(`{}`),
				},
			}},
		}},
	}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}

	scenarioResult := result.Scenarios[0]
	if scenarioResult.Status != report.StatusFail {
		t.Fatalf("scenario should fail, got %s", scenarioResult.Status)
	}

	if scenarioResult.Failure == nil {
		t.Fatalf("expected failure details")
	}

	if !strings.Contains(scenarioResult.Failure.Message, "unknown keyword") {
		t.Fatalf("unexpected failure message: %s", scenarioResult.Failure.Message)
	}
}

func TestKeywordStepNameCollisionFailsScenario(t *testing.T) {
	t.Parallel()

	httpProvider := &keywordFlowProvider{}
	runner := NewRunner(provider.NewRegistry(httpProvider))
	suite := &model.Suite{
		Keywords: map[string]*model.Keyword{
			"authenticate": {
				Name: "authenticate",
				Steps: []*model.Step{
					{
						Provider: "http",
						Name:     "create_user",
						Request: &model.Request{
							Method: expr(`"POST"`),
							URL:    expr(`"http://example.test/users"`),
							JSON:   expr(`{ email = input.email, password = input.password }`),
						},
						Expect: &model.Expect{Status: expr("201")},
					},
				},
			},
		},
		Scenarios: []*model.Scenario{buildKeywordScenario()},
	}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}

	scenarioResult := result.Scenarios[0]
	if scenarioResult.Status != report.StatusFail {
		t.Fatalf("scenario should fail, got %s", scenarioResult.Status)
	}

	if scenarioResult.Failure == nil {
		t.Fatalf("expected failure details")
	}

	if !strings.Contains(scenarioResult.Failure.Message, "collides with existing scenario step name") {
		t.Fatalf("unexpected failure message: %s", scenarioResult.Failure.Message)
	}
}

func TestKeywordUnknownExternalDependencyFailsGraphValidation(t *testing.T) {
	t.Parallel()

	httpProvider := &keywordFlowProvider{}
	runner := NewRunner(provider.NewRegistry(httpProvider))
	suite := &model.Suite{
		Keywords: map[string]*model.Keyword{
			"authenticate": {
				Name: "authenticate",
				Steps: []*model.Step{
					{
						Provider: "http",
						Name:     "auth_user",
						Request: &model.Request{
							Method: expr(`"POST"`),
							URL:    expr(`"http://example.test/auth"`),
							JSON:   expr(`{ email = result.missing_step.email, password = input.password }`),
						},
						Expect: &model.Expect{Status: expr("200")},
					},
				},
				Outputs: map[string]model.Expression{
					"token": expr(`"token"`),
				},
			},
		},
		Scenarios: []*model.Scenario{buildKeywordScenario()},
	}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}

	scenarioResult := result.Scenarios[0]
	if scenarioResult.Status != report.StatusFail {
		t.Fatalf("scenario should fail, got %s", scenarioResult.Status)
	}

	if scenarioResult.Failure == nil {
		t.Fatalf("expected failure details")
	}

	if !strings.Contains(scenarioResult.Failure.Message, "references unknown dependency") {
		t.Fatalf("unexpected failure message: %s", scenarioResult.Failure.Message)
	}
}

func buildKeywordScenario() *model.Scenario {
	return &model.Scenario{
		Name: "keyword flow",
		File: "test.tales",
		Steps: []*model.Step{
			{
				Provider: "http",
				Name:     "create_user",
				Request: &model.Request{
					Method: expr(`"POST"`),
					URL:    expr(`"http://example.test/users"`),
					JSON:   expr(`{ email = "user@example.com", password = "Passw0rd!" }`),
				},
				Expect: &model.Expect{Status: expr("201")},
				Capture: map[string]model.Expression{
					"email":    expr("request.json.email"),
					"password": expr("request.json.password"),
				},
			},
			{
				Provider: "keyword",
				Name:     "auth",
				Keyword: &model.KeywordCall{
					Name: expr(`"authenticate"`),
					Inputs: expr(`{
						email    = result.create_user.email
						password = result.create_user.password
					}`),
				},
			},
			{
				Provider: "http",
				Name:     "create_blog_post",
				Request: &model.Request{
					Method: expr(`"POST"`),
					URL:    expr(`"http://example.test/blog/posts"`),
					Headers: expr(`{
						Authorization = "Bearer ${result.auth.token}"
					}`),
					JSON: expr(`{
						title   = "keyword post"
						content = "hello"
					}`),
				},
				Expect: &model.Expect{Status: expr("201")},
			},
		},
	}
}
