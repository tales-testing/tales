package http

import (
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

func TestBuildRequestTemplateMinimal(t *testing.T) {
	t.Parallel()

	tmpl, err := BuildRequestTemplate(provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal("http://localhost:1337/healthz"),
		},
	})
	if err != nil {
		t.Fatalf("BuildRequestTemplate: %v", err)
	}

	if tmpl.Method != "GET" {
		t.Fatalf("method=%q", tmpl.Method)
	}

	if tmpl.URL != "http://localhost:1337/healthz" {
		t.Fatalf("url=%q", tmpl.URL)
	}

	if tmpl.Body != nil {
		t.Fatalf("expected nil body, got %v", tmpl.Body)
	}

	if tmpl.BasicAuth != nil {
		t.Fatalf("expected no basic auth")
	}
}

func TestBuildRequestTemplateJSONBodyHeaders(t *testing.T) {
	t.Parallel()

	tmpl, err := BuildRequestTemplate(provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("POST"),
			"url":    cty.StringVal("http://example.com/api"),
			"body":   bodyJSONValue(map[string]cty.Value{"name": cty.StringVal("axel")}),
		},
	})
	if err != nil {
		t.Fatalf("BuildRequestTemplate: %v", err)
	}

	if got, want := tmpl.Headers["Content-Type"], "application/json"; got != want {
		t.Fatalf("content-type=%q want %q", got, want)
	}

	if !strings.Contains(string(tmpl.Body), `"name":"axel"`) {
		t.Fatalf("body=%q", string(tmpl.Body))
	}
}

func TestBuildRequestTemplateBasicAuthMasksReport(t *testing.T) {
	t.Parallel()

	tmpl, err := BuildRequestTemplate(provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal("http://example.com/private"),
			"auth": cty.ObjectVal(map[string]cty.Value{
				"basic": cty.ObjectVal(map[string]cty.Value{
					"username": cty.StringVal("alice"),
					"password": cty.StringVal("s3cret"),
				}),
			}),
		},
	})
	if err != nil {
		t.Fatalf("BuildRequestTemplate: %v", err)
	}

	if tmpl.BasicAuth == nil || tmpl.BasicAuth.Username != "alice" {
		t.Fatalf("expected resolved basic auth, got %+v", tmpl.BasicAuth)
	}

	if got := tmpl.ReportHeaders["Authorization"]; got != "Basic ***" {
		t.Fatalf("ReportHeaders.Authorization=%q want masked", got)
	}
}

func TestBuildRequestTemplateRejectsMultipart(t *testing.T) {
	t.Parallel()

	_, err := BuildRequestTemplate(provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("POST"),
			"url":    cty.StringVal("http://example.com/upload"),
			"body": cty.ObjectVal(map[string]cty.Value{
				"multipart": cty.ObjectVal(map[string]cty.Value{
					"placeholder": cty.StringVal("anything"),
				}),
			}),
		},
	})
	if err == nil {
		t.Fatalf("expected multipart rejection")
	}

	if !strings.Contains(err.Error(), "multipart bodies are not supported in load requests") {
		t.Fatalf("unexpected error: %v", err)
	}
}
