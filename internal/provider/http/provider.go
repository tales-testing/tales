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

const (
	providerTypeHTTP = "http"
	headersKey       = "headers"
	headersAllKey    = "headers_all"
	cookiesKey       = "cookies"
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

	basicAuth, err := resolveBasicAuth(input.Request, headers)
	if err != nil {
		return nil, fmt.Errorf("resolve basic auth: %w", err)
	}

	body, err := resolveBody(input, headers)
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

	reportHeaders := cloneHeaders(headers)

	if basicAuth != nil {
		req.SetBasicAuth(basicAuth.username, basicAuth.password)

		reportHeaders["Authorization"] = "Basic ***"
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

	output, err := buildOutput(method, requestURL, reportHeaders, body, resp, respBytes, time.Since(start), input.Request)
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

	headersVal, ok := request[headersKey]
	if !ok || headersVal.IsNull() {
		return headers, nil
	}

	mapped, err := ctyMapString(headersVal)
	if err != nil {
		return nil, fmt.Errorf("request.headers: %w", err)
	}

	return mapped, nil
}

type basicAuthConfig struct {
	username string
	password string
}

func resolveBasicAuth(request map[string]cty.Value, headers map[string]string) (*basicAuthConfig, error) {
	authValue, ok := request["auth"]
	if !ok || authValue.IsNull() {
		return nil, nil
	}

	if !authValue.IsKnown() {
		return nil, fmt.Errorf("request.auth must be known")
	}

	if !authValue.Type().IsObjectType() && !authValue.Type().IsMapType() {
		return nil, fmt.Errorf("request.auth must be object")
	}

	authMap := authValue.AsValueMap()

	basicValue, ok := authMap["basic"]
	if !ok || basicValue.IsNull() {
		return nil, nil
	}

	if !basicValue.IsKnown() {
		return nil, fmt.Errorf("request.auth.basic must be known")
	}

	if hasAuthorizationHeader(headers) {
		return nil, fmt.Errorf("request cannot define both headers.Authorization and auth.basic")
	}

	if !basicValue.Type().IsObjectType() && !basicValue.Type().IsMapType() {
		return nil, fmt.Errorf("request.auth.basic must be object")
	}

	basicMap := basicValue.AsValueMap()

	username, err := authFieldString(basicMap, "username")
	if err != nil {
		return nil, err
	}

	password, err := authFieldString(basicMap, "password")
	if err != nil {
		return nil, err
	}

	return &basicAuthConfig{username: username, password: password}, nil
}

func hasAuthorizationHeader(headers map[string]string) bool {
	for key := range headers {
		if strings.EqualFold(key, "Authorization") {
			return true
		}
	}

	return false
}

func authFieldString(values map[string]cty.Value, key string) (string, error) {
	value, ok := values[key]
	if !ok {
		return "", fmt.Errorf("request.auth.basic.%s is required", key)
	}

	if !value.IsKnown() {
		return "", fmt.Errorf("request.auth.basic.%s must be known", key)
	}

	if value.IsNull() {
		return "", fmt.Errorf("request.auth.basic.%s must not be null", key)
	}

	if value.Type() != cty.String {
		return "", fmt.Errorf("request.auth.basic.%s must be a string", key)
	}

	return value.AsString(), nil
}

func resolveBody(input provider.Input, headers map[string]string) ([]byte, error) {
	bodyValue, ok := input.Request["body"]
	if !ok || bodyValue.IsNull() {
		return nil, nil
	}

	if !bodyValue.IsKnown() {
		return nil, fmt.Errorf("request.body must be known")
	}

	if !bodyValue.Type().IsObjectType() && !bodyValue.Type().IsMapType() {
		return nil, fmt.Errorf("request.body must be object")
	}

	bodyMap := bodyValue.AsValueMap()

	return resolveBodyMap(input, bodyMap, headers)
}

func resolveBodyMap(input provider.Input, bodyMap map[string]cty.Value, headers map[string]string) ([]byte, error) {
	var body []byte

	setFields := 0
	if jsonVal, ok := bodyMap["json"]; ok && !jsonVal.IsNull() {
		setFields++

		encoded, err := ctyjson.Marshal(jsonVal, jsonVal.Type())
		if err != nil {
			return nil, fmt.Errorf("marshal request.body.json: %w", err)
		}

		body = encoded

		setDefaultHeader(headers, "Content-Type", "application/json")
	}

	if formVal, ok := bodyMap["form"]; ok && !formVal.IsNull() {
		setFields++

		encoded, err := encodeFormBody(formVal)
		if err != nil {
			return nil, err
		}

		body = encoded

		setDefaultHeader(headers, "Content-Type", "application/x-www-form-urlencoded")
	}

	if rawVal, ok := bodyMap["raw"]; ok && !rawVal.IsNull() {
		setFields++

		encoded, err := encodeRawBody(rawVal)
		if err != nil {
			return nil, err
		}

		body = encoded
	}

	if multipartVal, ok := bodyMap["multipart"]; ok && !multipartVal.IsNull() {
		setFields++

		encoded, contentType, err := encodeMultipartBody(input, multipartVal)
		if err != nil {
			return nil, err
		}

		body = encoded

		// Force the multipart Content-Type, even if the caller already set
		// one: the encoder's boundary parameter is part of the header and
		// any user-supplied value (e.g. plain "multipart/form-data" without
		// boundary) would silently corrupt the request otherwise.
		overrideHeader(headers, "Content-Type", contentType)
	}

	if setFields == 0 {
		return nil, fmt.Errorf("request.body must define one of json, form, raw, or multipart")
	}

	if setFields > 1 {
		return nil, fmt.Errorf("request.body must define exactly one of json, form, raw, or multipart")
	}

	return body, nil
}

func encodeFormBody(formVal cty.Value) ([]byte, error) {
	form, err := ctyMapString(formVal)
	if err != nil {
		return nil, fmt.Errorf("request.body.form: %w", err)
	}

	values := url.Values{}

	for key, value := range form {
		values.Set(key, value)
	}

	return []byte(values.Encode()), nil
}

func encodeRawBody(rawVal cty.Value) ([]byte, error) {
	if !rawVal.IsKnown() {
		return nil, fmt.Errorf("request.body.raw must be known")
	}

	if rawVal.Type() != cty.String {
		return nil, fmt.Errorf("request.body.raw must be a string")
	}

	return []byte(rawVal.AsString()), nil
}

func setDefaultHeader(headers map[string]string, name, value string) {
	for key := range headers {
		if strings.EqualFold(key, name) {
			return
		}
	}

	headers[name] = value
}

// overrideHeader replaces any prior value (case-insensitive) for the given
// header name. Used when a body encoder produces a header that must match
// the wire payload exactly — for example multipart's Content-Type carries
// the encoder-generated boundary and would corrupt the request if the
// caller could pin a stale value.
func overrideHeader(headers map[string]string, name, value string) {
	for key := range headers {
		if strings.EqualFold(key, name) {
			delete(headers, key)
		}
	}

	headers[name] = value
}

func cloneHeaders(headers map[string]string) map[string]string {
	cloned := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		cloned[key] = value
	}

	return cloned
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
	responseJSON := decodeResponseJSON(resp.Header.Get("Content-Type"), respBytes)
	output := &provider.Output{
		Duration:   duration,
		StatusCode: resp.StatusCode,
		Request: map[string]cty.Value{
			"method":   cty.StringVal(method),
			"url":      cty.StringVal(requestURL),
			headersKey: toStringMapValue(headers),
		},
		Response: map[string]cty.Value{
			"status":      cty.NumberIntVal(int64(resp.StatusCode)),
			headersKey:    buildResponseHeaders(resp.Header),
			headersAllKey: buildResponseHeadersAll(resp.Header),
			cookiesKey:    buildResponseCookies(resp.Cookies()),
			"body":        cty.StringVal(string(respBytes)),
			"json":        responseJSON,
		},
	}

	if bodyVal, ok := request["body"]; ok {
		output.Request["body"] = bodyVal
	}

	if authVal, ok := request["auth"]; ok {
		output.Request["auth"] = authVal
	}

	return output, nil
}

