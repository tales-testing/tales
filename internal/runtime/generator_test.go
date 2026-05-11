package runtime

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	faker "github.com/euskadi31/go-faker"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/hyperxlab/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

func TestEmailGeneratorUsesDeterministicFaker(t *testing.T) {
	t.Parallel()

	params := map[string]cty.Value{
		"prefix": cty.StringVal("test-"),
		"domain": cty.StringVal("example.test"),
	}
	parts := []string{"scenario", "step", "request.json", "user_email"}

	first, err := runGenerator("email", params, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate first email: %v", err)
	}

	second, err := runGenerator("email", params, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate second email: %v", err)
	}

	otherSeed, err := runGenerator("email", params, newGeneratorRandom(5678, parts...))
	if err != nil {
		t.Fatalf("generate other-seed email: %v", err)
	}

	if first.AsString() != second.AsString() {
		t.Fatalf("same seed generated different emails: %q vs %q", first.AsString(), second.AsString())
	}

	if first.AsString() == otherSeed.AsString() {
		t.Fatalf("different seed generated same email: %q", first.AsString())
	}

	if !regexp.MustCompile(`^test-[a-z]+[.\-]?[a-z]*@example\.test$`).MatchString(first.AsString()) {
		t.Fatalf("email does not look like faker output with configured prefix/domain: %q", first.AsString())
	}
}

func TestPasswordGeneratorDefaultConstraints(t *testing.T) {
	t.Parallel()

	value, err := runGenerator("password", map[string]cty.Value{}, newGeneratorRandom(1234, "scenario", "step", "request.json", "password"))
	if err != nil {
		t.Fatalf("generate password: %v", err)
	}

	defaults := faker.DefaultPasswordOptions()
	password := value.AsString()
	if len(password) != defaults.Length {
		t.Fatalf("password length=%d", len(password))
	}

	assertPasswordConstraints(t, password, defaults)
}

func TestPasswordGeneratorCustomConstraints(t *testing.T) {
	t.Parallel()

	config := faker.PasswordOptions{
		Length:     24,
		MinUpper:   3,
		MinLower:   4,
		MinDigit:   5,
		MinSpecial: 2,
		Specials:   "!@",
	}

	value, err := runGenerator("password", passwordParams(config), newGeneratorRandom(1234, "scenario", "step", "request.json", "password"))
	if err != nil {
		t.Fatalf("generate password: %v", err)
	}

	assertPasswordConstraints(t, value.AsString(), config)
}

