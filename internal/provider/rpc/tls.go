package rpc

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// BuildTLSConfig translates the resolved TLSConfig into a *tls.Config the
// transports / reflection loader plug into their dial options.
//
// Behavior:
//   - nil cfg returns nil so the caller can use the default tls.Config when
//     plaintext is false and the user did not customize TLS.
//   - CAFile, when set, replaces the system root pool with the certificates
//     in that file.
//   - CertFile / KeyFile, when both set, enable mTLS client certificates.
//   - ServerName overrides the SNI / verification name (useful when the
//     reachable hostname differs from the certificate CN / SAN).
//   - InsecureSkipVerify disables the entire chain check — V1 honors it
//     verbatim so users can test against self-signed servers, but it must
//     never be the default and the docs flag it explicitly.
//
// The function never embeds file contents in error messages so private keys
// cannot leak through diagnostics.
func BuildTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	if cfg == nil {
		//nolint:gosec // V1: nil TLS config means "use system defaults"; TLS 1.2+ negotiated by Go's crypto/tls.
		return &tls.Config{}, nil
	}

	out := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // user opt-in for self-signed test servers; documented limitation.
		ServerName:         cfg.ServerName,
	}

	if cfg.CAFile != "" {
		pool, err := loadCAPool(cfg.CAFile)
		if err != nil {
			return nil, err
		}

		out.RootCAs = pool
	}

	if cfg.CertFile != "" || cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load tls client cert from %q / %q: %w", cfg.CertFile, cfg.KeyFile, err)
		}

		out.Certificates = []tls.Certificate{cert}
	}

	return out, nil
}

func loadCAPool(path string) (*x509.CertPool, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tls ca_file %q: %w", path, err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bytes) {
		return nil, fmt.Errorf("tls ca_file %q contains no PEM certificates", path)
	}

	return pool, nil
}
