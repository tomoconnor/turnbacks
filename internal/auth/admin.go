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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"turnbacks/internal/domain"
)

// AdminConfig configures client-certificate administration.
type AdminConfig struct {
	// ClientCAFile is a PEM bundle of the CA(s) that issue admin client
	// certificates. Empty disables certificate administration.
	ClientCAFile string
	// CertHeader is the request header in which the TLS-terminating proxy
	// forwards the verified client certificate: base64 DER (Caddy's
	// {http.request.tls.client.certificate_der_base64}) or URL-encoded PEM
	// (ingress-nginx's ssl-client-cert).
	CertHeader string
	// AllowedNames optionally restricts administration to these certificate
	// common names; empty allows any certificate from the CA. Removing a name
	// revokes that administrator.
	AllowedNames []string
	// DevLoopback makes requests from this machine (direct peer and client
	// both loopback) administrators. Development only.
	DevLoopback bool
}

// Admin decides whether a request comes from an administrator.
//
// TLS is terminated by a reverse proxy (Caddy in the Docker stack, the
// ingress controller on Kubernetes) which asks browsers for a client
// certificate, checks the TLS proof of possession and forwards the
// certificate in a header. Turnbacks believes that header only when the
// direct peer is a trusted proxy, and verifies the certificate chain, validity
// and client-auth usage itself against the admin CA, so a misconfigured proxy
// cannot grant administration.
//
// With single sign-on (IdentityHeader), users listed in AdminUsers or members
// of AdminGroups are administrators too; the proxy that sets those headers is
// trusted in the same way.
type Admin struct {
	roots       *x509.CertPool
	header      string
	allowed     map[string]bool
	dev         bool
	identity    Identifier
	mode        string
	adminUsers  map[string]bool
	adminGroups map[string]bool
	log         *slog.Logger
	now         func() time.Time
}

// maxCertHeader bounds the forwarded certificate size.
const maxCertHeader = 16 << 10

// NewAdmin constructs the admin checker and identity resolver.
func NewAdmin(cfg AdminConfig, idc IdentityConfig, log *slog.Logger) (*Admin, error) {
	id, err := NewIdentifier(idc)
	if err != nil {
		return nil, err
	}
	a := &Admin{header: cfg.CertHeader, dev: cfg.DevLoopback, identity: id, mode: idc.Mode, log: log, now: time.Now}
	if a.mode == "" {
		a.mode = IdentityIP
	}
	if idc.Mode == IdentityHeader {
		for _, u := range idc.AdminUsers {
			if u = NormaliseUser(u); u != "" {
				if a.adminUsers == nil {
					a.adminUsers = map[string]bool{}
				}
				a.adminUsers[u] = true
			}
		}
		for _, g := range idc.AdminGroups {
			if a.adminGroups == nil {
				a.adminGroups = map[string]bool{}
			}
			a.adminGroups[g] = true
		}
	}
	if a.header == "" {
		a.header = "X-Client-Cert"
	}
	if cfg.ClientCAFile != "" {
		b, err := os.ReadFile(cfg.ClientCAFile) //nolint:gosec // operator-supplied CA bundle path
		if err != nil {
			return nil, fmt.Errorf("admin client CA: %w", err)
		}
		a.roots = x509.NewCertPool()
		if !a.roots.AppendCertsFromPEM(b) {
			return nil, fmt.Errorf("admin client CA %s: no PEM certificates found", cfg.ClientCAFile)
		}
	}
	if len(cfg.AllowedNames) > 0 {
		a.allowed = map[string]bool{}
		for _, n := range cfg.AllowedNames {
			a.allowed[n] = true
		}
	}
	switch {
	case a.dev:
		log.Warn("development admin: every request from this machine is an administrator")
	case a.roots == nil && !a.ssoAdmin():
		log.Warn("neither TURNBACKS_ADMIN_CLIENT_CA nor single sign-on administrators are configured: administration (settings, merging, corrections, audit) is disabled")
	}
	return a, nil
}

// Enabled reports whether administration is possible at all.
func (a *Admin) Enabled() bool { return a.roots != nil || a.dev || a.ssoAdmin() }

// IdentityMode returns IdentityIP or IdentityHeader.
func (a *Admin) IdentityMode() string { return a.mode }

// CertificatesEnabled reports whether admin client certificates are accepted.
func (a *Admin) CertificatesEnabled() bool { return a.roots != nil }

