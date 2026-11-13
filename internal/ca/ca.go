// Copyright (c) 2026 Tom O'Connor <tom@twinhelix.org>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// Package ca is a deliberately tiny certificate authority for Turnbacks admin
// client certificates.
//
// It creates one self-signed ECDSA P-256 root that can only sign client
// certificates (its extended key usage is restricted to client
// authentication, so it cannot vouch for websites even if someone trusts it
// by mistake), and issues one certificate per administrator. Each certificate
// is written as PEM files and as a password-protected PKCS#12 bundle that
// browsers and operating-system keychains can import.
//
// The CA private key belongs on an administrator's workstation, not on the
// server: the server and the TLS proxy only need ca.crt.
package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// File names inside the CA directory.
const (
	CertFile = "ca.crt"
	KeyFile  = "ca.key"
	// Organization is set on every certificate the CA creates.
	Organization = "Turnbacks admin"
)

// NameRE restricts administrator names (the certificate common name) to
// something safe for file names and logs.
var NameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$`)

// Issued describes a newly issued client certificate.
type Issued struct {
	Name        string
	Serial      string // hex
	Fingerprint string // SHA-256 of the DER certificate, hex
	NotAfter    time.Time
	CertPath    string
	KeyPath     string
	P12Path     string
	P12Password string
}

// Init creates a new CA in dir. It refuses to overwrite an existing CA.
func Init(dir, commonName string, validity time.Duration) (*x509.Certificate, error) {
	if commonName == "" {
		commonName = "Turnbacks admin CA"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, KeyFile)); err == nil {
		return nil, fmt.Errorf("%s already exists; refusing to overwrite an existing CA", filepath.Join(dir, KeyFile))
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{Organization}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(validity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		// Restricting the root's EKU means anything it signs is only valid
		// for client authentication.
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := writeNew(filepath.Join(dir, KeyFile), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return nil, err
	}
	if err := writeNew(filepath.Join(dir, CertFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return nil, err
	}
	return x509.ParseCertificate(der)
}

// Load reads the CA certificate and key from dir.
func Load(dir string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(filepath.Join(dir, CertFile)) //nolint:gosec // operator-supplied CA directory
	if err != nil {
		return nil, nil, fmt.Errorf("read CA certificate: %w (run `turnbacks ca init` first)", err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, KeyFile)) //nolint:gosec // operator-supplied CA directory
	if err != nil {
		return nil, nil, fmt.Errorf("read CA key: %w", err)
	}
	cb, _ := pem.Decode(certPEM)
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		return nil, nil, errors.New("CA files are not PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("CA key is not ECDSA")
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		return nil, nil, errors.New("CA key does not match CA certificate")
	}
	return cert, key, nil
}

// Issue creates a client certificate for name, signed by the CA in dir, and
// writes <name>.crt, <name>.key and <name>.p12 to outDir.
func Issue(dir, name string, validity time.Duration, outDir string) (*Issued, error) {
	if !NameRE.MatchString(name) {
		return nil, fmt.Errorf("invalid name %q: use letters, digits, '.', '_', '@' or '-' (max 64)", name)
	}
	caCert, caKey, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return nil, err
	}
	base := filepath.Join(outDir, name)
	for _, ext := range []string{".crt", ".key", ".p12"} {
		if _, err := os.Stat(base + ext); err == nil {
			return nil, fmt.Errorf("%s already exists; choose another name (e.g. %s-2) or move the old files away", base+ext, name)
		}
	}
	now := time.Now()
	notAfter := now.Add(validity)
	if notAfter.After(caCert.NotAfter) {
		notAfter = caCert.NotAfter
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name, Organization: []string{Organization}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              notAfter,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	password, err := randomPassword()
	if err != nil {
		return nil, err
	}
	// 3DES/SHA-1 PKCS#12 is the format every browser and keychain (notably
	// macOS) imports reliably; the password is random and only protects the
	// file in transit to the administrator.
	p12, err := pkcs12.LegacyDES.Encode(key, cert, nil, password)
	if err != nil {
		return nil, err
	}
	if err := writeNew(base+".key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return nil, err
	}
	if err := writeNew(base+".crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return nil, err
	}
	if err := writeNew(base+".p12", p12, 0o600); err != nil {
		return nil, err
	}
	fp := sha256.Sum256(der)
	iss := &Issued{
		Name: name, Serial: hex.EncodeToString(serial.Bytes()), Fingerprint: hex.EncodeToString(fp[:]), NotAfter: notAfter,
		CertPath: base + ".crt", KeyPath: base + ".key", P12Path: base + ".p12", P12Password: password,
	}
	line := fmt.Sprintf("%s\t%s\tserial=%s\tsha256=%s\tnot_after=%s\n", now.UTC().Format(time.RFC3339), name, iss.Serial, iss.Fingerprint, notAfter.UTC().Format(time.RFC3339))
	if f, err := os.OpenFile(filepath.Join(dir, "issued.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil { //nolint:gosec // operator-supplied CA directory
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
	return iss, nil
}

func randomSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

func randomPassword() (string, error) {
	b := make([]byte, 15)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)) // 24 chars
	return s[0:6] + "-" + s[6:12] + "-" + s[12:18] + "-" + s[18:24], nil
}

func writeNew(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode) //nolint:gosec // operator-supplied path
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
