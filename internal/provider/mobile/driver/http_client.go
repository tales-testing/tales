package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tales-testing/tales/internal/provider/mobile/tree"
)

const (
	// DefaultRequestTimeout covers every driver endpoint. The /inputText
	// path can take a few seconds when the driver falls back to
	// char-by-char typing to dodge the iOS strong-password autofill
	// banner on SecureField(.newPassword). 30s leaves headroom without
	// hiding genuine driver hangs.
	//
	// It is a default rather than a constant because a shared CI runner
	// is a different machine: XCUIApplication.launch() has been measured
	// at 30s+ there against 5s on a developer Mac, and abandoning a
	// launch the driver goes on to complete desynchronizes the two sides
	// for the rest of the run. Raise it with driver.timeout on the
	// target rather than accepting that cascade.
	DefaultRequestTimeout = 30 * time.Second
	bodySnippetLimit      = 256

	payloadBundleIDKey = "bundleId"
	// payloadInputValueKey carries the text /inputText should type.
	//
	// It is deliberately not "text": "text" names the text *locator* on
	// every route, and reusing it here made an input_text action send
	// its own content as the element to look for. The DSL calls this
	// field value, so the wire does too.
	payloadInputValueKey = "value"
	payloadDurationKey   = "duration"
)

// Client is the HTTP/JSON implementation of Driver, targeting the
// TalesAppleDriver running inside an iOS Simulator.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the default *http.Client. Mainly used in tests.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// WithTimeout overrides the per-request timeout.
//
// A non-positive duration is ignored so callers can pass an unset
// configuration value through without branching; "no timeout at all" is
// deliberately not expressible, since it would turn a wedged driver into
// a hung suite.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if timeout <= 0 {
			return
		}

		c.httpClient.Timeout = timeout
	}
}

