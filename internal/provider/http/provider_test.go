package http

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/diagnostic"
	"github.com/tales-testing/tales/internal/provider"
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
			"body":   bodyJSONValue(map[string]cty.Value{"email": cty.StringVal("foo@example.com")}),
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

func TestHTTPProviderFormRequestEncodesValues(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}

		if got, want := string(body), "a=x%26y&b=z%3Dw&password=pa%2Bss+%25%23"; got != want {
			t.Fatalf("body=%q want %q", got, want)
		}

		if got := req.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("content-type=%q", got)
		}

		parsed, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}

		if got := parsed.Get("a"); got != "x&y" {
			t.Fatalf("form a=%q", got)
		}

		if got := parsed.Get("b"); got != "z=w" {
			t.Fatalf("form b=%q", got)
		}

		if got := parsed.Get("password"); got != "pa+ss %#" {
			t.Fatalf("form password=%q", got)
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
			"body": bodyFormValue(map[string]cty.Value{
				"a":        cty.StringVal("x&y"),
				"b":        cty.StringVal("z=w"),
				"password": cty.StringVal("pa+ss %#"),
			}),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	form := out.Request["body"].AsValueMap()["form"].AsValueMap()
	if form["a"].AsString() != "x&y" {
		t.Fatalf("output form was not preserved")
	}
}

