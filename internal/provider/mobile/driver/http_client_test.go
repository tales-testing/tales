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

	return New(srv.URL, WithHTTPClient(srv.Client()))
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

func TestClientLaunchSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/launch" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.Launch(context.Background(), "com.example.MyApp"); err != nil {
		t.Fatalf("launch: %v", err)
	}

	if captured["bundleId"] != "com.example.MyApp" {
		t.Fatalf("unexpected launch payload: %v", captured)
	}
}

func TestClientLaunchRequiresBundleID(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be hit when bundleID is empty")
	}))

	if err := client.Launch(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty bundleID")
	}
}

func TestClientTerminateSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/terminate" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.Terminate(context.Background(), "com.example.MyApp"); err != nil {
		t.Fatalf("terminate: %v", err)
	}

	if captured["bundleId"] != "com.example.MyApp" {
		t.Fatalf("unexpected terminate payload: %v", captured)
	}
}

func TestClientTerminateRequiresBundleID(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be hit when bundleID is empty")
	}))

	if err := client.Terminate(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty bundleID")
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

	if err := client.Tap(context.Background(), "com.example.MyApp", Locator{}, 12.5, 34.25); err != nil {
		t.Fatalf("tap: %v", err)
	}

	if captured["bundleId"] != "com.example.MyApp" || captured["x"] != 12.5 || captured["y"] != 34.25 {
		t.Fatalf("unexpected payload: %v", captured)
	}

	if _, hasID := captured["id"]; hasID {
		t.Fatalf("payload should omit empty id, got %v", captured)
	}
}

func TestClientSwipeSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/swipe" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.Swipe(context.Background(), "com.example.MyApp", 1, 2, 3, 4, 0.3); err != nil {
		t.Fatalf("swipe: %v", err)
	}

	if captured["startX"] != 1.0 || captured["startY"] != 2.0 ||
		captured["endX"] != 3.0 || captured["endY"] != 4.0 || captured["duration"] != 0.3 {
		t.Fatalf("unexpected swipe payload: %v", captured)
	}
}

func TestClientLongPressSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/longPress" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.LongPress(context.Background(), "com.example.MyApp", Locator{ID: "menu.item"}, 5, 6, 1.5); err != nil {
		t.Fatalf("longPress: %v", err)
	}

	if captured["id"] != "menu.item" || captured["x"] != 5.0 ||
		captured["y"] != 6.0 || captured["duration"] != 1.5 {
		t.Fatalf("unexpected longPress payload: %v", captured)
	}
}

func TestClientDoubleTapSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/doubleTap" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.DoubleTap(context.Background(), "com.example.MyApp", Locator{ID: "feed.item"}, 7, 8); err != nil {
		t.Fatalf("doubleTap: %v", err)
	}

	if captured["id"] != "feed.item" || captured["x"] != 7.0 || captured["y"] != 8.0 {
		t.Fatalf("unexpected doubleTap payload: %v", captured)
	}
}

func TestClientPressKeySendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pressKey" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.PressKey(context.Background(), "com.example.MyApp", "return"); err != nil {
		t.Fatalf("pressKey: %v", err)
	}

	if captured["bundleId"] != "com.example.MyApp" || captured["key"] != "return" {
		t.Fatalf("unexpected pressKey payload: %v", captured)
	}
}

func TestClientPressButtonSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pressButton" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.PressButton(context.Background(), "com.example.MyApp", "home"); err != nil {
		t.Fatalf("pressButton: %v", err)
	}

	if captured["bundleId"] != "com.example.MyApp" || captured["button"] != "home" {
		t.Fatalf("unexpected pressButton payload: %v", captured)
	}
}

func TestClientSetOrientationSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/orientation" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.SetOrientation(context.Background(), "landscape_left"); err != nil {
		t.Fatalf("setOrientation: %v", err)
	}

	if captured["orientation"] != "landscape_left" {
		t.Fatalf("unexpected setOrientation payload: %v", captured)
	}
}

func TestClientTapIncludesIDWhenProvided(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.Tap(context.Background(), "com.example.MyApp", Locator{ID: "auth.signup.accept_terms"}, 12.5, 34.25); err != nil {
		t.Fatalf("tap: %v", err)
	}

	if captured["id"] != "auth.signup.accept_terms" {
		t.Fatalf("payload missing id, got %v", captured)
	}
}

func TestClientInputTextSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inputText" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.InputText(context.Background(), "com.example.MyApp", Locator{}, "hello@example.com", false); err != nil {
		t.Fatalf("inputText: %v", err)
	}

	if captured["bundleId"] != "com.example.MyApp" || captured[payloadInputValueKey] != "hello@example.com" {
		t.Fatalf("unexpected payload %v", captured)
	}

	if _, hasID := captured["id"]; hasID {
		t.Fatalf("payload should omit empty id, got %v", captured)
	}

	if _, hasPaste := captured["paste"]; hasPaste {
		t.Fatalf("payload should omit paste=false, got %v", captured)
	}
}

