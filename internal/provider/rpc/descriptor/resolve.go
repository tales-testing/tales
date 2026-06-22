package descriptor

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Resolved bundles the message descriptors and full method name produced by
// resolving service + method against a Files registry. FullMethod is the
// canonical gRPC path `/pkg.Service/Method` shared by Connect and gRPC.
type Resolved struct {
	Service    protoreflect.ServiceDescriptor
	Method     protoreflect.MethodDescriptor
	Input      protoreflect.MessageDescriptor
	Output     protoreflect.MessageDescriptor
	FullMethod string
}

// Resolve looks up a fully-qualified service name (e.g. tales.test.v1.TestService)
// and a method name (e.g. Echo) in the registry. V1 rejects streaming methods
// because the rpc provider only supports unary calls. descriptorName is the
// user-facing name from config.rpc.descriptors used in error messages.
func Resolve(files *protoregistry.Files, descriptorName, service, method string) (*Resolved, error) {
	if files == nil {
		return nil, fmt.Errorf("descriptor registry is nil")
	}

	service = strings.TrimSpace(service)
	method = strings.TrimSpace(method)

	if service == "" {
		return nil, fmt.Errorf("rpc service name is empty")
	}

	if method == "" {
		return nil, fmt.Errorf("rpc method name is empty")
	}

	desc, err := files.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return nil, fmt.Errorf("rpc service %q not found in descriptor %q", service, descriptorName)
	}

	sd, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("rpc service %q in descriptor %q is not a service (got %T)", service, descriptorName, desc)
	}

	md := sd.Methods().ByName(protoreflect.Name(method))
	if md == nil {
		return nil, fmt.Errorf("rpc method %q not found in service %q", method, service)
	}

	if md.IsStreamingClient() || md.IsStreamingServer() {
		return nil, fmt.Errorf("rpc method %q is streaming; streaming RPC is not supported in V1", method)
	}

	return &Resolved{
		Service:    sd,
		Method:     md,
		Input:      md.Input(),
		Output:     md.Output(),
		FullMethod: "/" + service + "/" + method,
	}, nil
}
