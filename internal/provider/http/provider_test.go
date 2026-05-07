package http

import (
	"context"
	"net/http"
	"net/http/httptest"
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