// buildResponseHeaders exposes each header as its first value with the
// canonical MIME header name. The lowercase-duplicate keys produced by the
// previous implementation are gone — see CHANGELOG / docs for the breaking
// change. Multi-valued headers should be read via response.headers_all.
func buildResponseHeaders(header http.Header) cty.Value {
	if len(header) == 0 {
		return cty.EmptyObjectVal
	}

	values := make(map[string]cty.Value, len(header))
	for key, vs := range header {
		if len(vs) == 0 {
			continue
		}

		values[key] = cty.StringVal(vs[0])
	}

	if len(values) == 0 {
		return cty.EmptyObjectVal
	}

	return cty.ObjectVal(values)
}

// buildResponseHeadersAll exposes every value for every header as a list of
// strings, preserving wire order. Use this for Set-Cookie or any header whose
// semantics are multi-valued.
func buildResponseHeadersAll(header http.Header) cty.Value {
	if len(header) == 0 {
		return cty.EmptyObjectVal
	}

	values := make(map[string]cty.Value, len(header))
	for key, vs := range header {
		if len(vs) == 0 {
			values[key] = cty.ListValEmpty(cty.String)

			continue
		}

		listValues := make([]cty.Value, 0, len(vs))
		for _, v := range vs {
			listValues = append(listValues, cty.StringVal(v))
		}

		values[key] = cty.ListVal(listValues)
	}

	return cty.ObjectVal(values)
}