// New returns a Client pointing at the driver's base URL.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: DefaultRequestTimeout,
			Transport: &http.Transport{
				// Both drivers answer one request per connection and
				// close it (`Connection: close`), so pooling buys
				// nothing — and it costs a real failure: the transport
				// can hand a POST a connection the driver has already
				// closed, and Go does not retry a non-idempotent
				// request. The symptom is a single unexplained EOF on
				// the first POST after a GET, with nothing in the
				// driver log because the request never arrived.
				DisableKeepAlives: true,
			},
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Health pings GET /health.
func (c *Client) Health(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.errorFromResponse("/health", resp)
	}

	var payload struct {
		Status string `json:"status"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode /health response: %w", err)
	}

	if payload.Status != "ok" {
		return fmt.Errorf("driver /health returned status %q", payload.Status)
	}

	return nil
}

// Hierarchy fetches GET /hierarchy?bundleId=<bundleID>.
func (c *Client) Hierarchy(ctx context.Context, bundleID string) (*tree.ViewNode, error) {
	if bundleID == "" {
		return nil, fmt.Errorf("hierarchy: bundleID is required")
	}

	path := "/hierarchy?bundleId=" + url.QueryEscape(bundleID)

	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.errorFromResponse("/hierarchy", resp)
	}

	var root tree.ViewNode

	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return nil, fmt.Errorf("decode /hierarchy response: %w", err)
	}

	return &root, nil
}

// Tap posts to /tap.
func (c *Client) Tap(ctx context.Context, bundleID string, locator Locator, x, y float64) error {
	payload := map[string]any{payloadBundleIDKey: bundleID, "x": x, "y": y}
	applyLocator(payload, locator)

	return c.postJSON(ctx, "/tap", payload)
}

// Swipe posts to /swipe.
func (c *Client) Swipe(ctx context.Context, bundleID string, startX, startY, endX, endY, duration float64) error {
	payload := map[string]any{
		payloadBundleIDKey: bundleID,
		"startX":           startX,
		"startY":           startY,
		"endX":             endX,
		"endY":             endY,
		payloadDurationKey: duration,
	}

	return c.postJSON(ctx, "/swipe", payload)
}

// LongPress posts to /longPress.
func (c *Client) LongPress(ctx context.Context, bundleID string, locator Locator, x, y, duration float64) error {
	payload := map[string]any{payloadBundleIDKey: bundleID, "x": x, "y": y, payloadDurationKey: duration}
	applyLocator(payload, locator)

	return c.postJSON(ctx, "/longPress", payload)
}

// DoubleTap posts to /doubleTap.
func (c *Client) DoubleTap(ctx context.Context, bundleID string, locator Locator, x, y float64) error {
	payload := map[string]any{payloadBundleIDKey: bundleID, "x": x, "y": y}
	applyLocator(payload, locator)

	return c.postJSON(ctx, "/doubleTap", payload)
}

// PressKey posts to /pressKey.
func (c *Client) PressKey(ctx context.Context, bundleID, key string) error {
	return c.postJSON(ctx, "/pressKey", map[string]any{payloadBundleIDKey: bundleID, "key": key})
}

// PressButton posts to /pressButton.
func (c *Client) PressButton(ctx context.Context, bundleID, button string) error {
	return c.postJSON(ctx, "/pressButton", map[string]any{payloadBundleIDKey: bundleID, "button": button})
}

// SetOrientation posts to /orientation.
func (c *Client) SetOrientation(ctx context.Context, orientation string) error {
	return c.postJSON(ctx, "/orientation", map[string]any{"orientation": orientation})
}

// InputText posts to /inputText.
func (c *Client) InputText(ctx context.Context, bundleID string, locator Locator, text string, paste bool) error {
	payload := map[string]any{payloadBundleIDKey: bundleID, payloadInputValueKey: text}
	applyLocator(payload, locator)

	if paste {
		payload["paste"] = true
	}

	return c.postJSON(ctx, "/inputText", payload)
}

// EraseText posts to /eraseText.
func (c *Client) EraseText(ctx context.Context, bundleID string, locator Locator, characters int) error {
	if characters < 0 {
		return fmt.Errorf("eraseText: characters must be non-negative, got %d", characters)
	}

	payload := map[string]any{payloadBundleIDKey: bundleID, "characters": characters}
	applyLocator(payload, locator)

	return c.postJSON(ctx, "/eraseText", payload)
}

// DismissKeyboard posts to /dismissKeyboard.
func (c *Client) DismissKeyboard(ctx context.Context, bundleID string) error {
	return c.postJSON(ctx, "/dismissKeyboard", map[string]any{payloadBundleIDKey: bundleID})
}

// ScrollTo posts to /scrollTo.
func (c *Client) ScrollTo(ctx context.Context, bundleID string, locator Locator) error {
	payload := map[string]any{payloadBundleIDKey: bundleID}
	applyLocator(payload, locator)

	return c.postJSON(ctx, "/scrollTo", payload)
}

// Screenshot fetches GET /screenshot returning the raw PNG bytes.
func (c *Client) Screenshot(ctx context.Context) ([]byte, error) {
	resp, err := c.do(ctx, http.MethodGet, "/screenshot", nil)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.errorFromResponse("/screenshot", resp)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read /screenshot body: %w", err)
	}

	return data, nil
}

// Launch posts to /launch so the driver relaunches the app through
// XCUIApplication.launch(), re-establishing XCTest's automation session
// with the running process.
func (c *Client) Launch(ctx context.Context, bundleID string) error {
	if bundleID == "" {
		return fmt.Errorf("launch: bundleID is required")
	}

	return c.postJSON(ctx, "/launch", map[string]any{payloadBundleIDKey: bundleID})
}

// Activate posts to /activate so the driver binds its automation session
// to an app the host already launched.
func (c *Client) Activate(ctx context.Context, bundleID string) error {
	if bundleID == "" {
		return fmt.Errorf("activate: bundleID is required")
	}

	return c.postJSON(ctx, "/activate", map[string]any{payloadBundleIDKey: bundleID})
}

// Terminate posts to /terminate so the driver terminates the app through
// XCUIApplication.terminate(), keeping XCTest's process model in sync.
func (c *Client) Terminate(ctx context.Context, bundleID string) error {
	if bundleID == "" {
		return fmt.Errorf("terminate: bundleID is required")
	}

	return c.postJSON(ctx, "/terminate", map[string]any{payloadBundleIDKey: bundleID})
}

func (c *Client) postJSON(ctx context.Context, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s payload: %w", path, err)
	}

	resp, err := c.do(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.errorFromResponse(path, resp)
	}

	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build %s %s: %w", method, path, err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	//nolint:gosec // G107: baseURL comes from the validated target.driver config, not from user-controlled input
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %s %s: %w", method, path, err)
	}

	return resp, nil
}

func (c *Client) errorFromResponse(endpoint string, resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, bodySnippetLimit))

	return fmt.Errorf("driver %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(snippet)))
}

// applyLocator writes the locator onto a request payload.
//
// Only non-empty fields are written, so a coordinate-only command sends
// no locator keys at all and the driver takes the (x,y) path rather than
// searching for an element with an empty identifier.
func applyLocator(payload map[string]any, locator Locator) {
	if locator.ID != "" {
		payload["id"] = locator.ID
	}

	if locator.Label != "" {
		payload["label"] = locator.Label
	}

	if locator.Text != "" {
		payload["text"] = locator.Text
	}
}