func TestHTTPProviderFormRequestPreservesExplicitContentType(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded; charset=utf-8" {
			t.Fatalf("content-type=%q", got)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	p := New()
	_, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("POST"),
			"url":    cty.StringVal(ts.URL),
			"headers": cty.ObjectVal(map[string]cty.Value{
				"content-type": cty.StringVal("application/x-www-form-urlencoded; charset=utf-8"),
			}),
			"body": bodyFormValue(map[string]cty.Value{
				"a": cty.StringVal("x&y"),
			}),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPProviderFormRequestRejectsBodyConflict(t *testing.T) {
	t.Parallel()

	p := New()
	_, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("POST"),
			"url":    cty.StringVal("http://example.test"),
			"body": cty.ObjectVal(map[string]cty.Value{
				"form": cty.ObjectVal(map[string]cty.Value{"a": cty.StringVal("x&y")}),
				"raw":  cty.StringVal("a=x%26y"),
			}),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "request.body must define exactly one of json, form, raw, or multipart") {
		t.Fatalf("expected form/body conflict, got %v", err)
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

	auth := out.Request["auth"].AsValueMap()["basic"].AsValueMap()
	if auth["username"].AsString() != "admin" {
		t.Fatalf("auth username not preserved")
	}
	if auth["password"].AsString() != "secret" {
		t.Fatalf("auth password not preserved for request expressions")
	}

	sanitized := diagnostic.FromCTYMap(out.Request)
	basic := sanitized["auth"].(map[string]interface{})["basic"].(map[string]interface{})
	if basic["password"] != "***" {
		t.Fatalf("auth password should be masked in diagnostics: %#v", basic)
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

func TestHTTPProviderBasicAuthRejectsInvalidFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth cty.Value
		want string
	}{
		{
			name: "missing username",
			auth: cty.ObjectVal(map[string]cty.Value{
				"basic": cty.ObjectVal(map[string]cty.Value{
					"password": cty.StringVal("secret"),
				}),
			}),
			want: "resolve basic auth: request.auth.basic.username is required",
		},
		{
			name: "null password",
			auth: cty.ObjectVal(map[string]cty.Value{
				"basic": cty.ObjectVal(map[string]cty.Value{
					"username": cty.StringVal("admin"),
					"password": cty.NullVal(cty.String),
				}),
			}),
			want: "resolve basic auth: request.auth.basic.password must not be null",
		},
		{
			name: "number username",
			auth: cty.ObjectVal(map[string]cty.Value{
				"basic": cty.ObjectVal(map[string]cty.Value{
					"username": cty.NumberIntVal(123),
					"password": cty.StringVal("secret"),
				}),
			}),
			want: "resolve basic auth: request.auth.basic.username must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := New()
			_, err := p.Execute(context.Background(), provider.Input{
				Request: map[string]cty.Value{
					"method": cty.StringVal("GET"),
					"url":    cty.StringVal("http://example.test"),
					"auth":   tt.auth,
				},
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got %v", tt.want, err)
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
			"body": bodyJSONValue(map[string]cty.Value{"x": cty.StringVal("y")}),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", out.StatusCode)
	}
}

func TestHTTPProviderResponseHeadersPreservesValues(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "ia_session=abc123; Path=/; HttpOnly; Secure")
		w.Header().Add("Set-Cookie", "theme=dark; Path=/")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	p := New()
	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal(ts.URL),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headers := out.Response["headers"].AsValueMap()

	setCookies, ok := headers["Set-Cookie"]
	if !ok {
		t.Fatalf("expected Set-Cookie in response.headers, got keys: %v", headerKeys(headers))
	}

	got := []string{}
	for it := setCookies.ElementIterator(); it.Next(); {
		_, v := it.Element()
		got = append(got, v.AsString())
	}

	wantCookies := []string{
		"ia_session=abc123; Path=/; HttpOnly; Secure",
		"theme=dark; Path=/",
	}

	if len(got) != len(wantCookies) {
		t.Fatalf("expected %d Set-Cookie values, got %d: %v", len(wantCookies), len(got), got)
	}

	for i, w := range wantCookies {
		if got[i] != w {
			t.Fatalf("Set-Cookie[%d]: got %q want %q", i, got[i], w)
		}
	}

	ct, ok := headers["Content-Type"]
	if !ok {
		t.Fatalf("expected Content-Type in response.headers")
	}

	ctValues := []string{}
	for it := ct.ElementIterator(); it.Next(); {
		_, v := it.Element()
		ctValues = append(ctValues, v.AsString())
	}

	if len(ctValues) != 1 || ctValues[0] != "application/json" {
		t.Fatalf("unexpected Content-Type values: %v", ctValues)
	}
}

func TestHTTPProviderResponseCookies(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "ia_session=abc123; Path=/; HttpOnly; Secure")
		w.Header().Add("Set-Cookie", "theme=dark; Path=/")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	p := New()
	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal(ts.URL),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cookies := out.Response["cookies"].AsValueMap()

	session, ok := cookies["ia_session"]
	if !ok {
		t.Fatalf("expected ia_session cookie")
	}

	fields := session.AsValueMap()
	if fields["value"].AsString() != "abc123" {
		t.Fatalf("ia_session.value=%q want abc123", fields["value"].AsString())
	}

	if fields["path"].AsString() != "/" {
		t.Fatalf("ia_session.path=%q want /", fields["path"].AsString())
	}

	if !fields["http_only"].True() {
		t.Fatalf("ia_session.http_only should be true")
	}

	if !fields["secure"].True() {
		t.Fatalf("ia_session.secure should be true")
	}

	if fields["name"].AsString() != "ia_session" {
		t.Fatalf("ia_session.name=%q want ia_session", fields["name"].AsString())
	}

	theme, ok := cookies["theme"]
	if !ok {
		t.Fatalf("expected theme cookie")
	}

	if theme.AsValueMap()["value"].AsString() != "dark" {
		t.Fatalf("theme.value=%q want dark", theme.AsValueMap()["value"].AsString())
	}
}

func TestHTTPProviderResponseCookiesDuplicateNameLastWins(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "foo=1; Path=/")
		w.Header().Add("Set-Cookie", "foo=2; Path=/")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	p := New()
	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal(ts.URL),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foo := out.Response["cookies"].AsValueMap()["foo"]
	if foo.AsValueMap()["value"].AsString() != "2" {
		t.Fatalf("expected last-write-wins (value=2), got %q", foo.AsValueMap()["value"].AsString())
	}
}

func TestHTTPProviderResponseCookiesDashedName(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "ia-session=xyz; Path=/")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	p := New()
	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal(ts.URL),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cookies := out.Response["cookies"].AsValueMap()

	cookie, ok := cookies["ia-session"]
	if !ok {
		t.Fatalf("expected ia-session cookie under exact name (got keys: %v)", cookieKeys(cookies))
	}

	if cookie.AsValueMap()["value"].AsString() != "xyz" {
		t.Fatalf("ia-session.value=%q want xyz", cookie.AsValueMap()["value"].AsString())
	}
}

func TestHTTPProviderResponseHeadersEmpty(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	p := New()
	out, err := p.Execute(context.Background(), provider.Input{
		Request: map[string]cty.Value{
			"method": cty.StringVal("GET"),
			"url":    cty.StringVal(ts.URL),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cookies := out.Response["cookies"]; !cookies.Type().Equals(cty.EmptyObject) {
		t.Fatalf("expected empty cookies object, got type %s", cookies.Type().FriendlyName())
	}
}

func headerKeys(m map[string]cty.Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

func cookieKeys(m map[string]cty.Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

func basicAuthValue(username, password string) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"basic": cty.ObjectVal(map[string]cty.Value{
			"username": cty.StringVal(username),
			"password": cty.StringVal(password),
		}),
	})
}

func bodyJSONValue(values map[string]cty.Value) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"json": cty.ObjectVal(values),
	})
}

func bodyFormValue(values map[string]cty.Value) cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"form": cty.ObjectVal(values),
	})
}
