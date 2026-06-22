package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	rpcstatus "github.com/tales-testing/tales/internal/provider/rpc/status"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Connect protocol envelope constants per https://connectrpc.com/docs/protocol.
const (
	connectProtocolHeader  = "Connect-Protocol-Version"
	connectProtocolVersion = "1"
	connectContentTypeJSON = "application/json"
	connectContentTypePB   = "application/proto"
)

// ConnectConfig configures the Connect HTTP transport for one target.
// BaseURL is the prefix; FullMethod is appended at call time. Encoding
// selects the wire format. DefaultHeaders are merged before the per-call
// overrides. Types resolves Protobuf messages for protojson marshaling.
type ConnectConfig struct {
	BaseURL        string
	Encoding       string // "json" | "proto"
	TLS            *tls.Config
	DefaultHeaders map[string]string
	Types          *protoregistry.Types
	HTTPClient     *http.Client // optional, defaults to one built from TLS
}

// ConnectClient is a unary Connect transport built on net/http. The Connect
// protocol for unary is plain HTTP POST with a typed Content-Type — no
// streaming framing, no compression handshake — so a direct net/http
// implementation is simpler than wiring connectrpc.com/connect plus a
// custom codec for dynamicpb.Message, and it carries one fewer dependency.
type ConnectClient struct {
	cfg    ConnectConfig
	client *http.Client
}

// NewConnectClient returns a client. The HTTP client is configured with the
// supplied TLS settings; HTTP/2 is left to Go's defaults (Transport upgrades
// automatically when TLS negotiates h2).
func NewConnectClient(cfg ConnectConfig) *ConnectClient {
	if cfg.Encoding == "" {
		cfg.Encoding = "json"
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Transport: connectTransport(cfg.TLS)}
	}

	return &ConnectClient{cfg: cfg, client: httpClient}
}

// Invoke marshals the request, sends the POST, and either decodes the body
// into the response message (2xx) or parses a Connect error envelope (non
// 2xx). Transport-level errors return a Go error; protocol errors are
// surfaced via Result.Status / Result.Error.
func (c *ConnectClient) Invoke(ctx context.Context, call Call) (*Result, error) {
	if call.Request == nil {
		return nil, errors.New("connect client: call.Request is nil")
	}

	if call.Output == nil {
		return nil, errors.New("connect client: call.Output is nil")
	}

	body, contentType, err := c.marshalRequest(call.Request)
	if err != nil {
		return nil, err
	}

	if call.Timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, call.Timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+call.FullMethod, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("connect build request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set(connectProtocolHeader, connectProtocolVersion)

	for k, v := range c.cfg.DefaultHeaders {
		req.Header.Set(k, v)
	}

	for k, v := range call.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()

	resp, err := c.client.Do(req)
	duration := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", call.FullMethod, err)
	}

	defer func() { _ = resp.Body.Close() }()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("connect read body: %w", err)
	}

	result := &Result{
		Headers:  MaskHeaders(resp.Header),
		Trailers: MaskHeaders(resp.Trailer),
		Duration: duration,
	}

	if resp.StatusCode/100 == 2 {
		return c.finishSuccess(result, call.Output, responseBody, resp.Header.Get("Content-Type"))
	}

	return c.finishError(result, resp.StatusCode, responseBody, resp.Header.Get("Content-Type")), nil
}

// Close is a no-op for the HTTP-backed transport; idle connections are
// cleaned up by the http.Transport's normal lifecycle. It exists to satisfy
// the Transport interface so the provider can call Close uniformly across
// gRPC and Connect.
func (c *ConnectClient) Close() error {
	if t, ok := c.client.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}

	return nil
}

// connectTransport builds an http.Transport with the supplied TLS config.
// A custom Transport is required so the user's TLS settings take effect;
// http.DefaultTransport carries the standard library's defaults only.
func connectTransport(tlsCfg *tls.Config) *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}

	clone := base.Clone()
	if tlsCfg != nil {
		clone.TLSClientConfig = tlsCfg
	}

	return clone
}