// SSOEnabled reports whether single sign-on users can be administrators.
func (a *Admin) SSOEnabled() bool { return a.ssoAdmin() }

func (a *Admin) ssoAdmin() bool { return a.adminUsers != nil || a.adminGroups != nil }

// Admin sources, recorded in audit logs.
const (
	ViaCertificate = "certificate"
	ViaSSO         = "sso"
	ViaDev         = "dev"
)

// identify returns the administrator's name and how administration was
// granted, or "" for everyone else.
func (a *Admin) identify(r *http.Request, id Identity) (name, via string) {
	peer, ok := PeerFrom(r.Context())
	if a.dev && ok && peer.Addr.IsLoopback() {
		if ip, ok := ClientIP(r); ok && ip.IsLoopback() {
			return "dev", ViaDev
		}
	}
	if !ok || !peer.TrustedProxy {
		return "", ""
	}
	if n := a.certificateAdmin(r); n != "" {
		return n, ViaCertificate
	}
	if id.User != "" && a.ssoAdmin() {
		if a.adminUsers[id.User] {
			return id.User, ViaSSO
		}
		for _, g := range id.Groups {
			if a.adminGroups[g] {
				return id.User, ViaSSO
			}
		}
	}
	return "", ""
}

// certificateAdmin returns the common name of a valid forwarded admin
// certificate, or "". The caller has checked that the peer is a trusted proxy.
func (a *Admin) certificateAdmin(r *http.Request) string {
	if a.roots == nil {
		return ""
	}
	raw := strings.TrimSpace(r.Header.Get(a.header))
	// No certificate: proxies send nothing, an empty value, or (Caddy) the
	// unreplaced placeholder "{http.request.tls.client.…}".
	if raw == "" || strings.HasPrefix(raw, "{") || raw == "(null)" {
		return ""
	}
	cert, err := ParseForwardedCert(raw)
	if err != nil {
		a.log.WarnContext(r.Context(), "admin client certificate unreadable", "error", err)
		return ""
	}
	name, err := a.verify(cert)
	if err != nil {
		a.log.WarnContext(r.Context(), "admin client certificate rejected", "subject", cert.Subject.String(), "error", err)
		return ""
	}
	return name
}

func (a *Admin) verify(cert *x509.Certificate) (string, error) {
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots: a.roots, CurrentTime: a.now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return "", err
	}
	name := cert.Subject.CommonName
	if name == "" {
		return "", errors.New("certificate has no common name")
	}
	if a.allowed != nil && !a.allowed[name] {
		return "", fmt.Errorf("%q is not in TURNBACKS_ADMIN_NAMES", name)
	}
	return name, nil
}

// Actor resolves the actor of a request.
func (a *Admin) Actor(r *http.Request) domain.Actor {
	id := a.identity.Identify(r)
	name, via := a.identify(r, id)
	return domain.Actor{Reporter: id.Reporter, User: id.User, Admin: name != "", AdminName: name, AdminVia: via}
}

// Middleware attaches the actor to every request and records a log line for
// every state-changing request made by an administrator.
func (a *Admin) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := a.Actor(r)
		if actor.Admin && r.Method != http.MethodGet && r.Method != http.MethodHead {
			a.log.InfoContext(r.Context(), "admin request", "admin", actor.AdminName, "via", actor.AdminVia, "method", r.Method, "path", r.URL.Path)
		}
		next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
	})
}

// ParseForwardedCert decodes a client certificate forwarded by a proxy, as
// base64 DER or (URL-encoded) PEM.
func ParseForwardedCert(raw string) (*x509.Certificate, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxCertHeader {
		return nil, errors.New("certificate header too large")
	}
	if strings.Contains(raw, "%") {
		u, err := url.PathUnescape(raw) // PathUnescape keeps '+' (common in base64)
		if err != nil {
			return nil, fmt.Errorf("url-decode: %w", err)
		}
		raw = u
	}
	var der []byte
	if strings.HasPrefix(raw, "-----BEGIN") {
		b, _ := pem.Decode([]byte(raw))
		if b == nil || b.Type != "CERTIFICATE" {
			return nil, errors.New("not a PEM certificate")
		}
		der = b.Bytes
	} else {
		d, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("base64: %w", err)
		}
		der = d
	}
	return x509.ParseCertificate(der)
}
