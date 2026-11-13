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
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"turnbacks/internal/auth"
)

// writeLimiter limits state-changing requests per reporter (the single
// sign-on user or network address) with a token bucket: up to perMinute
// requests in a burst, refilled at perMinute per minute. It stops one person
// flooding the analytics, or the organisation's webhooks, with reports.
// Administrators and reads are not limited. Limits are per replica.
type writeLimiter struct {
	perMinute float64
	now       func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newWriteLimiter(perMinute int) *writeLimiter {
	return &writeLimiter{perMinute: float64(perMinute), now: time.Now, buckets: map[string]*bucket{}}
}

// take spends a token for key, or reports how long until one is available.
func (l *writeLimiter) take(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	// Forget buckets that have refilled, so memory is bounded by the number
	// of recently active people.
	if now.Sub(l.swept) > time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.at) > time.Minute {
				delete(l.buckets, k)
			}
		}
		l.swept = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.perMinute, at: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.perMinute, b.tokens+now.Sub(b.at).Minutes()*l.perMinute)
	b.at = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.perMinute * float64(time.Minute))
}

func (l *writeLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		a := auth.ActorFrom(r.Context())
		if a.Admin || a.Reporter == "" {
			next.ServeHTTP(w, r)
			return
		}
		if ok, wait := l.take(a.Reporter); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			writeErr(w, r, http.StatusTooManyRequests, "rate_limited", "too many requests; try again shortly", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
