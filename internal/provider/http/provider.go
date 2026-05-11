package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hyperxlab/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

const providerTypeHTTP = "http"

// Provider executes HTTP steps.
type Provider struct {
	client *http.Client
}

// New creates HTTP provider.
func New() *Provider {
	return &Provider{client: &http.Client{Timeout: 30 * time.Second}}
}

// Type returns provider type.
func (p *Provider) Type() string {
	return providerTypeHTTP
}

// Execute runs one HTTP request.
func (p *Provider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	method := resolveMethod(input.Request)

	requestURL, err := resolveURL(input.Request)
	if err != nil {
		return nil, fmt.Errorf("resolve request url: %w", err)
	}

	requestURL, err = appendQuery(requestURL, input.Request)
	if err != nil {
		return nil, fmt.Errorf("append query: %w", err)
	}

	headers, err := resolveHeaders(input.Request)
	if err != nil {
		return nil, fmt.Errorf("resolve headers: %w", err)
	}

	body, err := resolveBody(input.Request, headers)
	if err != nil {
		return nil, fmt.Errorf("resolve body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request failed: %w", err)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	client := p.client
	if input.Timeout > 0 {
		copyClient := *p.client
		copyClient.Timeout = input.Timeout
		client = &copyClient
	}

	start := time.Now()

	resp, err := doRequest(client, req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}

	output, err := buildOutput(method, requestURL, headers, body, resp, respBytes, time.Since(start), input.Request)
	if err != nil {
		return nil, fmt.Errorf("build output: %w", err)
	}

	return output, nil
}

func doRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("round trip failed: %w", err)
	}

	return resp, nil
}

func resolveMethod(request map[string]cty.Value) string {
	method := "GET"
	if v, ok := request["method"]; ok && !v.IsNull() && v.Type() == cty.String {
		method = strings.ToUpper(v.AsString())
	}

	return method
}

func resolveURL(request map[string]cty.Value) (string, error) {
	urlValue, ok := request["url"]
	if !ok || urlValue.IsNull() || urlValue.Type() != cty.String {
		return "", fmt.Errorf("request.url must be a string")
	}

	requestURL := urlValue.AsString()

	parsed, err := url.Parse(requestURL)
	if err != nil {
		return "", fmt.Errorf("invalid request url: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("request.url must include host")
	}

	parsed.User = nil
	parsed.Fragment = ""

	return parsed.String(), nil
}

func appendQuery(requestURL string, request map[string]cty.Value) (string, error) {
	queryVal, ok := request["query"]
	if !ok || queryVal.IsNull() {
		return requestURL, nil
	}

	query, err := ctyMapString(queryVal)
	if err != nil {
		return "", fmt.Errorf("request.query: %w", err)
	}

	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return "", fmt.Errorf("invalid request url: %w", err)
	}

	values := parsedURL.Query()
	for k, v := range query {
		values.Set(k, v)
	}

	parsedURL.RawQuery = values.Encode()

	return parsedURL.String(), nil
}

func resolveHeaders(request map[string]cty.Value) (map[string]string, error) {
	headers := map[string]string{}

	headersVal, ok := request["headers"]
	if !ok || headersVal.IsNull() {
		return headers, nil
	}

	mapped, err := ctyMapString(headersVal)
	if err != nil {
		return nil, fmt.Errorf("request.headers: %w", err)
	}

	return mapped, nil
}

func resolveBody(request map[string]cty.Value, headers map[string]string) ([]byte, error) {
	var body []byte

	if jsonVal, ok := request["json"]; ok && !jsonVal.IsNull() {
		encoded, err := ctyjson.Marshal(jsonVal, jsonVal.Type())
		if err != nil {
			return nil, fmt.Errorf("marshal request.json: %w", err)
		}

		body = encoded

		if _, exists := headers["Content-Type"]; !exists {
			headers["Content-Type"] = "application/json"
		}
	}

	if bodyVal, ok := request["body"]; ok && !bodyVal.IsNull() {
		if bodyVal.Type() != cty.String {
			return nil, fmt.Errorf("request.body must be a string")
		}

		body = []byte(bodyVal.AsString())
	}

	return body, nil
}

func buildOutput(
	method, requestURL string,
	headers map[string]string,
	body []byte,
	resp *http.Response,
	respBytes []byte,
	duration time.Duration,
	request map[string]cty.Value,
) (*provider.Output, error) {
	responseHeaders := map[string]cty.Value{}

	for key, values := range resp.Header {
		joined := strings.Join(values, ",")
		responseHeaders[key] = cty.StringVal(joined)
		responseHeaders[strings.ToLower(key)] = cty.StringVal(joined)
	}

	responseJSON := decodeResponseJSON(resp.Header.Get("Content-Type"), respBytes)
	output := &provider.Output{
		Duration:   duration,
		StatusCode: resp.StatusCode,
		Request: map[string]cty.Value{
			"method":  cty.StringVal(method),
			"url":     cty.StringVal(requestURL),
			"headers": toStringMapValue(headers),
			"body":    cty.StringVal(string(body)),
		},
		Response: map[string]cty.Value{
			"status":  cty.NumberIntVal(int64(resp.StatusCode)),
			"headers": cty.ObjectVal(responseHeaders),
			"body":    cty.StringVal(string(respBytes)),
			"json":    responseJSON,
		},
	}

	if jsonVal, ok := request["json"]; ok {
		output.Request["json"] = jsonVal
	}

	return output, nil
}

func decodeResponseJSON(contentType string, respBytes []byte) cty.Value {
	if len(respBytes) == 0 {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	if !strings.Contains(contentType, "application/json") && !strings.Contains(contentType, "+json") && !jsonLike(respBytes) {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	inputType, typeErr := ctyjson.ImpliedType(respBytes)
	if typeErr != nil {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	decoded, decodeErr := ctyjson.Unmarshal(respBytes, inputType)
	if decodeErr != nil {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	return decoded
}

func toStringMapValue(values map[string]string) cty.Value {
	if len(values) == 0 {
		return cty.EmptyObjectVal
	}

	mapped := make(map[string]cty.Value, len(values))
	for k, v := range values {
		mapped[k] = cty.StringVal(v)
	}

	return cty.ObjectVal(mapped)
}

func ctyMapString(value cty.Value) (map[string]string, error) {
	if !value.Type().IsObjectType() && !value.Type().IsMapType() {
		return nil, fmt.Errorf("value must be object")
	}

	mapped := map[string]string{}

	for key, val := range value.AsValueMap() {
		if val.Type() != cty.String {
			return nil, fmt.Errorf("key %q must be string", key)
		}

		mapped[key] = val.AsString()
	}

	return mapped, nil
}

func jsonLike(data []byte) bool {
	trimmed := strings.TrimSpace(string(data))

	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}
