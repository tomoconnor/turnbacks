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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"turnbacks/internal/auth"
	"turnbacks/internal/domain"
)

func TestWriteLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	l := newWriteLimiter(3)
	l.now = func() time.Time { return now }
	h := l.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	send := func(method string, a domain.Actor) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/turnbacks", nil)
		r = r.WithContext(auth.WithActor(r.Context(), a))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	alice := domain.Actor{Reporter: "ip:10.0.0.1"}
	for i := 0; i < 3; i++ {
		if w := send("POST", alice); w.Code != http.StatusNoContent {
			t.Fatalf("request %d: %d", i, w.Code)
		}
	}
	w := send("POST", alice)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "20" {
		t.Fatalf("over limit: %d retry-after=%q", w.Code, w.Header().Get("Retry-After"))
	}
	// Reads, other people and administrators are not limited.
	if send("GET", alice).Code != http.StatusNoContent ||
		send("POST", domain.Actor{Reporter: "ip:10.0.0.2"}).Code != http.StatusNoContent ||
		send("POST", domain.Actor{Reporter: "ip:10.0.0.1", Admin: true}).Code != http.StatusNoContent {
		t.Error("unexpected limit")
	}
	// Tokens refill over time.
	now = now.Add(20 * time.Second)
	if send("POST", alice).Code != http.StatusNoContent {
		t.Error("not refilled")
	}
	// Idle buckets are forgotten.
	now = now.Add(5 * time.Minute)
	send("POST", domain.Actor{Reporter: "ip:10.0.0.3"})
	if len(l.buckets) != 1 {
		t.Errorf("buckets = %d", len(l.buckets))
	}
}
