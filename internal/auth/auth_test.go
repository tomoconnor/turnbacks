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
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"testing"
)

func TestReporterKey(t *testing.T) {
	cases := map[string]string{
		"10.8.0.12:51234":        "ip:10.8.0.12",
		"[::ffff:10.8.0.12]:443": "ip:10.8.0.12",
		"[2001:db8::7]:80":       "ip:2001:db8::7",
		"garbage":                "",
	}
	for addr, want := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = addr
		if got := ReporterKey(r); got != want {
			t.Errorf("ReporterKey(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestHeaderIdentifierGroups(t *testing.T) {
	req := func(groups ...string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-Forwarded-Email", "ann@example.com")
		for _, g := range groups {
			r.Header.Add("X-Forwarded-Groups", g)
		}
		return r.WithContext(WithPeer(r.Context(), Peer{Addr: netip.MustParseAddr("10.0.0.1"), TrustedProxy: true}))
	}
	comma := HeaderIdentifier{UserHeader: "X-Forwarded-Email", GroupsHeader: "X-Forwarded-Groups"}
	if got := comma.Identify(req("staff, ops", "admins")).Groups; !slices.Equal(got, []string{"staff", "ops"}) {
		t.Errorf("comma: %q (a second header line must not add groups)", got)
	}
	pipe := HeaderIdentifier{UserHeader: "X-Forwarded-Email", GroupsHeader: "X-Forwarded-Groups", GroupsSeparator: "|"}
	if got := pipe.Identify(req("x,admins|staff")).Groups; !slices.Equal(got, []string{"x,admins", "staff"}) {
		t.Errorf("pipe: %q", got)
	}
	lines := HeaderIdentifier{UserHeader: "X-Forwarded-Email", GroupsHeader: "X-Forwarded-Groups", GroupsSeparator: "none"}
	if got := lines.Identify(req("x,admins", "staff")).Groups; !slices.Equal(got, []string{"x,admins", "staff"}) {
		t.Errorf("none: %q", got)
	}
}
