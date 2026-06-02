package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"time"

	sasl "github.com/emersion/go-sasl"
	smtp "github.com/emersion/go-smtp"
)

// smtpSender sends a message over SMTP, optionally with implicit TLS, STARTTLS
// and AUTH PLAIN.
type smtpSender struct{}

func (s smtpSender) Send(ctx context.Context, target Target, envelope Envelope, raw []byte) (*Result, error) {
	addr := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))

	dialer := net.Dialer{Timeout: target.Timeout}

	conn, err := dialer.DialContext(ctx, networkTCP, addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	applyDeadline(conn, ctx, target.Timeout)

	client, err := newSMTPClient(ctx, conn, target)
	if err != nil {
		_ = conn.Close()

		return nil, err
	}

	defer client.Close()

	if target.Username != "" || target.Password != "" {
		if err := client.Auth(sasl.NewPlainClient("", target.Username, target.Password)); err != nil {
			return nil, fmt.Errorf("AUTH: %w", err)
		}
	}

	result, err := runTransaction(client, envelope, raw, false)
	if err != nil {
		return nil, err
	}

	_ = client.Quit()

	return result, nil
}

// newSMTPClient establishes the SMTP session up to (and including) the greeting
// and any TLS upgrade. STARTTLS uses go-smtp's exported helper; implicit TLS
// wraps the connection before the greeting; the plain path sets the fixed HELO
// name explicitly.
func newSMTPClient(ctx context.Context, conn net.Conn, target Target) (*smtp.Client, error) {
	switch {
	case target.TLS:
		tlsConn := tls.Client(conn, tlsConfigFor(target))
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("tls handshake: %w", err)
		}

		client := smtp.NewClient(tlsConn)
		if err := client.Hello(heloName); err != nil {
			return nil, fmt.Errorf("EHLO: %w", err)
		}

		return client, nil
	case target.StartTLS:
		client, err := smtp.NewClientStartTLS(conn, tlsConfigFor(target))
		if err != nil {
			return nil, fmt.Errorf("STARTTLS: %w", err)
		}

		return client, nil
	default:
		client := smtp.NewClient(conn)
		if err := client.Hello(heloName); err != nil {
			return nil, fmt.Errorf("EHLO: %w", err)
		}

		return client, nil
	}
}

// applyDeadline bounds the whole exchange with a single absolute deadline: the
// smaller of the target timeout window and the parent context's deadline.
func applyDeadline(conn net.Conn, ctx context.Context, timeout time.Duration) {
	deadline := time.Now().Add(timeout)

	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	_ = conn.SetDeadline(deadline)
}

// tlsConfigFor builds the client TLS configuration for a target. ServerName is
// pinned to the host so verification works; InsecureSkipVerify is opt-in via
// config and defaults to false.
func tlsConfigFor(target Target) *tls.Config {
	insecure := target.InsecureSkipVerify

	return &tls.Config{
		ServerName:         target.Host,
		InsecureSkipVerify: insecure, //nolint:gosec // G402: opt-in via target.insecure_skip_verify for self-signed test servers; defaults false.
		MinVersion:         tls.VersionTLS12,
	}
}