func TestClientInputTextIncludesIDAndPasteWhenSet(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.InputText(context.Background(), "com.example.MyApp", Locator{ID: "auth.signup.password"}, "p@ssw0rd!", true); err != nil {
		t.Fatalf("inputText: %v", err)
	}

	if captured["id"] != "auth.signup.password" {
		t.Fatalf("payload missing id, got %v", captured)
	}

	if captured["paste"] != true {
		t.Fatalf("payload missing paste=true, got %v", captured)
	}
}

func TestClientTapIncludesLabelWhenSet(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tap" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.Tap(context.Background(), "com.example.MyApp", Locator{Label: "Done"}, 10, 20); err != nil {
		t.Fatalf("tap: %v", err)
	}

	if captured["label"] != "Done" {
		t.Fatalf("payload missing label=\"Done\", got %v", captured)
	}

	if _, hasID := captured["id"]; hasID {
		t.Fatalf("payload should omit empty id, got %v", captured)
	}
}

func TestClientScrollToSendsLocator(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/scrollTo" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.ScrollTo(context.Background(), "com.example.MyApp", Locator{ID: "form.identifier_value"}); err != nil {
		t.Fatalf("scrollTo: %v", err)
	}

	if captured["bundleId"] != "com.example.MyApp" {
		t.Fatalf("expected bundleId in payload, got %v", captured)
	}

	if captured["id"] != "form.identifier_value" {
		t.Fatalf("expected id in payload, got %v", captured)
	}

	if _, hasLabel := captured["label"]; hasLabel {
		t.Fatalf("payload should omit empty label, got %v", captured)
	}
}

func TestClientScrollToSendsLabel(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.ScrollTo(context.Background(), "com.example.MyApp", Locator{Label: "Done"}); err != nil {
		t.Fatalf("scrollTo: %v", err)
	}

	if captured["label"] != "Done" {
		t.Fatalf("expected label in payload, got %v", captured)
	}

	if _, hasID := captured["id"]; hasID {
		t.Fatalf("payload should omit empty id, got %v", captured)
	}
}

func TestClientDismissKeyboardSendsPayload(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dismissKeyboard" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.DismissKeyboard(context.Background(), "com.example.MyApp"); err != nil {
		t.Fatalf("dismissKeyboard: %v", err)
	}

	if captured["bundleId"] != "com.example.MyApp" {
		t.Fatalf("expected bundleId in payload, got %v", captured)
	}
}

func TestClientInputTextIncludesLabelWhenSet(t *testing.T) {
	t.Parallel()

	var captured map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))

	if err := client.InputText(context.Background(), "com.example.MyApp", Locator{Label: "Search"}, "needle", false); err != nil {
		t.Fatalf("inputText: %v", err)
	}

	if captured["label"] != "Search" {
		t.Fatalf("payload missing label, got %v", captured)
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

func TestTapSendsTheTextLocator(t *testing.T) {
	t.Parallel()

	var payload map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	if err := client.Tap(context.Background(), "com.example.MyApp", Locator{Text: "Sign in"}, 10, 20); err != nil {
		t.Fatalf("tap: %v", err)
	}

	if payload["text"] != "Sign in" {
		t.Fatalf("expected the text locator on the wire, got %v", payload)
	}

	// Only the locator that was set travels: sending empty id/label keys
	// would have the driver search for an element with a blank
	// identifier instead of matching on text.
	for _, absent := range []string{"id", "label"} {
		if _, ok := payload[absent]; ok {
			t.Fatalf("payload should not carry an empty %q, got %v", absent, payload)
		}
	}
}

func TestCoordinateOnlyTapSendsNoLocator(t *testing.T) {
	t.Parallel()

	var payload map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	if err := client.Tap(context.Background(), "com.example.MyApp", Locator{}, 10, 20); err != nil {
		t.Fatalf("tap: %v", err)
	}

	for _, absent := range []string{"id", "label", "text"} {
		if _, ok := payload[absent]; ok {
			t.Fatalf("a coordinate-only tap must send no locator keys, got %v", payload)
		}
	}
}

func TestLocatorIsEmpty(t *testing.T) {
	t.Parallel()

	if !(Locator{}).IsEmpty() {
		t.Fatal("a zero Locator must report empty")
	}

	for _, locator := range []Locator{{ID: "a"}, {Label: "b"}, {Text: "c"}} {
		if locator.IsEmpty() {
			t.Fatalf("%+v must not report empty", locator)
		}
	}
}

func TestInputTextKeepsContentAndLocatorApart(t *testing.T) {
	t.Parallel()

	var payload map[string]any

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	// Typing into an element located by its visible text is the case
	// that conflated the two: sending the content under "text" made the
	// driver look for an element named after what was being typed.
	err := client.InputText(
		context.Background(),
		"com.example.MyApp",
		Locator{Text: "Email"},
		"user@example.com",
		false,
	)
	if err != nil {
		t.Fatalf("input text: %v", err)
	}

	if payload[payloadInputValueKey] != "user@example.com" {
		t.Fatalf("content should travel as %q, got %v", payloadInputValueKey, payload)
	}

	if payload["text"] != "Email" {
		t.Fatalf("the text locator should stay under \"text\", got %v", payload)
	}
}
