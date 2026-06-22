package model

// RPCCall is the parsed data for an rpc provider step. The provider speaks
// ConnectRPC or native gRPC with Protobuf descriptors loaded at runtime
// (no codegen), driven by target, descriptor, service and method fields
// resolved at execution time. V1 supports unary RPC only.
type RPCCall struct {
	Target   Expression
	Service  Expression
	Method   Expression
	Message  Expression
	Headers  Expression
	Metadata Expression
	Timeout  Expression
	Expect   *RPCExpect
}

// RPCExpect carries the RPC-specific assertions. Status accepts the lowercase
// snake_case canonical codes (ok | invalid_argument | not_found | ...). Error
// matches against the structured error payload exposed under response.error,
// Message against response.message, Headers/Metadata/Trailers against the
// corresponding maps. The runtime walks each field with the shared assertion
// engine; absent fields are not asserted.
type RPCExpect struct {
	Status   Expression
	Error    Expression
	Message  Expression
	Headers  Expression
	Metadata Expression
	Trailers Expression
}
