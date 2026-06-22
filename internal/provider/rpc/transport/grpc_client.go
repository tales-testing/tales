package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	rpcstatus "github.com/tales-testing/tales/internal/provider/rpc/status"
)

// GRPCConfig configures one gRPC transport. Address is the dial target;
// Plaintext disables TLS. TLS is applied when Plaintext is false; a nil
// TLS uses Go's defaults (system roots, TLS 1.2+). DefaultMetadata is the
// target-level metadata merged in before the per-call overrides; values are
// stored verbatim and only masked when populated on Result.
//
// Dial overrides the dial step for tests; production leaves it nil and the
// client uses grpc.NewClient with the address + TLS settings.
type GRPCConfig struct {
	Address         string
	Plaintext       bool
	TLS             *tls.Config
	DefaultMetadata map[string]string
	Dial            func(ctx context.Context) (*grpc.ClientConn, error)
}

// GRPCClient invokes unary gRPC methods against one address. Connections are
// reused for the lifetime of the client; Close releases the underlying
// *grpc.ClientConn. The client wraps grpc.ClientConn.Invoke with dynamic
// request/response messages; gRPC's default proto codec handles *dynamicpb.Message
// out of the box because dynamicpb satisfies proto.Message.
type GRPCClient struct {
	cfg  GRPCConfig
	once sync.Once
	conn *grpc.ClientConn
	dial error
}

// NewGRPCClient builds a client; the connection is opened lazily on the
// first Invoke so a target that is never used does not dial.
func NewGRPCClient(cfg GRPCConfig) *GRPCClient {
	return &GRPCClient{cfg: cfg}
}

// Invoke marshals call.Request via the default proto codec, dials lazily,
// applies headers + per-call metadata, applies the timeout, and decodes the
// response into a fresh *dynamicpb.Message based on call.Output. gRPC status
// errors are mapped to Result.Status / Result.Error; transport errors return
// a Go error from this method.
func (c *GRPCClient) Invoke(ctx context.Context, call Call) (*Result, error) {
	if call.Request == nil {
		return nil, errors.New("grpc client: call.Request is nil")
	}

	if call.Output == nil {
		return nil, errors.New("grpc client: call.Output is nil")
	}

	conn, err := c.acquire()
	if err != nil {
		return nil, err
	}

	if call.Timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, call.Timeout)
		defer cancel()
	}

	md := mergeMetadata(c.cfg.DefaultMetadata, call.Metadata)
	if len(md) > 0 {
		ctx = metadata.NewOutgoingContext(ctx, metadata.New(md))
	}

	var (
		headers  metadata.MD
		trailers metadata.MD
	)

	start := time.Now()

	resp := newDynamicResponse(call.Output)

	invokeErr := conn.Invoke(ctx, call.FullMethod, call.Request, resp, grpc.Header(&headers), grpc.Trailer(&trailers))
	duration := time.Since(start)

	result := &Result{
		Headers:  MaskHeaders(headers),
		Trailers: MaskHeaders(trailers),
		Metadata: MaskHeaders(headers),
		Duration: duration,
	}

	if invokeErr == nil {
		result.Status = rpcstatus.StatusOK
		result.Message = resp

		return result, nil
	}

	st, ok := status.FromError(invokeErr)
	if !ok {
		// Genuine transport error; surface to the caller.
		return nil, fmt.Errorf("grpc invoke %s: %w", call.FullMethod, invokeErr)
	}

	code := uint32(st.Code())
	result.Code = code
	result.Status = rpcstatus.FromCode(code)
	result.Error = &ErrorDetail{
		Code:    result.Status,
		CodeRaw: code,
		Message: st.Message(),
	}

	for _, d := range st.Details() {
		result.Error.Details = append(result.Error.Details, ErrorDetailItem{
			Type:  fmt.Sprintf("%T", d),
			Value: nil,
		})
	}

	return result, nil
}

// Close releases the underlying gRPC connection. Safe to call multiple times.
func (c *GRPCClient) Close() error {
	if c.conn == nil {
		return nil
	}

	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("grpc close: %w", err)
	}

	return nil
}

func (c *GRPCClient) acquire() (*grpc.ClientConn, error) {
	c.once.Do(func() {
		c.conn, c.dial = c.dialOnce(context.Background())
	})

	if c.dial != nil {
		return nil, c.dial
	}

	return c.conn, nil
}

func (c *GRPCClient) dialOnce(ctx context.Context) (*grpc.ClientConn, error) {
	if c.cfg.Dial != nil {
		return c.cfg.Dial(ctx)
	}

	if c.cfg.Address == "" {
		return nil, errors.New("grpc client: address is empty")
	}

	var opts []grpc.DialOption

	if c.cfg.Plaintext {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(c.cfg.TLS)))
	}

	conn, err := grpc.NewClient(c.cfg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", c.cfg.Address, err)
	}

	return conn, nil
}

// mergeMetadata returns the union of the two maps, with override winning on
// duplicate keys. Keys are lowercased per gRPC convention.
func mergeMetadata(defaults, override map[string]string) map[string]string {
	if len(defaults) == 0 && len(override) == 0 {
		return nil
	}

	out := make(map[string]string, len(defaults)+len(override))

	for k, v := range defaults {
		out[lowerKey(k)] = v
	}

	for k, v := range override {
		out[lowerKey(k)] = v
	}

	return out
}

func lowerKey(key string) string {
	out := make([]byte, len(key))

	for i := range len(key) {
		b := key[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}

		out[i] = b
	}

	return string(out)
}

// newDynamicResponse allocates an empty *dynamicpb.Message of the response
// type for the call so grpc's default proto codec can unmarshal directly
// into it.
func newDynamicResponse(output protoreflect.MessageDescriptor) *dynamicpb.Message {
	return dynamicpb.NewMessage(output)
}
