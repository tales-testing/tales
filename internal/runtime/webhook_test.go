package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	webhookprovider "github.com/tales-testing/tales/internal/provider/webhook"
	"github.com/zclconf/go-cty/cty"
)

func webhookTestEvaluator() *lang.Evaluator {
	return lang.NewEvaluator(func(string, lang.GenerateMeta) (cty.Value, error) {
		return cty.NilVal, fmt.Errorf("generate is unavailable in webhook test")
	})
}

func receivedWith(headerValue, rawBody string, jsonBody cty.Value) map[string]cty.Value {
	return map[string]cty.Value{
		"method": cty.StringVal("POST"),
		"path":   cty.StringVal("/hook"),
		"headers": cty.ObjectVal(map[string]cty.Value{
			"X-Webhook-Signature": cty.StringVal(headerValue),
		}),
		"body": cty.ObjectVal(map[string]cty.Value{
			"raw":  cty.StringVal(rawBody),
			"json": jsonBody,
		}),
		"json": jsonBody,
	}
}

func webhookHMACStep(payload string) *model.Step {
	return &model.Step{
		Name: "w",
		Webhook: &model.WebhookCall{
			Expect: &model.WebhookExpect{
				HMAC: &model.WebhookHMACExpect{
					Header:  expr(`"X-Webhook-Signature"`),
					Secret:  expr(`"top-secret-key"`),
					Format:  expr(`"t={timestamp},v1={signature}"`),
					Payload: expr(payload),
				},
			},
		},
	}
}

func TestAssertWebhookHMACValid(t *testing.T) {
	t.Parallel()

	raw := `{"id":"evt_1"}`

	sig, err := webhookprovider.ComputeHMAC("sha256", "top-secret-key", "1700000000."+raw)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}

	received := receivedWith(fmt.Sprintf("t=1700000000,v1=%s", sig), raw, cty.NullVal(cty.DynamicPseudoType))
	scope := lang.ScopeData{Config: map[string]cty.Value{}, Result: map[string]cty.Value{}, Request: received, Response: map[string]cty.Value{}, Input: map[string]cty.Value{}}
	step := webhookHMACStep(`"${timestamp}.${request.body.raw}"`)

	if err := assertWebhookHMAC(webhookTestEvaluator(), scope, "s", step, step.Webhook.Expect.HMAC, received); err != nil {
		t.Fatalf("expected valid signature to pass, got %v", err)
	}
}

func TestAssertWebhookHMACInvalidIsLeakFree(t *testing.T) {
	t.Parallel()

	received := receivedWith("t=1700000000,v1=deadbeef", `{"id":"evt_1"}`, cty.NullVal(cty.DynamicPseudoType))
	scope := lang.ScopeData{Config: map[string]cty.Value{}, Result: map[string]cty.Value{}, Request: received, Response: map[string]cty.Value{}, Input: map[string]cty.Value{}}
	step := webhookHMACStep(`"${timestamp}.${request.body.raw}"`)

	err := assertWebhookHMAC(webhookTestEvaluator(), scope, "s", step, step.Webhook.Expect.HMAC, received)
	if err == nil {
		t.Fatal("expected invalid signature error")
	}

	if !strings.Contains(err.Error(), "invalid webhook signature") {
		t.Fatalf("unexpected error: %v", err)
	}

	// The secret and the computed digest must never appear in the error.
	if strings.Contains(err.Error(), "top-secret-key") {
		t.Fatalf("error leaked the secret: %v", err)
	}
}

func TestAssertWebhookHMACMissingHeader(t *testing.T) {
	t.Parallel()

	received := map[string]cty.Value{
		"headers": cty.EmptyObjectVal,
		"body":    cty.ObjectVal(map[string]cty.Value{"raw": cty.StringVal("{}"), "json": cty.NullVal(cty.DynamicPseudoType)}),
	}
	scope := lang.ScopeData{Config: map[string]cty.Value{}, Result: map[string]cty.Value{}, Request: received, Response: map[string]cty.Value{}, Input: map[string]cty.Value{}}
	step := webhookHMACStep(`"${timestamp}.${request.body.raw}"`)

	err := assertWebhookHMAC(webhookTestEvaluator(), scope, "s", step, step.Webhook.Expect.HMAC, received)
	if err == nil || !strings.Contains(err.Error(), "missing signature header") {
		t.Fatalf("expected missing header error, got %v", err)
	}
}

func TestAssertWebhookRequestMethodMismatch(t *testing.T) {
	t.Parallel()

	received := receivedWith("t=1,v1=ab", `{}`, cty.NullVal(cty.DynamicPseudoType))
	scope := lang.ScopeData{Config: map[string]cty.Value{}, Result: map[string]cty.Value{}, Request: received, Response: map[string]cty.Value{}, Input: map[string]cty.Value{}}

	expect := &model.WebhookRequestExpect{Method: expr(`"GET"`)}
	step := &model.Step{Name: "w"}

	err := assertWebhookRequest(webhookTestEvaluator(), scope, "s", step, expect, received)
	if err == nil {
		t.Fatal("expected method mismatch error")
	}
}

func TestParseByteSize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"", 0, false},
		{"512", 512, false},
		{"100B", 100, false},
		{"1KB", 1 << 10, false},
		{"10MB", 10 << 20, false},
		{"10mb", 10 << 20, false},
		{"  2MB  ", 2 << 20, false},
		{"1GB", 1 << 30, false},
		{"1.5KB", 1536, false},
		{"abc", 0, true},
		{"10XB", 0, true},
		{"MB", 0, true},
	}

	for _, tc := range cases {
		got, err := parseByteSize(tc.in)

		if tc.wantErr {
			if err == nil {
				t.Errorf("parseByteSize(%q): expected error, got %d", tc.in, got)
			}

			continue
		}

		if err != nil {
			t.Errorf("parseByteSize(%q): unexpected error %v", tc.in, err)

			continue
		}

		if got != tc.want {
			t.Errorf("parseByteSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestAssertWebhookRequestJSONPartialMatch(t *testing.T) {
	t.Parallel()

	jsonBody := cty.ObjectVal(map[string]cty.Value{
		"id":    cty.StringVal("evt_1"),
		"type":  cty.StringVal("order.completed"),
		"extra": cty.StringVal("ignored"),
	})
	received := receivedWith("t=1,v1=ab", `{"id":"evt_1"}`, jsonBody)
	scope := lang.ScopeData{Config: map[string]cty.Value{}, Result: map[string]cty.Value{}, Request: received, Response: map[string]cty.Value{}, Input: map[string]cty.Value{}}

	expect := &model.WebhookRequestExpect{
		Method: expr(`"POST"`),
		JSON:   expr(`{ type = "order.completed" }`),
	}
	step := &model.Step{Name: "w"}

	if err := assertWebhookRequest(webhookTestEvaluator(), scope, "s", step, expect, received); err != nil {
		t.Fatalf("expected partial json match to pass, got %v", err)
	}
}
