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

	"github.com/hyperxlab/tales/internal/provider/mobile/tree"
)

const (
	// defaultRequestTimeout covers every driver endpoint. The /inputText
	// path can take a few seconds when the driver falls back to
	// char-by-char typing to dodge the iOS strong-password autofill
	// banner on SecureField(.newPassword). 30s leaves headroom without
	// hiding genuine driver hangs.
	defaultRequestTimeout = 30 * time.Second
	bodySnippetLimit      = 256
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

// New returns a Client pointing at the driver's base URL.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: defaultRequestTimeout},
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
func (c *Client) Tap(ctx context.Context, bundleID, id string, x, y float64) error {
	payload := map[string]any{"bundleId": bundleID, "x": x, "y": y}
	if id != "" {
		payload["id"] = id
	}

	return c.postJSON(ctx, "/tap", payload)
}

// Swipe posts to /swipe.
func (c *Client) Swipe(ctx context.Context, bundleID string, startX, startY, endX, endY, duration float64) error {
	payload := map[string]any{
		"bundleId": bundleID,
		"startX":   startX,
		"startY":   startY,
		"endX":     endX,
		"endY":     endY,
		"duration": duration,
	}

	return c.postJSON(ctx, "/swipe", payload)
}

// LongPress posts to /longPress.
func (c *Client) LongPress(ctx context.Context, bundleID, id string, x, y, duration float64) error {
	payload := map[string]any{"bundleId": bundleID, "x": x, "y": y, "duration": duration}
	if id != "" {
		payload["id"] = id
	}

	return c.postJSON(ctx, "/longPress", payload)
}

// DoubleTap posts to /doubleTap.
func (c *Client) DoubleTap(ctx context.Context, bundleID, id string, x, y float64) error {
	payload := map[string]any{"bundleId": bundleID, "x": x, "y": y}
	if id != "" {
		payload["id"] = id
	}

	return c.postJSON(ctx, "/doubleTap", payload)
}

// PressKey posts to /pressKey.
func (c *Client) PressKey(ctx context.Context, bundleID, key string) error {
	return c.postJSON(ctx, "/pressKey", map[string]any{"bundleId": bundleID, "key": key})
}

// PressButton posts to /pressButton.
func (c *Client) PressButton(ctx context.Context, bundleID, button string) error {
	return c.postJSON(ctx, "/pressButton", map[string]any{"bundleId": bundleID, "button": button})
}

// SetOrientation posts to /orientation.
func (c *Client) SetOrientation(ctx context.Context, orientation string) error {
	return c.postJSON(ctx, "/orientation", map[string]any{"orientation": orientation})
}

// InputText posts to /inputText.
func (c *Client) InputText(ctx context.Context, bundleID, id, text string, paste bool) error {
	payload := map[string]any{"bundleId": bundleID, "text": text}
	if id != "" {
		payload["id"] = id
	}

	if paste {
		payload["paste"] = true
	}

	return c.postJSON(ctx, "/inputText", payload)
}

// EraseText posts to /eraseText.
func (c *Client) EraseText(ctx context.Context, bundleID string, characters int) error {
	if characters < 0 {
		return fmt.Errorf("eraseText: characters must be non-negative, got %d", characters)
	}

	return c.postJSON(ctx, "/eraseText", map[string]any{"bundleId": bundleID, "characters": characters})
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
