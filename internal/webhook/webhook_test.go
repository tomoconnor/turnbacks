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
	"context"
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
	if got := text(FormatMarkdown); got != `Change: \<b\> & co [Open in Turnbacks](https://tb.example/x)` {
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

// User-entered titles must not ping a channel or smuggle in links.
func TestRenderEscapesUserText(t *testing.T) {
	stored, _ := json.Marshal(Envelope{
		Event: EventProblemCreated, URL: "https://tb.example/p",
		Summary: "New problem: “@channel VPN down\n<!here> re-login at [Open in Turnbacks](https://evil.example/sso) *now*”",
	})
	render := func(format string) string {
		b, err := Render(format, stored)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	var md struct{ Text string }
	_ = json.Unmarshal([]byte(render(FormatMarkdown)), &md)
	for _, bad := range []string{"@channel", "[Open in Turnbacks](https://evil", "*now*", "\n"} {
		if strings.Contains(md.Text, bad) {
			t.Errorf("markdown contains %q: %q", bad, md.Text)
		}
	}
	if !strings.Contains(md.Text, `\[Open in Turnbacks\]\(https://evil.example/sso\)`) || !strings.HasSuffix(md.Text, "[Open in Turnbacks](https://tb.example/p)") {
		t.Errorf("markdown = %q", md.Text)
	}
	var sl struct{ Text string }
	_ = json.Unmarshal([]byte(render(FormatSlack)), &sl)
	if strings.Contains(sl.Text, "<!here>") || strings.Contains(sl.Text, "@channel") || strings.Contains(sl.Text, "\n") {
		t.Errorf("slack = %q", sl.Text)
	}
	teams := render(FormatTeams)
	if strings.Contains(teams, `"TextBlock"`) || !strings.Contains(teams, `"TextRun"`) {
		t.Errorf("teams summary must be a TextRun (no Markdown): %s", teams)
	}
}

func TestSendRedactsURL(t *testing.T) {
	c := NewClient(time.Second, false) // loopback refused
	res := Send(context.Background(), c, Delivery{
		ID: "d", Event: EventTest, URL: "http://127.0.0.1:9/services/T000/B000/s3cr3t?sig=abc", Format: FormatSlack,
		Stored: []byte(`{"summary":"x"}`),
	}, time.Now())
	if res.Err == nil || res.Retry {
		t.Fatalf("result = %+v", res)
	}
	if msg := res.Err.Error(); strings.Contains(msg, "s3cr3t") || strings.Contains(msg, "sig=") || !strings.Contains(msg, "127.0.0.1:9") {
		t.Errorf("error = %q", msg)
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
		{"100.100.100.200", true, false}, // Alibaba Cloud metadata
		{"fd00:ec2::254", true, false},   // AWS metadata (IPv6)
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
