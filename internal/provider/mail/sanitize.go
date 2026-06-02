package mail

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// sanitizeSendError wraps a transport error with target context (name,
// protocol, endpoint) while scrubbing the SMTP password if it leaked into the
// message. The message body is never part of these errors.
func sanitizeSendError(target Target, err error) error {
	msg := err.Error()

	if target.Password != "" {
		msg = strings.ReplaceAll(msg, target.Password, "***")
	}

	return fmt.Errorf("mail send failed (target %q, protocol %s, endpoint %s): %s",
		target.Name, target.Protocol, targetEndpoint(target), scrub(msg))
}

// targetEndpoint returns the human-facing endpoint for diagnostics. It never
// includes credentials.
func targetEndpoint(target Target) string {
	if target.Protocol == protocolLMTP {
		return target.Network + "!" + target.Address
	}

	return net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
}

// scrub removes obvious credential fragments (AUTH lines) from a protocol error
// string as a defensive measure.
func scrub(msg string) string {
	lines := strings.Split(msg, "\n")
	for i, line := range lines {
		if strings.Contains(strings.ToUpper(line), "AUTH ") {
			lines[i] = "AUTH ***"
		}
	}

	return strings.Join(lines, "\n")
}

// scrubMessage normalizes a server reply text into a single clean line for the
// assertable response. The reply is server-controlled and carries no secret; it
// is collapsed (CR/LF/tabs to spaces) and trimmed so it stays a tidy one-liner.
func scrubMessage(msg string) string {
	replacer := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ")

	return strings.TrimSpace(replacer.Replace(msg))
}
