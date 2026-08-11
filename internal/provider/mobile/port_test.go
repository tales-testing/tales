package mobile

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestFreeTCPPortReturnsBindablePort(t *testing.T) {
	t.Parallel()

	port, err := freeTCPPort(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}

	if port <= 0 {
		t.Fatalf("expected a positive port, got %d", port)
	}

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("expected allocated port %d to be bindable: %v", port, err)
	}

	_ = ln.Close()
}

func TestFreeTCPPortDefaultsHost(t *testing.T) {
	t.Parallel()

	port, err := freeTCPPort(context.Background(), "")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}

	if port <= 0 {
		t.Fatalf("expected a positive port, got %d", port)
	}
}

func TestFreeTCPPortHandsOutDistinctPorts(t *testing.T) {
	t.Parallel()

	seen := map[int]struct{}{}

	for i := range 20 {
		port, err := freeTCPPort(context.Background(), "127.0.0.1")
		if err != nil {
			t.Fatalf("free port %d: %v", i, err)
		}

		if _, dup := seen[port]; dup {
			t.Fatalf("freeTCPPort returned duplicate port %d", port)
		}

		seen[port] = struct{}{}
	}
}

func TestResolveDriverEndpointAllocatesWhenEmbeddedAndPortUnset(t *testing.T) {
	t.Parallel()

	target := Target{
		Name:   "iphone",
		Driver: DriverConfig{Host: "127.0.0.1"}, // embedded, no explicit port
	}

	resolved, err := ResolveDriverEndpoint(context.Background(), target)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if resolved.Driver.Port == 0 {
		t.Fatalf("expected an allocated port, got %d", resolved.Driver.Port)
	}

	// The client base URL, the driver env, and the health URL all derive from
	// BaseURL(), so it must carry the allocated port.
	want := ":" + strconv.Itoa(resolved.Driver.Port)
	if !strings.HasSuffix(resolved.Driver.BaseURL(), want) {
		t.Fatalf("BaseURL %q must end with allocated port %s", resolved.Driver.BaseURL(), want)
	}
}

func TestResolveDriverEndpointKeepsExplicitPort(t *testing.T) {
	t.Parallel()

	target := Target{
		Name:   "iphone",
		Driver: DriverConfig{Host: "127.0.0.1", Port: 9080, PortSet: true},
	}

	resolved, err := ResolveDriverEndpoint(context.Background(), target)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if resolved.Driver.Port != 9080 {
		t.Fatalf("expected explicit port preserved, got %d", resolved.Driver.Port)
	}
}

func TestResolveDriverEndpointSkipsExternalMode(t *testing.T) {
	t.Parallel()

	// External mode connects to a driver Tales does not own; the port must be
	// left untouched even though it was not explicitly set.
	target := Target{
		Name:   "iphone",
		Driver: DriverConfig{Host: "127.0.0.1", Port: 9080, External: true},
	}

	resolved, err := ResolveDriverEndpoint(context.Background(), target)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if resolved.Driver.Port != 9080 {
		t.Fatalf("expected external port untouched, got %d", resolved.Driver.Port)
	}
}