// buildResponseCookies parses Set-Cookie headers and exposes them as a map of
// cookie objects keyed by cookie name. Duplicate cookie names use the
// last-seen value, matching browser overwrite behavior.
func buildResponseCookies(cookies []*http.Cookie) cty.Value {
	if len(cookies) == 0 {
		return cty.EmptyObjectVal
	}

	mapped := make(map[string]cty.Value, len(cookies))
	for _, c := range cookies {
		mapped[c.Name] = cookieToCtyValue(c)
	}

	return cty.ObjectVal(mapped)
}

func cookieToCtyValue(c *http.Cookie) cty.Value {
	expires := ""
	if !c.Expires.IsZero() {
		expires = c.Expires.UTC().Format(time.RFC3339)
	}

	return cty.ObjectVal(map[string]cty.Value{
		"name":      cty.StringVal(c.Name),
		"value":     cty.StringVal(c.Value),
		"raw":       cty.StringVal(c.Raw),
		"path":      cty.StringVal(c.Path),
		"domain":    cty.StringVal(c.Domain),
		"expires":   cty.StringVal(expires),
		"max_age":   cty.NumberIntVal(int64(c.MaxAge)),
		"secure":    cty.BoolVal(c.Secure),
		"http_only": cty.BoolVal(c.HttpOnly),
		"same_site": cty.StringVal(sameSiteString(c.SameSite)),
	})
}

func sameSiteString(s http.SameSite) string {
	switch s {
	case http.SameSiteLaxMode:
		return "lax"
	case http.SameSiteStrictMode:
		return "strict"
	case http.SameSiteNoneMode:
		return "none"
	case http.SameSiteDefaultMode:
		return ""
	default:
		return ""
	}
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
	if !value.IsKnown() {
		return nil, fmt.Errorf("value must be known")
	}

	if value.IsNull() {
		return nil, fmt.Errorf("value must not be null")
	}

	if !value.Type().IsObjectType() && !value.Type().IsMapType() {
		return nil, fmt.Errorf("value must be object")
	}

	mapped := map[string]string{}

	for key, val := range value.AsValueMap() {
		if !val.IsKnown() {
			return nil, fmt.Errorf("key %q must be known", key)
		}

		if val.IsNull() {
			return nil, fmt.Errorf("key %q must not be null", key)
		}

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