func TestPasswordGeneratorInvalidConfigs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params map[string]cty.Value
		want   string
	}{
		{
			name:   "non-positive length",
			params: map[string]cty.Value{"length": cty.NumberIntVal(0)},
			want:   "password length must be > 0",
		},
		{
			name:   "negative minimum",
			params: map[string]cty.Value{"min_upper": cty.NumberIntVal(-1)},
			want:   "password minimum counts must be >= 0",
		},
		{
			name: "impossible required length",
			params: map[string]cty.Value{
				"length":      cty.NumberIntVal(3),
				"min_upper":   cty.NumberIntVal(1),
				"min_lower":   cty.NumberIntVal(1),
				"min_digit":   cty.NumberIntVal(1),
				"min_special": cty.NumberIntVal(1),
			},
			want: "password length is smaller than the sum of minimums",
		},
		{
			name: "empty specials",
			params: map[string]cty.Value{
				"min_special": cty.NumberIntVal(1),
				"specials":    cty.StringVal(""),
			},
			want: "password specials must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := runGenerator("password", tt.params, newGeneratorRandom(1234, "scenario", "step", "request.json", "password"))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestPasswordGeneratorSeedDeterminism(t *testing.T) {
	t.Parallel()

	parts := []string{"scenario", "step", "request.json", "user_password"}
	first, err := runGenerator("password", map[string]cty.Value{}, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate first password: %v", err)
	}

	second, err := runGenerator("password", map[string]cty.Value{}, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate second password: %v", err)
	}

	otherSeed, err := runGenerator("password", map[string]cty.Value{}, newGeneratorRandom(5678, parts...))
	if err != nil {
		t.Fatalf("generate other-seed password: %v", err)
	}

	if first.AsString() != second.AsString() {
		t.Fatalf("same seed generated different passwords: %q vs %q", first.AsString(), second.AsString())
	}

	if first.AsString() == otherSeed.AsString() {
		t.Fatalf("different seed generated same password: %q", first.AsString())
	}
}

func TestPasswordGeneratorParallelScenarioExecutionIsStable(t *testing.T) {
	t.Parallel()

	serial := runGeneratedPasswordSuite(t, 1, false)
	parallel := runGeneratedPasswordSuite(t, 8, false)

	for scenario, serialPassword := range serial {
		if parallel[scenario] != serialPassword {
			t.Fatalf("password changed for %s: parallel=1 %q parallel=8 %q", scenario, serialPassword, parallel[scenario])
		}
	}
}

func TestPasswordGeneratorUnrelatedStepDoesNotChangePassword(t *testing.T) {
	t.Parallel()

	withoutUnrelated := runGeneratedPasswordSuite(t, 4, false)
	withUnrelated := runGeneratedPasswordSuite(t, 4, true)

	for scenario, password := range withoutUnrelated {
		if withUnrelated[scenario] != password {
			t.Fatalf("password changed after adding unrelated step for %s: %q vs %q", scenario, password, withUnrelated[scenario])
		}
	}
}

func TestPasswordGeneratorRetryKeepsSamePassword(t *testing.T) {
	t.Parallel()

	providerImpl := &generatedPasswordProvider{
		calls:     map[string]int{},
		passwords: map[string][]string{},
		passAfter: map[string]int{"register": 3},
	}

	runner := NewRunner(provider.NewRegistry(providerImpl))
	suite := generatedPasswordSuite(1, false)
	suite.Scenarios[0].Steps[0].Retry = &model.Retry{Attempts: 3}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1234, Parallel: 1})
	if err != nil {
		t.Fatalf("run suite: %v", err)
	}

	step := result.Scenarios[0].Steps[0]
	if step.Status != report.StatusPass {
		t.Fatalf("step should pass, got %s", step.Status)
	}

	passwords := providerImpl.passwords["scenario_1/register"]
	if len(passwords) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(passwords))
	}

	for _, password := range passwords[1:] {
		if password != passwords[0] {
			t.Fatalf("password changed across retries: %#v", passwords)
		}
	}
}

func TestGeneratedPasswordCanBeCapturedFromRequestJSON(t *testing.T) {
	t.Parallel()

	result := runGeneratedPasswordSuite(t, 1, false)
	password := result["scenario_1"]

	if password == "" {
		t.Fatalf("captured password is empty")
	}

	assertPasswordConstraints(t, password, faker.DefaultPasswordOptions())
}

type generatedPasswordProvider struct {
	mu        sync.Mutex
	calls     map[string]int
	passwords map[string][]string
	passAfter map[string]int
}

func (p *generatedPasswordProvider) Type() string {
	return "http"
}

func (p *generatedPasswordProvider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	_ = ctx

	key := input.Scenario + "/" + input.Step.Name
	password := passwordFromRequest(input.Request)

	p.mu.Lock()
	p.calls[key]++
	call := p.calls[key]
	p.passwords[key] = append(p.passwords[key], password)
	passAfter := p.passAfter[input.Step.Name]
	p.mu.Unlock()

	status := 200
	if passAfter > 0 && call < passAfter {
		status = 500
	}

	return &provider.Output{
		StatusCode: status,
		Request:    input.Request,
		Response: map[string]cty.Value{
			"status":  cty.NumberIntVal(int64(status)),
			"headers": cty.EmptyObjectVal,
			"body":    cty.StringVal(`{"ok":true}`),
			"json":    cty.ObjectVal(map[string]cty.Value{"ok": cty.BoolVal(true)}),
		},
	}, nil
}

func (p *generatedPasswordProvider) generatedPasswordsByScenario() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()

	passwords := map[string]string{}
	for key, values := range p.passwords {
		parts := strings.Split(key, "/")
		if len(parts) != 2 || parts[1] != "register" || len(values) == 0 {
			continue
		}

		passwords[parts[0]] = values[len(values)-1]
	}

	return passwords
}

