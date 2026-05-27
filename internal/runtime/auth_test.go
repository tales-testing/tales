package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
)

func TestBasicAuthRejectsInvalidEvaluatedValuesBeforeProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		username         string
		password         string
		wantErrorMessage string
	}{
		{
			name:             "null username",
			username:         "null",
			password:         `"secret"`,
			wantErrorMessage: "basic.username must not be null",
		},
		{
			name:             "number password",
			username:         `"admin"`,
			password:         "123",
			wantErrorMessage: "basic.password must be a string, got number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			providerImpl := &fakeProvider{failFor: map[string]bool{}}
			runner := NewRunner(provider.NewRegistry(providerImpl))
			suite := &model.Suite{Scenarios: []*model.Scenario{{
				Name: "s",
				File: "x.tales",
				Steps: []*model.Step{{
					Provider: "http",
					Name:     "protected",
					Request: &model.Request{
						Method: expr(`"GET"`),
						URL:    expr(`"http://example.test"`),
						Auth: &model.RequestAuth{Basic: &model.BasicAuth{
							Username: expr(tt.username),
							Password: expr(tt.password),
						}},
					},
					Expect: &model.Expect{Status: expr(`200`)},
				}},
			}}}

			result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
			if err != nil {
				t.Fatalf("unexpected runner error: %v", err)
			}

			step := result.Scenarios[0].Steps[0]
			if step.Status != report.StatusFail {
				t.Fatalf("step should fail, got %s", step.Status)
			}
			if step.Failure == nil || !strings.Contains(step.Failure.Message, tt.wantErrorMessage) {
				t.Fatalf("expected %q, got %#v", tt.wantErrorMessage, step.Failure)
			}
			if len(providerImpl.calls) != 0 {
				t.Fatalf("provider should not run with invalid basic auth, calls=%v", providerImpl.calls)
			}
		})
	}
}
