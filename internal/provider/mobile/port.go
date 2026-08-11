package mobile

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
)

// ResolveDriverEndpoint returns the target with a concrete driver port.
//
// In embedded mode with driver.port omitted, it allocates a free host port so
// concurrent simulators do not collide: the iOS simulator binds the driver's
// HTTP server on the host's shared loopback, so two drivers running at once
// must use different ports. When the user set driver.port explicitly, or in
// external mode (where Tales connects to an already-running driver it does not
// own), the configured port is returned unchanged.
func ResolveDriverEndpoint(ctx context.Context, target Target) (Target, error) {
	if target.Driver.External || target.Driver.PortSet {
		return target, nil
	}

	port, err := freeTCPPort(ctx, target.Driver.Host)
	if err != nil {
		return Target{}, fmt.Errorf("allocate driver port for target %q: %w", target.Name, err)
	}

	target.Driver.Port = port

	fmt.Fprintf(os.Stderr, "Auto-selected driver port %d for target %q\n", port, target.Name)

	return target, nil
}

// claimedPorts records every port this process has already handed out so two
// concurrently-built targets never receive the same one. Binding port 0 and
// closing the listener only guarantees the port is free at that instant, not
// that a sibling allocation will not draw the same number; the claimed set
// makes our own allocations collision-free (a foreign process grabbing the
// port in the close→bind window remains a small, accepted TOCTOU risk).
var (
	claimedPortsMu sync.Mutex
	claimedPorts   = map[int]struct{}{}
)

const freePortMaxAttempts = 50

// freeTCPPort asks the OS for an unused TCP port on host (binding to port 0 and
// reading back the assignment) and returns one not already claimed in this
// process. The listener is closed before returning.
func freeTCPPort(ctx context.Context, host string) (int, error) {
	if host == "" {
		host = DefaultDriverHost
	}

	claimedPortsMu.Lock()
	defer claimedPortsMu.Unlock()

	for range freePortMaxAttempts {
		port, err := listenForFreePort(ctx, host)
		if err != nil {
			return 0, err
		}

		if _, taken := claimedPorts[port]; taken {
			continue
		}

		claimedPorts[port] = struct{}{}

		return port, nil
	}

	return 0, fmt.Errorf("could not find an unclaimed free port on %q after %d attempts", host, freePortMaxAttempts)
}

func listenForFreePort(ctx context.Context, host string) (int, error) {
	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, fmt.Errorf("listen for free port on %q: %w", host, err)
	}

	defer func() { _ = ln.Close() }()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", ln.Addr())
	}

	return addr.Port, nil
}
