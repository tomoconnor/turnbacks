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

package auth

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"turnbacks/internal/ca"
)

type testPKI struct {
	caFile string
	certs  map[string]*x509.Certificate
}

func newPKI(t *testing.T, names ...string) testPKI {
	t.Helper()
	dir := t.TempDir()
	if _, err := ca.Init(dir, "", 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	p := testPKI{caFile: filepath.Join(dir, ca.CertFile), certs: map[string]*x509.Certificate{}}
	for _, n := range names {
		iss, err := ca.Issue(dir, n, time.Hour, filepath.Join(dir, "issued"))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(iss.CertPath)
		blk, _ := pem.Decode(b)
		p.certs[n], _ = x509.ParseCertificate(blk.Bytes)
	}
	return p
}

func b64(c *x509.Certificate) string { return base64.StdEncoding.EncodeToString(c.Raw) }

func newAdmin(t *testing.T, cfg AdminConfig) *Admin {
	t.Helper()
	a, err := NewAdmin(cfg, IdentityConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func request(peer string, trusted bool, hdr map[string]string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = hostPort(peer)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r.WithContext(WithPeer(r.Context(), Peer{Addr: netip.MustParseAddr(peer), TrustedProxy: trusted}))
}

func adminName(a *Admin, r *http.Request) string { return a.Actor(r).AdminName }

func hostPort(ip string) string { return netip.AddrPortFrom(netip.MustParseAddr(ip), 1234).String() }

func TestAdminClientCertificate(t *testing.T) {
	good := newPKI(t, "jane", "raj")
	other := newPKI(t, "jane")
	a := newAdmin(t, AdminConfig{ClientCAFile: good.caFile, CertHeader: "X-Client-Cert"})
	if !a.Enabled() {
		t.Fatal("should be enabled")
	}
	jane := good.certs["jane"]
	pemJane := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: jane.Raw}))

	cases := []struct {
		name    string
		trusted bool
		header  string
		want    string
	}{
		{"base64 DER from trusted proxy", true, b64(jane), "jane"},
		{"URL-encoded PEM (ingress-nginx)", true, url.PathEscape(pemJane), "jane"},
		{"raw PEM", true, pemJane, "jane"},
		{"header from an untrusted peer is ignored", false, b64(jane), ""},
		{"certificate from another CA", true, b64(other.certs["jane"]), ""},
		{"garbage", true, "not-a-cert", ""},
		{"no header", true, "", ""},
		{"Caddy placeholder when no certificate", true, "{http.request.tls.client.certificate_der_base64}", ""},
	}
	for _, c := range cases {
		hdr := map[string]string{}
		if c.header != "" {
			hdr["X-Client-Cert"] = c.header
		}
		if got := adminName(a, request("10.0.0.5", c.trusted, hdr)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	// Expired certificates are rejected.
	a.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if got := adminName(a, request("10.0.0.5", true, map[string]string{"X-Client-Cert": b64(jane)})); got != "" {
		t.Errorf("expired certificate accepted as %q", got)
	}
}

func TestAdminAllowedNames(t *testing.T) {
	p := newPKI(t, "jane", "raj")
	a := newAdmin(t, AdminConfig{ClientCAFile: p.caFile, AllowedNames: []string{"jane"}})
	if got := adminName(a, request("10.0.0.5", true, map[string]string{"X-Client-Cert": b64(p.certs["jane"])})); got != "jane" {
		t.Errorf("jane: %q", got)
	}
	if got := adminName(a, request("10.0.0.5", true, map[string]string{"X-Client-Cert": b64(p.certs["raj"])})); got != "" {
		t.Errorf("raj is not allowed but got %q", got)
	}
}

func TestDevLoopbackAdmin(t *testing.T) {
	a := newAdmin(t, AdminConfig{DevLoopback: true})
	if got := adminName(a, request("127.0.0.1", true, nil)); got != "dev" {
		t.Errorf("loopback: %q", got)
	}
	// Via a local proxy (e.g. an ngrok agent) for a remote client: not admin.
	r := request("127.0.0.1", true, nil)
	r.RemoteAddr = hostPort("203.0.113.9")
	if got := adminName(a, r); got != "" {
		t.Errorf("remote client through local proxy: %q", got)
	}
	if got := adminName(newAdmin(t, AdminConfig{}), request("127.0.0.1", true, nil)); got != "" {
		t.Errorf("dev admin off: %q", got)
	}
}

func TestHeaderIdentity(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := newPKI(t, "jane")
	a, err := NewAdmin(AdminConfig{ClientCAFile: p.caFile, CertHeader: "X-Client-Cert"}, IdentityConfig{
		Mode: IdentityHeader, UserHeader: "X-Forwarded-Email", GroupsHeader: "X-Forwarded-Groups",
		AdminUsers: []string{"Boss@Example.com"}, AdminGroups: []string{"turnbacks-admins"},
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	if a.IdentityMode() != IdentityHeader || !a.SSOEnabled() || !a.CertificatesEnabled() {
		t.Fatal("expected header identity with SSO and certificate administration")
	}
	cases := []struct {
		name                string
		trusted             bool
		hdr                 map[string]string
		reporter, user      string
		adminName, adminVia string
	}{
		{"user from trusted proxy", true, map[string]string{"X-Forwarded-Email": " Ann@Example.com "}, "user:ann@example.com", "ann@example.com", "", ""},
		{"header from untrusted peer falls back to address", false, map[string]string{"X-Forwarded-Email": "ann@example.com"}, "ip:10.0.0.5", "", "", ""},
		{"no header falls back to address", true, nil, "ip:10.0.0.5", "", "", ""},
		{"control characters rejected", true, map[string]string{"X-Forwarded-Email": "a\x00b"}, "ip:10.0.0.5", "", "", ""},
		{"admin user", true, map[string]string{"X-Forwarded-Email": "boss@example.com"}, "user:boss@example.com", "boss@example.com", "boss@example.com", ViaSSO},
		{"admin group", true, map[string]string{"X-Forwarded-Email": "raj@example.com", "X-Forwarded-Groups": "staff, turnbacks-admins"}, "user:raj@example.com", "raj@example.com", "raj@example.com", ViaSSO},
		{"other groups", true, map[string]string{"X-Forwarded-Email": "raj@example.com", "X-Forwarded-Groups": "staff"}, "user:raj@example.com", "raj@example.com", "", ""},
		{"admin group from untrusted peer", false, map[string]string{"X-Forwarded-Email": "raj@example.com", "X-Forwarded-Groups": "turnbacks-admins"}, "ip:10.0.0.5", "", "", ""},
		{"certificate still works", true, map[string]string{"X-Forwarded-Email": "jane@example.com", "X-Client-Cert": b64(p.certs["jane"])}, "user:jane@example.com", "jane@example.com", "jane", ViaCertificate},
	}
	for _, c := range cases {
		got := a.Actor(request("10.0.0.5", c.trusted, c.hdr))
		if got.Reporter != c.reporter || got.User != c.user || got.AdminName != c.adminName || got.AdminVia != c.adminVia || got.Admin != (c.adminName != "") {
			t.Errorf("%s: got %+v", c.name, got)
		}
	}

	// In address mode the headers mean nothing, even from a trusted proxy.
	ip := newAdmin(t, AdminConfig{})
	got := ip.Actor(request("10.0.0.5", true, map[string]string{"X-Forwarded-Email": "boss@example.com"}))
	if got.Reporter != "ip:10.0.0.5" || got.User != "" || got.Admin {
		t.Errorf("ip mode: %+v", got)
	}
	if _, err := NewIdentifier(IdentityConfig{Mode: "carrier-pigeon"}); err == nil {
		t.Error("unknown mode accepted")
	}
}
