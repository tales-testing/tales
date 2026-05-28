package http

import (
	"fmt"

	"github.com/tales-testing/tales/internal/provider"
)

// BasicAuth carries resolved basic authentication credentials.
type BasicAuth struct {
	Username string
	Password string
}

// RequestTemplate is a clonable description of an HTTP request resolved
// from a provider.Input request block. Callers (the load provider in
// particular) build many http.Request instances from a single template,
// wrapping Body in bytes.NewReader for each in-flight attempt so that
// concurrent requests do not share a reader.
type RequestTemplate struct {
	Method        string
	URL           string
	Headers       map[string]string
	ReportHeaders map[string]string
	Body          []byte
	BasicAuth     *BasicAuth
}

// BuildRequestTemplate resolves a provider.Input request map into a
// reusable template. Multipart bodies are rejected because they embed
// the encoder boundary into the Content-Type header and cannot be
// safely reused across attempts; the load provider documents this
// limitation in V1.
func BuildRequestTemplate(input provider.Input) (*RequestTemplate, error) {
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

	if bodyValue, ok := input.Request["body"]; ok && !bodyValue.IsNull() {
		if !bodyValue.Type().IsObjectType() && !bodyValue.Type().IsMapType() {
			return nil, fmt.Errorf("request.body must be object")
		}

		if multipartVal, ok := bodyValue.AsValueMap()["multipart"]; ok && !multipartVal.IsNull() {
			return nil, fmt.Errorf("multipart bodies are not supported in load requests")
		}
	}

	basic, err := resolveBasicAuth(input.Request, headers)
	if err != nil {
		return nil, fmt.Errorf("resolve basic auth: %w", err)
	}

	body, err := resolveBody(input, headers)
	if err != nil {
		return nil, fmt.Errorf("resolve body: %w", err)
	}

	reportHeaders := cloneHeaders(headers)

	tmpl := &RequestTemplate{
		Method:        method,
		URL:           requestURL,
		Headers:       headers,
		ReportHeaders: reportHeaders,
		Body:          body,
	}

	if basic != nil {
		tmpl.BasicAuth = &BasicAuth{Username: basic.username, Password: basic.password}
		reportHeaders["Authorization"] = maskedBasicAuth
	}

	return tmpl, nil
}
