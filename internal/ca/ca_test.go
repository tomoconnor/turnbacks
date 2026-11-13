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

package ca

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

func TestInitIssue(t *testing.T) {
	dir := t.TempDir()
	root, err := Init(dir, "", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir, "", time.Hour); err == nil {
		t.Fatal("Init should refuse to overwrite an existing CA")
	}
	if fi, _ := os.Stat(filepath.Join(dir, KeyFile)); fi.Mode().Perm() != 0o600 {
		t.Errorf("ca.key mode = %v", fi.Mode().Perm())
	}

	iss, err := Issue(dir, "jane", 365*24*time.Hour, filepath.Join(dir, "issued"))
	if err != nil {
		t.Fatal(err)
	}
	if !iss.NotAfter.Equal(root.NotAfter) {
		t.Errorf("leaf validity should be capped at the CA's: %v vs %v", iss.NotAfter, root.NotAfter)
	}
	if _, err := Issue(dir, "jane", time.Hour, filepath.Join(dir, "issued")); err == nil {
		t.Error("Issue should refuse to overwrite an existing certificate")
	}
	if _, err := Issue(dir, "../evil", time.Hour, dir); err == nil {
		t.Error("Issue should reject unsafe names")
	}

	p12, _ := os.ReadFile(iss.P12Path)
	key, cert, err := pkcs12.Decode(p12, iss.P12Password)
	if err != nil || key == nil {
		t.Fatalf("p12 does not decode: %v", err)
	}
	if cert.Subject.CommonName != "jane" {
		t.Errorf("CN = %q", cert.Subject.CommonName)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Errorf("client cert does not verify for client auth: %v", err)
	}
	// The root is restricted to client authentication: it cannot vouch for websites.
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err == nil {
		t.Error("client cert must not verify for server auth")
	}
}