func runGeneratedPasswordSuite(t *testing.T, parallel int, includeUnrelated bool) map[string]string {
	t.Helper()

	providerImpl := &generatedPasswordProvider{
		calls:     map[string]int{},
		passwords: map[string][]string{},
		passAfter: map[string]int{},
	}
	runner := NewRunner(provider.NewRegistry(providerImpl))

	result, err := runner.Run(context.Background(), generatedPasswordSuite(6, includeUnrelated), Options{Seed: 1234, Parallel: parallel})
	if err != nil {
		t.Fatalf("run suite: %v", err)
	}

	for _, scenario := range result.Scenarios {
		for _, step := range scenario.Steps {
			if step.Name != "register" {
				continue
			}

			requestJSON, ok := step.Request["json"].(map[string]interface{})
			if !ok {
				t.Fatalf("request json missing for %s: %#v", scenario.Name, step.Request)
			}

			if requestJSON["password"] != "***" {
				t.Fatalf("reported password should be masked for %s: %#v", scenario.Name, requestJSON)
			}
		}
	}

	return providerImpl.generatedPasswordsByScenario()
}

func generatedPasswordSuite(scenarioCount int, includeUnrelated bool) *model.Suite {
	suite := &model.Suite{
		Version: 1,
		Generators: map[string]*model.Generator{
			"user_password": {
				Type: "password",
				Name: "user_password",
				Params: map[string]model.Expression{
					"length":      expr("16"),
					"min_upper":   expr("1"),
					"min_lower":   expr("1"),
					"min_digit":   expr("1"),
					"min_special": expr("1"),
					"specials":    expr(`"!@#$%^&*"`),
				},
			},
		},
		ConfigExpr: map[string]model.Expression{},
	}

	for i := 1; i <= scenarioCount; i++ {
		scenario := &model.Scenario{
			Name: fmt.Sprintf("scenario_%d", i),
			File: "test.tales",
			Steps: []*model.Step{
				generatedPasswordStep("register"),
			},
		}
		if includeUnrelated {
			scenario.Steps = append(scenario.Steps, newRetryHTTPStatusStep("health", 200))
		}

		suite.Scenarios = append(suite.Scenarios, scenario)
	}

	return suite
}

func generatedPasswordStep(name string) *model.Step {
	return &model.Step{
		Provider: "http",
		Name:     name,
		Request: &model.Request{
			Method: expr(`"POST"`),
			URL:    expr(`"http://example.test/users"`),
			JSON: expr(`{
				email = "user@example.com"
				password = generate("user_password")
			}`),
		},
		Expect: &model.Expect{Status: expr("200")},
		Capture: map[string]model.Expression{
			"password": expr("request.json.password"),
		},
	}
}

func passwordFromRequest(request map[string]cty.Value) string {
	jsonValue, ok := request["json"]
	if !ok || jsonValue.IsNull() || !jsonValue.Type().IsObjectType() {
		return ""
	}

	password := jsonValue.GetAttr("password")
	if password.Type() != cty.String {
		return ""
	}

	return password.AsString()
}

func passwordParams(config faker.PasswordOptions) map[string]cty.Value {
	return map[string]cty.Value{
		"length":      cty.NumberIntVal(int64(config.Length)),
		"min_upper":   cty.NumberIntVal(int64(config.MinUpper)),
		"min_lower":   cty.NumberIntVal(int64(config.MinLower)),
		"min_digit":   cty.NumberIntVal(int64(config.MinDigit)),
		"min_special": cty.NumberIntVal(int64(config.MinSpecial)),
		"specials":    cty.StringVal(config.Specials),
	}
}

func assertPasswordConstraints(t *testing.T, password string, config faker.PasswordOptions) {
	t.Helper()

	if len(password) != config.Length {
		t.Fatalf("password length=%d want %d: %q", len(password), config.Length, password)
	}

	counts := map[string]int{}
	for _, char := range password {
		switch {
		case char >= 'A' && char <= 'Z':
			counts["upper"]++
		case char >= 'a' && char <= 'z':
			counts["lower"]++
		case char >= '0' && char <= '9':
			counts["digit"]++
		case strings.ContainsRune(config.Specials, char):
			counts["special"]++
		default:
			t.Fatalf("password contains unsupported character %q in %q", char, password)
		}
	}

	if counts["upper"] < config.MinUpper {
		t.Fatalf("upper count=%d want at least %d in %q", counts["upper"], config.MinUpper, password)
	}
	if counts["lower"] < config.MinLower {
		t.Fatalf("lower count=%d want at least %d in %q", counts["lower"], config.MinLower, password)
	}
	if counts["digit"] < config.MinDigit {
		t.Fatalf("digit count=%d want at least %d in %q", counts["digit"], config.MinDigit, password)
	}
	if counts["special"] < config.MinSpecial {
		t.Fatalf("special count=%d want at least %d in %q", counts["special"], config.MinSpecial, password)
	}
}
