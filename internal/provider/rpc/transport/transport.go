// Package transport implements the on-the-wire dispatch for ConnectRPC and
// native gRPC unary calls using dynamic Protobuf messages. Each protocol has
// its own Transport implementation; the upper layers (provider) pick one
// based on the resolved target configuration and share the descriptor +
// codec pipeline through the Call / Result types below.
package transport

import (
	"context"
	"io"
	"time"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Call is the per-request payload the provider hands to a Transport. Headers
// and Metadata are already merged from target defaults plus step overrides;
// the transport applies them verbatim and is responsible for masking secrets
// only when populating Result.
type Call struct {
	FullMethod string                         // canonical /pkg.Service/Method
	Service    string                         // pkg.Service
	Method     string                         // Method
	Output     protoreflect.MessageDescriptor // response descriptor
	Request    *dynamicpb.Message             // marshaled by the codec layer
	Types      *protoregistry.Types           // for protojson resolver
	Headers    map[string]string              // Connect only
	Metadata   map[string]string              // gRPC only
	Timeout    time.Duration
}

// Result is what every Transport returns for a unary call. Status is the
// canonical lowercase snake_case name (ok | invalid_argument | ...);
// Code is the raw numeric value the protocol returned. Message is the
// decoded response when Status == "ok", nil otherwise. Headers / Trailers /
// Metadata are already masked.
type Result struct {
	Status   string
	Code     uint32
	Message  *dynamicpb.Message
	Headers  map[string][]string
	Metadata map[string][]string
	Trailers map[string][]string
	Error    *ErrorDetail
	Duration time.Duration
}

// ErrorDetail carries the structured failure payload the user asserts under
// expect { error = {...} }. Details are best-effort: protocols may surface
// rich detail messages; the transport stores them as opaque JSON-shaped
// values so the upper layer does not have to know about google.rpc.Status.
type ErrorDetail struct {
	Code    string
	CodeRaw uint32
	Message string
	Details []ErrorDetailItem
}

// ErrorDetailItem is one structured detail from an error. Type is the
// fully-qualified message name; Value is the protojson-decoded payload as a
// map[string]any.
type ErrorDetailItem struct {
	Type  string
	Value map[string]any
}

// Transport invokes one unary call and returns its Result. Transport-level
// failures (connection refused, TLS handshake error, ...) return an error;
// a protocol error (gRPC status != OK, Connect error response) is encoded
// in Result.Status / Result.Error so the user can assert on it.
type Transport interface {
	Invoke(ctx context.Context, call Call) (*Result, error)
	io.Closer
}
