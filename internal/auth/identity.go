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
	"fmt"
	"net/http"
	"strings"
	"unicode"
)

// Identity sources.
const (
	// IdentityIP identifies people by client network address (the default):
	// one VPN address is one person.
	IdentityIP = "ip"
	// IdentityHeader identifies people by a user header set by a single
	// sign-on proxy (oauth2-proxy, Pomerium, Authelia, Cloudflare Access…).
	IdentityHeader = "header"
)

// IdentityConfig chooses where a request's identity comes from.
type IdentityConfig struct {
	// Mode is IdentityIP or IdentityHeader.
	Mode string
	// UserHeader carries the signed-in user (e.g. X-Forwarded-Email).
	UserHeader string
	// GroupsHeader carries the user's groups (optional).
	GroupsHeader string
	// GroupsSeparator splits GroupsHeader: "," by default, or "none" for one
	// group per header line. A group name containing the separator cannot be
	// told apart from two groups, so with "," a user who can name a group
	// "x,admins" would be in "admins"; choose a separator your identity
	// provider forbids in group names, or grant administration by user.
	GroupsSeparator string
	// AdminUsers and AdminGroups grant administration to single sign-on users.
	AdminUsers  []string
	AdminGroups []string
}

// Identity is who made a request, as far as Turnbacks can tell.
type Identity struct {
	// Reporter is the pseudonymous reporter key ("ip:…" or "user:…").
	Reporter string
	// User is the single sign-on user, when known.
	User string
	// Groups are the single sign-on user's groups, when known.
	Groups []string
}

// Identifier resolves the identity of a request.
type Identifier interface {
	Identify(r *http.Request) Identity
}

// NewIdentifier returns the identifier for the configuration.
func NewIdentifier(cfg IdentityConfig) (Identifier, error) {
	switch cfg.Mode {
	case "", IdentityIP:
		return IPIdentifier{}, nil
	case IdentityHeader:
		if cfg.UserHeader == "" {
			return nil, fmt.Errorf("identity mode %q needs a user header", cfg.Mode)
		}
		sep := cfg.GroupsSeparator
		if sep == "" {
			sep = ","
		}
		return HeaderIdentifier{UserHeader: cfg.UserHeader, GroupsHeader: cfg.GroupsHeader, GroupsSeparator: sep}, nil
	default:
		return nil, fmt.Errorf("unknown identity mode %q (want ip or header)", cfg.Mode)
	}
}

// IPIdentifier identifies the reporter by client address.
type IPIdentifier struct{}

// Identify implements Identifier.
func (IPIdentifier) Identify(r *http.Request) Identity { return Identity{Reporter: ReporterKey(r)} }

// HeaderIdentifier identifies the reporter by a user header set by a trusted
// single sign-on proxy. The header is ignored unless the direct peer is a
// trusted proxy; requests without it (for example the tb CLI on a route that
// bypasses single sign-on) fall back to the client address.
type HeaderIdentifier struct {
	UserHeader      string
	GroupsHeader    string
	GroupsSeparator string // "," when empty; "none" for one group per header line
}

// maxUserLen keeps "user:<name>" within the reporter column (200 characters).
const maxUserLen = 190

// Identify implements Identifier.
func (h HeaderIdentifier) Identify(r *http.Request) Identity {
	if peer, ok := PeerFrom(r.Context()); ok && peer.TrustedProxy {
		if u := NormaliseUser(r.Header.Get(h.UserHeader)); u != "" {
			id := Identity{Reporter: "user:" + u, User: u}
			if h.GroupsHeader != "" {
				id.Groups = h.groups(r)
			}
			return id
		}
	}
	return IPIdentifier{}.Identify(r)
}

// NormaliseUser trims and lower-cases a user name from a proxy header. It
// returns "" for values that are empty, too long or contain control
// characters.
func NormaliseUser(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" || len(v) > maxUserLen || strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return ""
	}
	return v
}

// groups reads the groups header. With a separator, only the first header
// line is read, so a second, separately added line cannot add groups.
func (h HeaderIdentifier) groups(r *http.Request) []string {
	if h.GroupsSeparator == "none" {
		var out []string
		for _, g := range r.Header.Values(h.GroupsHeader) {
			if g = strings.TrimSpace(g); g != "" {
				out = append(out, g)
			}
		}
		return out
	}
	sep := h.GroupsSeparator
	if sep == "" {
		sep = ","
	}
	return splitList(r.Header.Get(h.GroupsHeader), sep)
}

func splitList(v, sep string) []string {
	var out []string
	for _, s := range strings.Split(v, sep) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
