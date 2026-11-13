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

package webhook

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestRender(t *testing.T) {
	stored, _ := json.Marshal(Envelope{Event: EventChangeMarked, Summary: "Change: <b> & co", URL: "https://tb.example/x", Data: map[string]int{"n": 1}})
	text := func(format string) string {
		b, err := Render(format, stored)
		if err != nil {
			t.Fatal(err)
		}
		var m struct{ Text string }
		_ = json.Unmarshal(b, &m)
		return m.Text
	}
	if got := text(FormatSlack); got != "Change: &lt;b&gt; &amp; co <https://tb.example/x|Open in Turnbacks>" {
		t.Errorf("slack = %q", got)
	}
	if got := text(FormatMarkdown); got != "Change: <b> & co [Open in Turnbacks](https://tb.example/x)" {
		t.Errorf("markdown = %q", got)
	}
	if b, _ := Render(FormatJSON, stored); string(b) != string(stored) {
		t.Errorf("json = %s", b)
	}
	if b, _ := Render(FormatTeams, stored); !strings.Contains(string(b), `"contentType":"application/vnd.microsoft.card.adaptive"`) || !strings.Contains(string(b), "Action.OpenUrl") {
		t.Errorf("teams = %s", b)
	}
	if _, err := Render("carrier-pigeon", stored); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestSignVerify(t *testing.T) {
	body := []byte(`{"a":1}`)
	sig := Sign("whsec_x", 1700000000, body)
	if !strings.HasPrefix(sig, "sha256=") || !Verify("whsec_x", 1700000000, body, sig) {
		t.Fatal("round trip failed")
	}
	if Verify("whsec_x", 1700000001, body, sig) || Verify("other", 1700000000, body, sig) || Verify("whsec_x", 1700000000, []byte(`{}`), sig) {
		t.Error("verification too lenient")
	}
}

func TestAddressPolicy(t *testing.T) {
	for _, c := range []struct {
		ip             string
		loopback, want bool
	}{
		{"10.1.2.3", false, true},
		{"192.168.1.10", false, true},
		{"203.0.113.5", false, true},
		{"169.254.169.254", true, false}, // cloud metadata
		{"fe80::1", true, false},
		{"0.0.0.0", true, false},
		{"127.0.0.1", false, false},
		{"127.0.0.1", true, true},
		{"::ffff:169.254.169.254", true, false},
	} {
		if got := allowed(netip.MustParseAddr(c.ip), c.loopback); got != c.want {
			t.Errorf("allowed(%s, loopback=%v) = %v", c.ip, c.loopback, got)
		}
	}
	for _, u := range []string{"ftp://x", "https://user:pw@x", "not a url", "https://"} {
		if ValidateURL(u) == nil {
			t.Errorf("ValidateURL(%q) accepted", u)
		}
	}
	if ValidateURL("https://chat.internal/hooks/abc") != nil {
		t.Error("valid URL rejected")
	}
}

func TestBackoff(t *testing.T) {
	if Backoff(1) != 30*time.Second || Backoff(2) != time.Minute || Backoff(30) != 6*time.Hour {
		t.Errorf("backoff = %v %v %v", Backoff(1), Backoff(2), Backoff(30))
	}
}
