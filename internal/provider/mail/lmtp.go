package mail

import (
	"context"
	"fmt"
	"net"

	smtp "github.com/emersion/go-smtp"
)

// lmtpSender sends a message over LMTP (RFC 2033) on a tcp or unix socket. LMTP
// has no TLS or auth in V1; the per-recipient final responses are read by
// finishLMTP.
type lmtpSender struct{}

func (s lmtpSender) Send(ctx context.Context, target Target, envelope Envelope, raw []byte) (*Result, error) {
	dialer := net.Dialer{Timeout: target.Timeout}

	conn, err := dialer.DialContext(ctx, target.Network, target.Address)
	if err != nil {
		return nil, fmt.Errorf("dial %s %s: %w", target.Network, target.Address, err)
	}

	applyDeadline(conn, ctx, target.Timeout)

	client := smtp.NewClientLMTP(conn)

	defer client.Close()

	if err := client.Hello(heloName); err != nil {
		return nil, fmt.Errorf("LHLO: %w", err)
	}

	result, err := runTransaction(client, envelope, raw, true)
	if err != nil {
		return nil, err
	}

	_ = client.Quit()

	return result, nil
}
