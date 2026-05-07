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
	return "http"
}

// Execute runs one HTTP request.
func (p *Provider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	method := "GET"
	if v, ok := input.Request["method"]; ok && !v.IsNull() && v.Type() == cty.String {
		method = strings.ToUpper(v.AsString())
	}

	urlValue, ok := input.Request["url"]
	if !ok || urlValue.IsNull() || urlValue.Type() != cty.String {
		return nil, fmt.Errorf("request.url must be a string")
	}
	requestURL := urlValue.AsString()

	if queryVal, ok := input.Request["query"]; ok && !queryVal.IsNull() {
		query, err := ctyMapString(queryVal)
		if err != nil {
			return nil, fmt.Errorf("request.query: %w", err)
		}
		parsedURL, err := url.Parse(requestURL)
		if err != nil {
			return nil, fmt.Errorf("invalid request url: %w", err)
		}
		q := parsedURL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		parsedURL.RawQuery = q.Encode()
		requestURL = parsedURL.String()
	}

	headers := map[string]string{}
	if headersVal, ok := input.Request["headers"]; ok && !headersVal.IsNull() {
		var err error
		headers, err = ctyMapString(headersVal)
		if err != nil {
			return nil, fmt.Errorf("request.headers: %w", err)
		}
	}

	var body []byte
	if jsonVal, ok := input.Request["json"]; ok && !jsonVal.IsNull() {
		encoded, err := ctyjson.Marshal(jsonVal, jsonVal.Type())
		if err != nil {
			return nil, fmt.Errorf("marshal request.json: %w", err)
		}
		body = encoded
		if _, exists := headers["Content-Type"]; !exists {
			headers["Content-Type"] = "application/json"
		}
	}
	if bodyVal, ok := input.Request["body"]; ok && !bodyVal.IsNull() {
		if bodyVal.Type() != cty.String {
			return nil, fmt.Errorf("request.body must be a string")
		}
		body = []byte(bodyVal.AsString())
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
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}
	duration := time.Since(start)

	responseHeaders := map[string]cty.Value{}
	for key, values := range resp.Header {
		responseHeaders[key] = cty.StringVal(strings.Join(values, ","))
	}

	responseJSON := cty.NullVal(cty.DynamicPseudoType)
	if len(respBytes) > 0 {
		contentType := resp.Header.Get("Content-Type")
		if strings.Contains(contentType, "application/json") || strings.Contains(contentType, "+json") || jsonLike(respBytes) {
			inputType, typeErr := ctyjson.ImpliedType(respBytes)
			if typeErr == nil {
				decoded, decodeErr := ctyjson.Unmarshal(respBytes, inputType)
				if decodeErr == nil {
					responseJSON = decoded
				}
			}
		}
	}

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
	if jsonVal, ok := input.Request["json"]; ok {
		output.Request["json"] = jsonVal
	}
	return output, nil
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
