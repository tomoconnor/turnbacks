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

// Package auth handles request identity for an open-access deployment.
//
// There are no user accounts. Every request gets a pseudonymous reporter key
// derived from the client IP address (one VPN address is one person), used to
// count distinct people affected and, for administrators only, as an audit
// trail. Administration requires a TLS client certificate issued by the
// Turnbacks admin CA (see admin.go).
package auth

import (
	"context"
	"net"
	"net/http"
	"net/netip"

	"turnbacks/internal/domain"
)

type ctxKey struct{}

// WithActor stores the actor in the context.
func WithActor(ctx context.Context, a domain.Actor) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

// ActorFrom returns the request's actor.
func ActorFrom(ctx context.Context) domain.Actor {
	a, _ := ctx.Value(ctxKey{}).(domain.Actor)
	return a
}

// ClientIP returns the client address of the request. r.RemoteAddr has
// already been rewritten from X-Forwarded-For by the trusted-proxy middleware
// when (and only when) the direct peer is a configured trusted proxy.
func ClientIP(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap().WithZone(""), true
}

// ReporterKey is the pseudonymous reporter key for a request.
func ReporterKey(r *http.Request) string {
	if ip, ok := ClientIP(r); ok {
		return "ip:" + ip.String()
	}
	return ""
}

type peerKey struct{}

// Peer is the TCP peer of a request before any X-Forwarded-For rewriting.
type Peer struct {
	Addr netip.Addr
	// TrustedProxy is true when the peer is a configured trusted proxy, so
	// headers it sets (X-Forwarded-For, the client-certificate header) may be
	// believed.
	TrustedProxy bool
}

// WithPeer records the direct peer; the trusted-proxy middleware calls it
// before rewriting r.RemoteAddr.
func WithPeer(ctx context.Context, p Peer) context.Context {
	return context.WithValue(ctx, peerKey{}, p)
}

// PeerFrom returns the direct peer recorded by WithPeer.
func PeerFrom(ctx context.Context) (Peer, bool) {
	p, ok := ctx.Value(peerKey{}).(Peer)
	return p, ok
}

type unixKey struct{}

// MarkUnixSocket records that the request arrived on a Unix-socket listener.
// Only processes with access to the socket file (in practice, the reverse
// proxy sharing its volume) can connect, so such peers are trusted proxies.
func MarkUnixSocket(ctx context.Context) context.Context {
	return context.WithValue(ctx, unixKey{}, true)
}

// ViaUnixSocket reports whether the request arrived on a Unix socket.
func ViaUnixSocket(ctx context.Context) bool {
	v, _ := ctx.Value(unixKey{}).(bool)
	return v
}
