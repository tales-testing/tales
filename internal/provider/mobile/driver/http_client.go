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
	defaultRequestTimeout = 10 * time.Second
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
func (c *Client) Tap(ctx context.Context, x, y float64) error {
	return c.postJSON(ctx, "/tap", map[string]float64{"x": x, "y": y})
}

// InputText posts to /inputText.
func (c *Client) InputText(ctx context.Context, text string) error {
	return c.postJSON(ctx, "/inputText", map[string]string{"text": text})
}

// EraseText posts to /eraseText.
func (c *Client) EraseText(ctx context.Context, characters int) error {
	if characters < 0 {
		return fmt.Errorf("eraseText: characters must be non-negative, got %d", characters)
	}

	return c.postJSON(ctx, "/eraseText", map[string]int{"characters": characters})
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

	transport := c.httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("call %s %s: %w", method, path, err)
	}

	return resp, nil
}

func (c *Client) errorFromResponse(endpoint string, resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, bodySnippetLimit))

	return fmt.Errorf("driver %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(snippet)))
}
