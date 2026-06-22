package rpc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildTLSConfig_Nil(t *testing.T) {
	t.Parallel()

	cfg, err := BuildTLSConfig(nil)
	if err != nil {
		t.Fatalf("BuildTLSConfig(nil): %v", err)
	}

	if cfg == nil {
		t.Fatal("expected default tls.Config, got nil")
	}
}

func TestBuildTLSConfig_BasicSkipVerify(t *testing.T) {
	t.Parallel()

	cfg, err := BuildTLSConfig(&TLSConfig{ServerName: "svc.example", InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}

	if cfg.ServerName != "svc.example" {
		t.Errorf("ServerName = %q", cfg.ServerName)
	}

	if !cfg.InsecureSkipVerify {
		t.Errorf("InsecureSkipVerify should be true")
	}

	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d", cfg.MinVersion, tls.VersionTLS12)
	}
}

func TestBuildTLSConfig_LoadCAFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")

	if err := os.WriteFile(caPath, generateCertificatePEM(t), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	cfg, err := BuildTLSConfig(&TLSConfig{CAFile: caPath})
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}

	if cfg.RootCAs == nil {
		t.Errorf("RootCAs should be populated")
	}
}

func TestBuildTLSConfig_LoadCAMissingFile(t *testing.T) {
	t.Parallel()

	_, err := BuildTLSConfig(&TLSConfig{CAFile: filepath.Join(t.TempDir(), "absent.pem")})
	if err == nil || !strings.Contains(err.Error(), "read tls ca_file") {
		t.Errorf("expected ca-read error, got %v", err)
	}
}

func TestBuildTLSConfig_LoadCAInvalidContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")

	if err := os.WriteFile(caPath, []byte("not a pem"), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	_, err := BuildTLSConfig(&TLSConfig{CAFile: caPath})
	if err == nil || !strings.Contains(err.Error(), "no PEM certificates") {
		t.Errorf("expected no-pem error, got %v", err)
	}
}

func TestBuildTLSConfig_LoadClientCert(t *testing.T) {
	t.Parallel()

	certPath, keyPath := generateClientCertPair(t)

	cfg, err := BuildTLSConfig(&TLSConfig{CertFile: certPath, KeyFile: keyPath})
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}

	if len(cfg.Certificates) != 1 {
		t.Errorf("Certificates len = %d, want 1", len(cfg.Certificates))
	}
}

// generateCertificatePEM emits a self-signed CA certificate as PEM bytes. Used
// for tests that only need a parseable PEM in the trust store; the cert is
// never used to verify a real connection.
func generateCertificatePEM(t *testing.T) []byte {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa keygen: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "tales-test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageCertSign,
		IsCA:         true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// generateClientCertPair emits a self-signed leaf certificate + the matching
// private key as PEM files, returning their paths. Used for the mTLS load
// test only; the cert is not chained to any CA.
func generateClientCertPair(t *testing.T) (string, string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa keygen: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "tales-test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	keyDer, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	return certPath, keyPath
}
