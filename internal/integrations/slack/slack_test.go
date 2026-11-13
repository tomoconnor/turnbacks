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

package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"
	"time"
)

func sign(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "v0:%s:", ts)
	mac.Write(body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerify(t *testing.T) {
	secret := "8f742231b10e8888abcd99yyyzzz85a5"
	now := time.Unix(1_800_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte("token=x&team_id=T1&user_id=U1&command=%2Fturnback&text=vpn+10")
	sig := sign(secret, ts, body)

	if err := Verify([]byte(secret), ts, sig, body, now); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := Verify([]byte(secret), ts, sig, append(body, 'x'), now); err == nil {
		t.Error("tampered body accepted")
	}
	if err := Verify([]byte("other"), ts, sig, body, now); err == nil {
		t.Error("wrong secret accepted")
	}
	if err := Verify([]byte(secret), ts, sig, body, now.Add(6*time.Minute)); err == nil {
		t.Error("stale request accepted")
	}
	if err := Verify([]byte(secret), ts, "v1=abc", body, now); err == nil {
		t.Error("unknown version accepted")
	}
	if err := Verify([]byte(secret), "notanumber", sig, body, now); err == nil {
		t.Error("bad timestamp accepted")
	}
}

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in        string
		min       int
		confirmed bool
		ok        bool
	}{
		{"10", 10, false, true}, {"10m", 10, false, true}, {"2h", 120, false, true},
		{"180!", 180, true, true}, {"1hr", 60, false, true}, {"ten", 0, false, false}, {"-5", 0, false, false},
	}
	for _, c := range cases {
		m, conf, ok := ParseDuration(c.in)
		if m != c.min || conf != c.confirmed || ok != c.ok {
			t.Errorf("ParseDuration(%q) = %d %v %v", c.in, m, conf, ok)
		}
	}
}

func TestParseQuantity(t *testing.T) {
	cases := []struct {
		in        string
		n         int
		confirmed bool
		ok        bool
	}{{"x15", 15, false, true}, {"15x", 15, false, true}, {"X3", 3, false, true}, {"x30!", 30, true, true}, {"x0", 0, false, false}, {"15", 0, false, false}, {"again", 0, false, false}}
	for _, c := range cases {
		n, conf, ok := ParseQuantity(c.in)
		if n != c.n || conf != c.confirmed || ok != c.ok {
			t.Errorf("ParseQuantity(%q) = %d %v %v", c.in, n, conf, ok)
		}
	}
}
