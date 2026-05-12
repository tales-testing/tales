package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return New(srv.URL)
}

func TestClientHealthOK(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))

	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestClientHealthNon200(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "driver not ready")
	}))

	err := client.Health(context.Background())
	if err == nil {
		t.Fatal("expected error from non-200 health")
	}

	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected error to include 503, got %v", err)
	}
}

func TestClientHealthUnexpectedStatus(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"booting"}`)
	}))

	err := client.Health(context.Background())
	if err == nil || !strings.Contains(err.Error(), "booting") {
		t.Fatalf("expected status mismatch error, got %v", err)
	}
}

func TestClientHierarchyParses(t *testing.T) {
	t.Parallel()

	payload := `{
		"id": "root",
		"type": "application",
		"enabled": true,
		"visible": true,
		"bounds": {"x":0,"y":0,"width":390,"height":844},
		"children": [
			{
				"id": "welcome.register",
				"type": "button",
				"label": "Create account",
				"enabled": true,
				"visible": true,
				"bounds": {"x":20,"y":100,"width":100,"height":40}
			}
		]
	}`

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hierarchy" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}

		if r.URL.Query().Get("bundleId") != "com.example.MyApp" {
			t.Errorf("unexpected bundleId %q", r.URL.Query().Get("bundleId"))
		}

		_, _ = io.WriteString(w, payload)
	}))

	root, err := client.Hierarchy(context.Background(), "com.example.MyApp")
	if err != nil {
		t.Fatalf("hierarchy: %v", err)
	}

	if root == nil || root.ID != "root" {
		t.Fatalf("expected root node, got %+v", root)
	}

	if len(root.Children) != 1 || root.Children[0].ID != "welcome.register" {
		t.Fatalf("expected one child, got %+v", root.Children)
	}
}

func TestClientHierarchyMalformedJSON(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":`) // truncated
	}))

	if _, err := client.Hierarchy(context.Background(), "com.example.MyApp"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestClientTapSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tap" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.Tap(context.Background(), "com.example.MyApp", 12.5, 34.25); err != nil {
		t.Fatalf("tap: %v", err)
	}

	if captured["bundleId"] != "com.example.MyApp" || captured["x"] != 12.5 || captured["y"] != 34.25 {
		t.Fatalf("unexpected payload: %v", captured)
	}
}

func TestClientInputTextSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]string

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inputText" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.InputText(context.Background(), "com.example.MyApp", "hello@example.com"); err != nil {
		t.Fatalf("inputText: %v", err)
	}

	if captured["bundleId"] != "com.example.MyApp" || captured["text"] != "hello@example.com" {
		t.Fatalf("unexpected payload %v", captured)
	}
}

func TestClientEraseTextSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eraseText" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.EraseText(context.Background(), "com.example.MyApp", 5); err != nil {
		t.Fatalf("eraseText: %v", err)
	}

	if captured["bundleId"] != "com.example.MyApp" || captured["characters"] != float64(5) {
		t.Fatalf("unexpected payload %v", captured)
	}
}

func TestClientEraseTextRejectsNegative(t *testing.T) {
	t.Parallel()

	client := New("http://unused")
	if err := client.EraseText(context.Background(), "com.example.MyApp", -1); err == nil {
		t.Fatal("expected error for negative characters")
	}
}

func TestClientScreenshotReturnsBytes(t *testing.T) {
	t.Parallel()

	want := bytes.Repeat([]byte{0x89, 0x50, 0x4E, 0x47}, 4)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(want)
	}))

	got, err := client.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected screenshot bytes (len=%d)", len(got))
	}
}

func TestClientScreenshotNon200(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))

	if _, err := client.Screenshot(context.Background()); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 error, got %v", err)
	}
}
