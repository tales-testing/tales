package runtime

import (
	"context"
	"sync"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// recordingEmailProvider echoes the email it receives in the request body and
// records every email seen, in call order. It backs the keyword-seed tests:
// the assertions look at the recorded emails to detect cross-call-site
// collisions of deterministic generators invoked inside keywords.
type recordingEmailProvider struct {
	mu     sync.Mutex
	emails []string
}

func (p *recordingEmailProvider) Type() string {
	return "http"
}

func (p *recordingEmailProvider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	_ = ctx

	email, err := nestedString(input.Request, "body", "json", "email")
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.emails = append(p.emails, email)
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
}

func (p *recordingEmailProvider) recorded() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]string, len(p.emails))
	copy(out, p.emails)

	return out
}

// emailGeneratorSuite builds a suite with a register_user keyword whose internal
// step generates an email, plus a scenario that invokes the keyword once per
// callSites entry (each from a distinct calling step name).
func emailGeneratorSuite(callSites []string) *model.Suite {
	steps := make([]*model.Step, 0, len(callSites))

	for _, name := range callSites {
		steps = append(steps, &model.Step{
			Provider: "keyword",
			Name:     name,
			Keyword: &model.KeywordCall{
				Name:   expr(`"register_user"`),
				Inputs: expr(`{}`),
			},
		})
	}

	return &model.Suite{
		Generators: map[string]*model.Generator{
			"user_email": {Type: "email", Name: "user_email", Params: map[string]model.Expression{}},
		},
		Keywords: map[string]*model.Keyword{
			"register_user": {
				Name: "register_user",
				Steps: []*model.Step{
					{
						Provider: "http",
						Name:     "register",
						Request: &model.Request{
							Method: expr(`"POST"`),
							URL:    expr(`"http://example.test/signup"`),
							Body:   bodyJSONExpr(`{ email = generate("user_email") }`),
						},
						Expect: &model.Expect{Status: expr("201")},
						Capture: map[string]model.Expression{
							"email": expr("request.body.json.email"),
						},
					},
				},
				Outputs: map[string]model.Expression{
					"email": expr("result.register.email"),
				},
			},
		},
		Scenarios: []*model.Scenario{{
			Name:  "register flow",
			File:  "test.tales",
			Steps: steps,
		}},
	}
}

func runEmailGeneratorSuite(t *testing.T, callSites []string) []string {
	t.Helper()

	httpProvider := &recordingEmailProvider{}
	runner := NewRunner(provider.NewRegistry(httpProvider))

	result, err := runner.Run(context.Background(), emailGeneratorSuite(callSites), Options{Seed: 1234, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}

	if status := result.Scenarios[0].Status; status != report.StatusPass {
		t.Fatalf("expected scenario pass, got %s, failure=%v", status, result.Scenarios[0].Failure)
	}

	emails := httpProvider.recorded()
	if len(emails) != len(callSites) {
		t.Fatalf("expected %d generated emails, got %d (%v)", len(callSites), len(emails), emails)
	}

	return emails
}

// TestKeywordGeneratorDistinctPerCallSite is the core regression test: calling
// the same keyword twice in one scenario must yield two DIFFERENT generated
// emails. Before the fix the seed mix ignored the calling step, so both calls
// collided and produced the same email ("user already exists" in real suites).
func TestKeywordGeneratorDistinctPerCallSite(t *testing.T) {
	t.Parallel()

	emails := runEmailGeneratorSuite(t, []string{"signup_a", "signup_b"})

	if emails[0] == emails[1] {
		t.Fatalf("expected distinct emails for two keyword call sites, got duplicate %q", emails[0])
	}
}

// TestKeywordGeneratorDeterministicAcrossRuns pins determinism: the same seed
// must reproduce the same pair of emails across independent runs.
func TestKeywordGeneratorDeterministicAcrossRuns(t *testing.T) {
	t.Parallel()

	first := runEmailGeneratorSuite(t, []string{"signup_a", "signup_b"})
	second := runEmailGeneratorSuite(t, []string{"signup_a", "signup_b"})

	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("expected deterministic email at index %d, got %q then %q", i, first[i], second[i])
		}
	}
}

// TestSingleKeywordGeneratorStableAcrossRuns guards the non-buggy single-call
// case: one keyword invocation stays stable across runs (same seed).
func TestSingleKeywordGeneratorStableAcrossRuns(t *testing.T) {
	t.Parallel()

	first := runEmailGeneratorSuite(t, []string{"signup_only"})
	second := runEmailGeneratorSuite(t, []string{"signup_only"})

	if first[0] != second[0] {
		t.Fatalf("expected stable email across runs, got %q then %q", first[0], second[0])
	}
}

// TestNestedKeywordGeneratorDistinctPerCallSite checks the call stack is honored
// across nesting: a wrapper keyword calls register_user, and two wrapper call
// sites must still yield distinct deep-generated emails.
func TestNestedKeywordGeneratorDistinctPerCallSite(t *testing.T) {
	t.Parallel()

	httpProvider := &recordingEmailProvider{}
	runner := NewRunner(provider.NewRegistry(httpProvider))

	suite := emailGeneratorSuite([]string{"wrap_a", "wrap_b"})
	// Point the two scenario steps at the wrapper keyword instead of register_user.
	for _, step := range suite.Scenarios[0].Steps {
		step.Keyword.Name = expr(`"wrap"`)
	}

	suite.Keywords["wrap"] = &model.Keyword{
		Name: "wrap",
		Steps: []*model.Step{
			{
				Provider: "keyword",
				Name:     "inner",
				Keyword: &model.KeywordCall{
					Name:   expr(`"register_user"`),
					Inputs: expr(`{}`),
				},
			},
		},
		Outputs: map[string]model.Expression{
			"email": expr("result.inner.email"),
		},
	}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1234, Parallel: 1})
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}

	if status := result.Scenarios[0].Status; status != report.StatusPass {
		t.Fatalf("expected scenario pass, got %s, failure=%v", status, result.Scenarios[0].Failure)
	}

	emails := httpProvider.recorded()
	if len(emails) != 2 {
		t.Fatalf("expected 2 generated emails, got %d (%v)", len(emails), emails)
	}

	if emails[0] == emails[1] {
		t.Fatalf("expected distinct emails for two nested keyword call sites, got duplicate %q", emails[0])
	}
}