func (c *ConnectClient) marshalRequest(msg *dynamicpb.Message) ([]byte, string, error) {
	switch c.cfg.Encoding {
	case "proto":
		bytes, err := proto.Marshal(msg)
		if err != nil {
			return nil, "", fmt.Errorf("connect marshal proto: %w", err)
		}

		return bytes, connectContentTypePB, nil
	default: // json (the default)
		opts := protojson.MarshalOptions{Resolver: c.cfg.Types}

		bytes, err := opts.Marshal(msg)
		if err != nil {
			return nil, "", fmt.Errorf("connect marshal json: %w", err)
		}

		return bytes, connectContentTypeJSON, nil
	}
}

func (c *ConnectClient) finishSuccess(result *Result, output protoreflect.MessageDescriptor, body []byte, contentType string) (*Result, error) {
	msg := dynamicpb.NewMessage(output)

	if err := c.unmarshalResponse(body, contentType, msg); err != nil {
		return nil, fmt.Errorf("connect decode response: %w", err)
	}

	result.Status = rpcstatus.StatusOK
	result.Message = msg

	return result, nil
}

func (c *ConnectClient) finishError(result *Result, httpStatus int, body []byte, contentType string) *Result {
	// Connect error envelopes are JSON regardless of the request encoding.
	envelope := struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Details []json.RawMessage `json:"details,omitempty"`
	}{}

	if strings.Contains(contentType, "json") {
		_ = json.Unmarshal(body, &envelope)
	} else if len(body) > 0 && body[0] == '{' {
		// Some servers omit the Content-Type on errors; try anyway.
		_ = json.Unmarshal(body, &envelope)
	}

	canonical, _ := rpcstatus.Normalize(envelope.Code)

	code := connectHTTPStatusToCode(httpStatus)
	if canonical == "" {
		canonical = rpcstatus.FromCode(code)
	}

	result.Status = canonical
	result.Code = code
	result.Error = &ErrorDetail{
		Code:    canonical,
		CodeRaw: result.Code,
		Message: envelope.Message,
	}

	for _, raw := range envelope.Details {
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}

		detailType, _ := item["type"].(string)
		result.Error.Details = append(result.Error.Details, ErrorDetailItem{
			Type:  detailType,
			Value: item,
		})
	}

	return result
}

func (c *ConnectClient) unmarshalResponse(body []byte, contentType string, msg *dynamicpb.Message) error {
	if strings.Contains(contentType, "proto") {
		if err := proto.Unmarshal(body, msg); err != nil {
			return fmt.Errorf("proto unmarshal: %w", err)
		}

		return nil
	}

	opts := protojson.UnmarshalOptions{Resolver: c.cfg.Types, DiscardUnknown: false}
	if err := opts.Unmarshal(body, msg); err != nil {
		return fmt.Errorf("json unmarshal: %w", err)
	}

	return nil
}

// connectHTTPStatusToCode maps a Connect HTTP status code to its gRPC code
// numeric equivalent per the Connect spec. Unknown HTTP codes degrade to
// gRPC "unknown" (2).
func connectHTTPStatusToCode(httpStatus int) uint32 {
	switch httpStatus {
	case http.StatusBadRequest:
		return 3 // invalid_argument
	case http.StatusUnauthorized:
		return 16 // unauthenticated
	case http.StatusForbidden:
		return 7 // permission_denied
	case http.StatusNotFound:
		return 12 // unimplemented (Connect's convention for HTTP 404)
	case http.StatusRequestTimeout:
		return 4 // deadline_exceeded
	case http.StatusConflict:
		return 10 // aborted
	case http.StatusPreconditionFailed:
		return 9 // failed_precondition
	case http.StatusTooManyRequests:
		return 8 // resource_exhausted
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return 14 // unavailable
	case http.StatusInternalServerError, http.StatusNotImplemented:
		return 13 // internal
	default:
		return 2 // unknown
	}
}
