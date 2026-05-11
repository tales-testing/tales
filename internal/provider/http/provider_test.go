package http

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperxlab/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

func TestHTTPProviderJSONRequestAndResponse(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			t.Fatalf("method=%s", req.Method)
		}
		if req.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content-type: %s", req.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"1","ok":true}`))
	}))
	defer ts.Close()

	p := New()
	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("POST"),
			"url":    cty.StringVal(ts.URL),
			"json": cty.ObjectVal(map[string]cty.Value{
				"email": cty.StringVal("foo@example.com"),
			}),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d", out.StatusCode)
	}
	if out.Response["json"].IsNull() {
		t.Fatalf("expected decoded json response")
	}
}

func TestHTTPProviderBasicAuth(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		username, password, ok := req.BasicAuth()
		if !ok {
			t.Fatalf("basic auth missing")
		}
		if username != "admin" || password != "secret" {
			t.Fatalf("basic auth=%q/%q", username, password)
		}
		if req.Header.Get("X-Test") != "yes" {
			t.Fatalf("preserved header missing")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"authenticated":true}`))
	}))
	defer ts.Close()

	p := New()
	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal(ts.URL),
			"headers": cty.ObjectVal(map[string]cty.Value{
				"X-Test": cty.StringVal("yes"),
			}),
			"auth": basicAuthValue("admin", "secret"),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", out.StatusCode)
	}

	headers := out.Request["headers"].AsValueMap()
	if headers["X-Test"].AsString() != "yes" {
		t.Fatalf("output header not preserved")
	}
	if headers["Authorization"].AsString() != "Basic ***" {
		t.Fatalf("authorization should be report-safe, got %q", headers["Authorization"].AsString())
	}
}

func TestHTTPProviderBasicAuthRejectsAuthorizationHeaderConflict(t *testing.T) {
	t.Parallel()

	tests := []string{"Authorization", "authorization", "AUTHORIZATION"}
	for _, headerName := range tests {
		t.Run(headerName, func(t *testing.T) {
			t.Parallel()

			p := New()
			_, err := p.Execute(context.Background(), provider.Input{
				Request: map[string]cty.Value{
					"method": cty.StringVal("GET"),
					"url":    cty.StringVal("http://example.test"),
					"headers": cty.ObjectVal(map[string]cty.Value{
						headerName: cty.StringVal("Bearer abc"),
					}),
					"auth": basicAuthValue("admin", "secret"),
				},
			})
			if err == nil || !strings.Contains(err.Error(), "request cannot define both headers.Authorization and auth.basic") {
				t.Fatalf("expected conflict error, got %v", err)
			}
		})
	}
}

func TestHTTPProviderBasicAuthAllowsEmptyPassword(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		username, password, ok := req.BasicAuth()
		if !ok {
			t.Fatalf("basic auth missing")
		}
		if username != "admin" || password != "" {
			t.Fatalf("basic auth=%q/%q", username, password)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	p := New()
	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal(ts.URL),
			"auth":   basicAuthValue("admin", ""),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", out.StatusCode)
	}
}

func TestHTTPProviderBasicAuthAllowsEmptyUsername(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		username, password, ok := req.BasicAuth()
		if !ok {
			t.Fatalf("basic auth missing")
		}
		if username != "" || password != "secret" {
			t.Fatalf("basic auth=%q/%q", username, password)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	p := New()
	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal(ts.URL),
			"auth":   basicAuthValue("", "secret"),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", out.StatusCode)
	}
}

func TestHTTPProviderBasicAuthDoesNotExposeRawAuthorizationInOutput(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString([]byte("admin:secret"))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	p := New()
	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal(ts.URL),
			"auth":   basicAuthValue("admin", "secret"),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rendered := out.Request["headers"].GoString()
	if strings.Contains(rendered, "secret") || strings.Contains(rendered, encoded) || strings.Contains(rendered, "admin:secret") {
		t.Fatalf("basic auth leaked in output: %s", rendered)
	}
}

func TestHTTPProviderConnectHeaders(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Connect-Protocol-Version") != "1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	p := New()
	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("POST"),
			"url":    cty.StringVal(ts.URL),
			"headers": cty.ObjectVal(map[string]cty.Value{
				"Connect-Protocol-Version": cty.StringVal("1"),
			}),
			"json": cty.ObjectVal(map[string]cty.Value{"x": cty.StringVal("y")}),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", out.StatusCode)
	}
}

func basicAuthValue(username, password string) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"basic": cty.ObjectVal(map[string]cty.Value{
			"username": cty.StringVal(username),
			"password": cty.StringVal(password),
		}),
	})
}
