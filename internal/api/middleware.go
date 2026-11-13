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

package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"turnbacks/internal/auth"
	"turnbacks/internal/logx"
)

var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._\-]{8,64}$`)

// requestID assigns a correlation ID, honouring a well-formed inbound
// X-Request-ID header, and echoes it in the response.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !requestIDRe.MatchString(id) {
			b := make([]byte, 12)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(logx.WithRequestID(r.Context(), id)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// accessLog writes one structured line per request. Client IP addresses and
// user agents are deliberately not logged (privacy principle: no device or
// network identifiers are collected).
func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				return
			}
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}
			log.Log(r.Context(), level, "http request",
				"method", r.Method, "path", r.URL.Path, "status", rec.status,
				"bytes", rec.bytes, "duration_ms", time.Since(start).Milliseconds())
		})
	}
}

// recoverer converts panics into 500 responses.
func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					log.ErrorContext(r.Context(), "panic", "panic", v, "stack", string(debug.Stack()))
					writeErr(w, r, http.StatusInternalServerError, "internal", "an internal error occurred", nil)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// contentSecurityPolicy is strict: no inline scripts, no third-party origins.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; worker-src 'self'; manifest-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"

// secureHeaders sets conservative security headers on every response.
func secureHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", contentSecurityPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// trustedProxies rewrites r.RemoteAddr and the request scheme from
// X-Forwarded-For / X-Forwarded-Proto only when the direct peer is a
// configured trusted proxy. Client IPs are used solely for this purpose and
// are never logged or stored.
func trustedProxies(prefixes []netip.Prefix) func(http.Handler) http.Handler {
	trusted := func(addr string) bool {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return false
		}
		ip = ip.Unmap()
		for _, p := range prefixes {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := auth.Peer{}
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				if ip, err := netip.ParseAddr(host); err == nil {
					peer.Addr = ip.Unmap()
				}
			}
			peer.TrustedProxy = auth.ViaUnixSocket(r.Context()) || (len(prefixes) > 0 && trusted(r.RemoteAddr))
			r = r.WithContext(auth.WithPeer(r.Context(), peer))
			if !peer.TrustedProxy {
				r.Header.Del("X-Forwarded-Proto")
				next.ServeHTTP(w, r)
				return
			}
			// Walk X-Forwarded-For right-to-left, skipping trusted hops.
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				parts := strings.Split(xff, ",")
				for i := len(parts) - 1; i >= 0; i-- {
					p := strings.TrimSpace(parts[i])
					if !trusted(p) {
						if _, err := netip.ParseAddr(p); err == nil {
							r.RemoteAddr = net.JoinHostPort(p, "0")
						}
						break
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requestTimeout bounds handler execution time via the request context.
func requestTimeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// noStore prevents caching of API responses (they may contain private data,
// and the service worker must never serve stale authenticated content).
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// csrfGuard rejects cross-site state-changing requests. Session cookies are
// SameSite=Lax; additionally, unsafe methods must come from the same origin
// (Fetch Metadata / Origin) and carry a JSON body, which cross-site HTML
// forms cannot produce without a CORS preflight that this server never
// grants.
func csrfGuard(publicOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				writeErr(w, r, http.StatusForbidden, "csrf_rejected", "cross-site request rejected", nil)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
				if !sameOrigin(origin, r, publicOrigin) {
					writeErr(w, r, http.StatusForbidden, "csrf_rejected", "cross-origin request rejected", nil)
					return
				}
			}
			if r.Method != http.MethodDelete && r.ContentLength != 0 {
				if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
					writeErr(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", nil)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func sameOrigin(origin string, r *http.Request, publicOrigin string) bool {
	if publicOrigin != "" && origin == publicOrigin {
		return true
	}
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	return o == r.Host
}
