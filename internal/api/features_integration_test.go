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

package api_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"turnbacks/internal/api"
	"turnbacks/internal/auth"
	"turnbacks/internal/webhook"
)

var putSettings = map[string]any{"org_name": "Acme", "currency": "GBP", "default_hourly_rate_minor": 6000, "reporting_timezone": "Europe/London",
	"recoverable_pct": 80, "large_duration_minutes": 120, "note_retention_days": nil, "reporter_retention_days": nil}

func TestSingleSignOnIdentity(t *testing.T) {
	h := newHarness(t, auth.IdentityConfig{
		Mode: auth.IdentityHeader, UserHeader: "X-Forwarded-Email", GroupsHeader: "X-Forwarded-Groups",
		AdminGroups: []string{"turnbacks-admins"},
	})
	ann := map[string]string{"X-Forwarded-Email": "Ann@Example.com"}
	boss := map[string]string{"X-Forwarded-Email": "boss@example.com", "X-Forwarded-Groups": "staff,turnbacks-admins"}
	c := h.person("10.8.0.12")

	info := decodeBody[api.SessionInfo](t, c.do("GET", "/api/v1/session", nil, ann))
	if info.IdentityMode != "header" || info.User != "ann@example.com" || info.Admin {
		t.Errorf("ann session = %+v", info)
	}
	info = decodeBody[api.SessionInfo](t, c.do("GET", "/api/v1/session", nil, boss))
	if !info.Admin || info.AdminName != "boss@example.com" || info.AdminVia != "sso" || len(info.AdminMethods) != 2 {
		t.Errorf("boss session = %+v", info)
	}

	p := c.createProblem("Single sign-on problem", "")
	expectStatus(t, c.do("POST", "/api/v1/turnbacks", map[string]any{"id": uuid.New(), "problem_id": p.ID, "duration_minutes": 5}, ann), http.StatusCreated)
	// Without the header (e.g. the CLI on a route that bypasses single sign-on), the address is used.
	expectStatus(t, c.do("POST", "/api/v1/turnbacks", map[string]any{"id": uuid.New(), "problem_id": p.ID, "duration_minutes": 5}, nil), http.StatusCreated)

	l := decodeBody[api.List[api.Turnback]](t, c.do("GET", "/api/v1/turnbacks", nil, boss))
	got := map[string]bool{}
	for _, it := range l.Items {
		got[*it.Reporter] = true
	}
	if !got["user:ann@example.com"] || !got["ip:10.8.0.12"] {
		t.Errorf("reporters = %v", got)
	}
	expectStatus(t, c.do("PUT", "/api/v1/settings", putSettings, ann), http.StatusForbidden)
	expectStatus(t, c.do("PUT", "/api/v1/settings", putSettings, boss), http.StatusOK)
}

func TestChangeMarkersBeforeAndAfter(t *testing.T) {
	h := newHarness(t)
	alice := h.person("10.8.0.12")
	admin := h.admin()
	p := alice.createProblem("VPN drops", "vpn")
	other := alice.createProblem("Slow builds", "")
	now := time.Now().UTC()
	record := func(problem uuid.UUID, daysAgo int) {
		expectStatus(t, alice.do("POST", "/api/v1/turnbacks", map[string]any{
			"id": uuid.New(), "problem_id": problem, "duration_minutes": 10, "occurred_at": now.AddDate(0, 0, -daysAgo),
		}, nil), http.StatusCreated)
	}
	record(p.ID, 12)
	record(p.ID, 12)
	record(p.ID, 5)
	record(other.ID, 5)

	// Anyone can mark a change.
	resp := alice.do("POST", "/api/v1/changes", map[string]any{
		"title": "VPN client 2.3 rolled out", "problem_id": p.ID, "occurred_at": now.AddDate(0, 0, -10), "url": "https://example.com/change/1",
	}, nil)
	expectStatus(t, resp, http.StatusCreated)
	m := decodeBody[api.ChangeMarker](t, resp)
	if m.ProblemTitle == nil || *m.ProblemTitle != "VPN drops" || m.Source != "web" || m.CreatedBy != nil {
		t.Errorf("marker = %+v", m)
	}
	global := decodeBody[api.ChangeMarker](t, alice.do("POST", "/api/v1/changes", map[string]any{"title": "New laptops", "occurred_at": now.AddDate(0, 0, -10)}, nil))

	c := decodeBody[api.MarkerComparison](t, alice.do("GET", "/api/v1/changes/"+m.ID.String()+"/comparison?window_days=7", nil, nil))
	if c.Before.Totals.Turnbacks != 2 || c.After.Totals.Turnbacks != 1 || c.WindowSeconds != 7*86400 || !c.InsufficientData { // under 14 days is flagged as insufficient
		t.Errorf("problem comparison = %+v", c)
	}
	if c.Change.TurnbacksBP == nil || *c.Change.TurnbacksBP != -5000 {
		t.Errorf("change = %+v", c.Change)
	}
	// Organisation-wide changes compare everything.
	c = decodeBody[api.MarkerComparison](t, alice.do("GET", "/api/v1/changes/"+global.ID.String()+"/comparison?window_days=7", nil, nil))
	if c.Before.Totals.Turnbacks != 2 || c.After.Totals.Turnbacks != 2 {
		t.Errorf("global comparison = %+v", c)
	}
	// The window is limited to the time elapsed since the change.
	recent := decodeBody[api.ChangeMarker](t, alice.do("POST", "/api/v1/changes", map[string]any{"title": "Just now", "occurred_at": now.Add(-3 * time.Hour)}, nil))
	c = decodeBody[api.MarkerComparison](t, alice.do("GET", "/api/v1/changes/"+recent.ID.String()+"/comparison", nil, nil))
	if c.WindowSeconds < 3*3600-120 || c.WindowSeconds > 3*3600 || !c.InsufficientData {
		t.Errorf("recent comparison = %+v", c)
	}

	// Listing a problem's changes includes organisation-wide ones unless asked not to.
	list := decodeBody[struct{ Items []api.ChangeMarker }](t, alice.do("GET", "/api/v1/changes?problem_id="+p.ID.String(), nil, nil))
	if len(list.Items) != 3 {
		t.Errorf("with global: %d", len(list.Items))
	}
	list = decodeBody[struct{ Items []api.ChangeMarker }](t, alice.do("GET", "/api/v1/changes?include_global=false&problem_id="+p.ID.String(), nil, nil))
	if len(list.Items) != 1 {
		t.Errorf("without global: %d", len(list.Items))
	}
	overview := decodeBody[struct{ Items []api.MarkerComparison }](t, alice.do("GET", "/api/v1/analytics/changes?window_days=7", nil, nil))
	if len(overview.Items) != 3 {
		t.Errorf("overview: %d", len(overview.Items))
	}

	// Validation.
	expectStatus(t, alice.do("POST", "/api/v1/changes", map[string]any{"title": "Future", "occurred_at": now.Add(time.Hour)}, nil), http.StatusUnprocessableEntity)
	expectStatus(t, alice.do("POST", "/api/v1/changes", map[string]any{"title": ""}, nil), http.StatusUnprocessableEntity)
	expectStatus(t, alice.do("POST", "/api/v1/changes", map[string]any{"title": "x", "source": "webhook"}, nil), http.StatusUnprocessableEntity)

	// Only administrators can edit and delete, and they see who marked it.
	expectStatus(t, alice.do("DELETE", "/api/v1/changes/"+m.ID.String(), nil, nil), http.StatusForbidden)
	upd := decodeBody[api.ChangeMarker](t, admin.do("PATCH", "/api/v1/changes/"+m.ID.String(), map[string]any{
		"title": "VPN client 2.3", "occurred_at": m.OccurredAt, "problem_id": p.ID,
	}, nil))
	if upd.Title != "VPN client 2.3" || upd.CreatedBy == nil || *upd.CreatedBy != "ip:10.8.0.12" {
		t.Errorf("updated = %+v", upd)
	}
	expectStatus(t, admin.do("DELETE", "/api/v1/changes/"+m.ID.String(), nil, nil), http.StatusNoContent)
	expectStatus(t, admin.do("DELETE", "/api/v1/changes/"+m.ID.String(), nil, nil), http.StatusNotFound)
}

// receiver records webhook requests.
type receiver struct {
	mu     sync.Mutex
	reqs   []*http.Request
	bodies [][]byte
	status int
}

func (rc *receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.reqs = append(rc.reqs, r)
	rc.bodies = append(rc.bodies, b)
	if rc.status != 0 {
		w.WriteHeader(rc.status)
	}
}

func (rc *receiver) count() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return len(rc.reqs)
}

func TestOutboundWebhooks(t *testing.T) {
	h := newHarness(t)
	h.svc.SetWebhookClient(webhook.NewClient(5*time.Second, true))
	h.svc.SetPublicURL("https://turnbacks.example.com")
	alice := h.person("10.8.0.12")
	admin := h.admin()
	rc := &receiver{}
	srv := httptest.NewServer(rc)
	t.Cleanup(srv.Close)
	ctx := context.Background()

	expectStatus(t, alice.do("POST", "/api/v1/webhooks", map[string]any{"name": "x", "url": srv.URL, "format": "json", "events": []string{"turnback.recorded"}}, nil), http.StatusForbidden)
	expectStatus(t, admin.do("POST", "/api/v1/webhooks", map[string]any{"name": "x", "url": "ftp://x", "format": "json", "events": []string{"nope"}}, nil), http.StatusUnprocessableEntity)

	resp := admin.do("POST", "/api/v1/webhooks", map[string]any{
		"name": "Audit sink", "url": srv.URL + "/hook", "format": "json", "events": []string{"turnback.recorded", "change.marked"},
	}, nil)
	expectStatus(t, resp, http.StatusCreated)
	hook := decodeBody[api.Webhook](t, resp)
	if !strings.HasPrefix(hook.Secret, "whsec_") || !hook.Enabled {
		t.Fatalf("webhook = %+v", hook)
	}
	chat := decodeBody[api.Webhook](t, admin.do("POST", "/api/v1/webhooks", map[string]any{
		"name": "Chat", "url": srv.URL + "/chat", "format": "slack", "events": []string{"problem.created"},
	}, nil))

	p := alice.createProblem("Flaky <DNS> & things", "")
	expectStatus(t, alice.do("POST", "/api/v1/turnbacks", map[string]any{"id": uuid.New(), "problem_id": p.ID, "duration_minutes": 10, "quantity": 3}, nil), http.StatusCreated)
	if _, err := h.svc.DeliverDueWebhooks(ctx); err != nil {
		t.Fatal(err)
	}
	if rc.count() != 2 {
		t.Fatalf("deliveries = %d, want 2 (problem.created to chat, turnback.recorded to json)", rc.count())
	}
	var sawJSON, sawChat bool
	for i, r := range rc.reqs {
		body := rc.bodies[i]
		switch r.URL.Path {
		case "/hook":
			sawJSON = true
			ts, _ := strconv.ParseInt(r.Header.Get(webhook.HeaderTimestamp), 10, 64)
			if !webhook.Verify(hook.Secret, ts, body, r.Header.Get(webhook.HeaderSignature)) {
				t.Error("bad signature")
			}
			var env struct {
				Event string         `json:"event"`
				URL   string         `json:"url"`
				Data  map[string]any `json:"data"`
			}
			_ = json.Unmarshal(body, &env)
			if env.Event != "turnback.recorded" || env.Data["quantity"] != float64(3) || !strings.HasPrefix(env.URL, "https://turnbacks.example.com/problems/") {
				t.Errorf("envelope = %s", body)
			}
			if strings.Contains(string(body), "10.8.0.12") {
				t.Error("reporter leaked in webhook payload")
			}
		case "/chat":
			sawChat = true
			var msg struct{ Text string }
			_ = json.Unmarshal(body, &msg)
			if !strings.Contains(msg.Text, "Flaky &lt;DNS&gt; &amp; things") || !strings.Contains(msg.Text, "|Open in Turnbacks>") {
				t.Errorf("slack text = %q", msg.Text)
			}
			if r.Header.Get(webhook.HeaderSignature) != "" {
				t.Error("chat formats are not signed")
			}
		}
	}
	if !sawJSON || !sawChat {
		t.Error("missing deliveries")
	}

	// Server errors are retried later; client errors are not.
	rc.status = http.StatusBadGateway
	expectStatus(t, admin.do("POST", "/api/v1/webhooks/"+hook.ID.String()+"/test", nil, nil), http.StatusAccepted)
	_, _ = h.svc.DeliverDueWebhooks(ctx)
	ds := decodeBody[struct{ Items []api.WebhookDelivery }](t, admin.do("GET", "/api/v1/webhooks/"+hook.ID.String()+"/deliveries", nil, nil))
	if ds.Items[0].Event != "test" || ds.Items[0].Status != "pending" || ds.Items[0].Attempts != 1 || *ds.Items[0].LastStatusCode != 502 {
		t.Errorf("after 502: %+v", ds.Items[0])
	}
	rc.status = http.StatusNotFound
	expectStatus(t, admin.do("POST", "/api/v1/webhooks/"+chat.ID.String()+"/test", nil, nil), http.StatusAccepted)
	_, _ = h.svc.DeliverDueWebhooks(ctx)
	ds = decodeBody[struct{ Items []api.WebhookDelivery }](t, admin.do("GET", "/api/v1/webhooks/"+chat.ID.String()+"/deliveries", nil, nil))
	if ds.Items[0].Status != "failed" {
		t.Errorf("after 404: %+v", ds.Items[0])
	}
	list := decodeBody[struct{ Items []api.Webhook }](t, admin.do("GET", "/api/v1/webhooks", nil, nil))
	if len(list.Items) != 2 || list.Items[0].Secret != "" || list.Items[0].LastResult == nil {
		t.Errorf("list = %+v", list.Items)
	}

	// Rotating the secret returns the new one.
	rot := decodeBody[api.Webhook](t, admin.do("POST", "/api/v1/webhooks/"+hook.ID.String()+"/rotate-secret", nil, nil))
	if rot.Secret == "" || rot.Secret == hook.Secret {
		t.Error("secret not rotated")
	}

	// Loopback is refused unless allowed; such failures are not retried.
	h.svc.SetWebhookClient(webhook.NewClient(5*time.Second, false))
	expectStatus(t, admin.do("POST", "/api/v1/webhooks/"+hook.ID.String()+"/test", nil, nil), http.StatusAccepted)
	_, _ = h.svc.DeliverDueWebhooks(ctx)
	ds = decodeBody[struct{ Items []api.WebhookDelivery }](t, admin.do("GET", "/api/v1/webhooks/"+hook.ID.String()+"/deliveries", nil, nil))
	if ds.Items[0].Status != "failed" || ds.Items[0].LastError == nil || !strings.Contains(*ds.Items[0].LastError, "not allowed") {
		t.Errorf("loopback: %+v", ds.Items[0])
	}

	// Disabling a webhook stops deliveries that are already queued.
	h.svc.SetWebhookClient(webhook.NewClient(5*time.Second, true))
	rc.status = http.StatusBadGateway
	expectStatus(t, admin.do("POST", "/api/v1/webhooks/"+hook.ID.String()+"/test", nil, nil), http.StatusAccepted)
	_, _ = h.svc.DeliverDueWebhooks(ctx) // fails, retry scheduled
	expectStatus(t, admin.do("PATCH", "/api/v1/webhooks/"+hook.ID.String(), map[string]any{
		"name": "Audit sink", "url": srv.URL + "/hook", "format": "json", "events": []string{"turnback.recorded"}, "enabled": false,
	}, nil), http.StatusOK)
	if _, err := h.st.Pool.Exec(ctx, "UPDATE webhook_deliveries SET next_attempt_at = now() WHERE status = 'pending'"); err != nil {
		t.Fatal(err)
	}
	before := rc.count()
	_, _ = h.svc.DeliverDueWebhooks(ctx)
	if rc.count() != before {
		t.Error("disabled webhook still delivered")
	}
	ds = decodeBody[struct{ Items []api.WebhookDelivery }](t, admin.do("GET", "/api/v1/webhooks/"+hook.ID.String()+"/deliveries", nil, nil))
	if ds.Items[0].Status != "failed" || ds.Items[0].LastError == nil || *ds.Items[0].LastError != "webhook disabled" {
		t.Errorf("disabled: %+v", ds.Items[0])
	}

	expectStatus(t, admin.do("DELETE", "/api/v1/webhooks/"+hook.ID.String(), nil, nil), http.StatusNoContent)
	expectStatus(t, admin.do("GET", "/api/v1/webhooks/"+hook.ID.String()+"/deliveries", nil, nil), http.StatusNotFound)
}

func TestInboundWebhooks(t *testing.T) {
	h := newHarness(t)
	alice := h.person("10.8.0.12")
	admin := h.admin()
	p := alice.createProblem("VPN drops", "vpn")
	post := func(path, body, contentType string, hdr map[string]string) *http.Response {
		req, _ := http.NewRequest("POST", h.srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// Generic JSON with a bearer token generated by Turnbacks.
	expectStatus(t, alice.do("POST", "/api/v1/inbound-hooks", map[string]any{"name": "CI", "kind": "json"}, nil), http.StatusForbidden)
	resp := admin.do("POST", "/api/v1/inbound-hooks", map[string]any{"name": "CI", "kind": "json"}, nil)
	expectStatus(t, resp, http.StatusCreated)
	ci := decodeBody[api.InboundHook](t, resp)
	if !strings.HasPrefix(ci.Token, "tbhook_") || !strings.HasSuffix(ci.URL, "/api/v1/hooks/"+ci.ID.String()) {
		t.Fatalf("hook = %+v", ci)
	}
	bearer := map[string]string{"Authorization": "Bearer " + ci.Token}
	expectStatus(t, post(ci.URL[strings.Index(ci.URL, "/api"):], `{"problem":"vpn","minutes":10}`, "application/json", map[string]string{"Authorization": "Bearer wrong"}), http.StatusUnauthorized)
	expectStatus(t, post("/api/v1/hooks/"+uuid.NewString(), `{}`, "application/json", bearer), http.StatusUnauthorized)
	id := uuid.New()
	body := `{"problem":"vpn","minutes":10,"quantity":2,"reporter":"nightly-build","id":"` + id.String() + `"}`
	expectStatus(t, post("/api/v1/hooks/"+ci.ID.String(), body, "application/json", bearer), http.StatusCreated)
	expectStatus(t, post("/api/v1/hooks/"+ci.ID.String(), body, "application/json", bearer), http.StatusOK) // idempotent replay
	expectStatus(t, post("/api/v1/hooks/"+ci.ID.String(), `{"problem":"nope","minutes":10}`, "application/json", bearer), http.StatusUnprocessableEntity)
	resp = post("/api/v1/hooks/"+ci.ID.String(), `{"action":"change","problem":"vpn","title":"Deployed VPN 2.4","url":"https://ci.example.com/1"}`, "application/json", bearer)
	expectStatus(t, resp, http.StatusCreated)
	l := decodeBody[api.List[api.Turnback]](t, admin.do("GET", "/api/v1/turnbacks?problem_id="+p.ID.String(), nil, nil))
	if l.Total != 2 || *l.Items[0].Reporter != "hook:CI:nightly-build" || l.Items[0].Source != "webhook" {
		t.Errorf("json hook turnbacks = %+v", l)
	}
	marks := decodeBody[struct{ Items []api.ChangeMarker }](t, alice.do("GET", "/api/v1/changes", nil, nil))
	if len(marks.Items) != 1 || marks.Items[0].Source != "webhook" || marks.Items[0].Title != "Deployed VPN 2.4" {
		t.Errorf("markers = %+v", marks.Items)
	}

	// Mattermost slash command, authenticated with Mattermost's token.
	expectStatus(t, admin.do("POST", "/api/v1/inbound-hooks", map[string]any{"name": "MM", "kind": "mattermost", "secret": "short"}, nil), http.StatusUnprocessableEntity)
	mmToken := "mattermost-generated-token-123"
	mm := decodeBody[api.InboundHook](t, admin.do("POST", "/api/v1/inbound-hooks", map[string]any{"name": "MM", "kind": "mattermost", "secret": mmToken}, nil))
	if mm.Token != "" {
		t.Error("mattermost tokens are not echoed")
	}
	form := url.Values{"token": {mmToken}, "team_id": {"t1"}, "user_id": {"u1"}, "command": {"/turnback"}, "text": {"vpn 15 again"}}
	resp = post("/api/v1/hooks/"+mm.ID.String(), form.Encode(), "application/x-www-form-urlencoded", nil)
	expectStatus(t, resp, http.StatusOK)
	reply := decodeBody[map[string]string](t, resp)
	if !strings.Contains(reply["text"], "Recorded 15 min against **VPN drops**") || reply["response_type"] != "ephemeral" {
		t.Errorf("mattermost reply = %v", reply)
	}
	form.Set("token", "wrong-token-xxxxxxxxxxxx")
	expectStatus(t, post("/api/v1/hooks/"+mm.ID.String(), form.Encode(), "application/x-www-form-urlencoded", nil), http.StatusUnauthorized)

	// Microsoft Teams outgoing webhook, authenticated with an HMAC signature.
	key := []byte("0123456789abcdef0123456789abcdef")
	secret := base64.StdEncoding.EncodeToString(key)
	tm := decodeBody[api.InboundHook](t, admin.do("POST", "/api/v1/inbound-hooks", map[string]any{"name": "Turnbacks", "kind": "teams", "secret": secret}, nil))
	activity := `{"type":"message","id":"act-1","text":"<at>Turnbacks</at>&nbsp;vpn 20 x2 <b>ugh</b>","from":{"id":"29:1","aadObjectId":"aad-1"}}`
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(activity))
	sig := map[string]string{"Authorization": "HMAC " + base64.StdEncoding.EncodeToString(mac.Sum(nil))}
	resp = post("/api/v1/hooks/"+tm.ID.String(), activity, "application/json", sig)
	expectStatus(t, resp, http.StatusOK)
	reply = decodeBody[map[string]string](t, resp)
	if reply["type"] != "message" || !strings.Contains(reply["text"], "Recorded 2 × 20 min") {
		t.Errorf("teams reply = %v", reply)
	}
	expectStatus(t, post("/api/v1/hooks/"+tm.ID.String(), activity, "application/json", map[string]string{"Authorization": "HMAC AAAA"}), http.StatusUnauthorized)
	l = decodeBody[api.List[api.Turnback]](t, admin.do("GET", "/api/v1/turnbacks?problem_id="+p.ID.String(), nil, nil))
	sources := map[string]int{}
	for _, it := range l.Items {
		sources[it.Source+" "+*it.Reporter]++
	}
	if sources["mattermost mattermost:t1/u1"] != 1 || sources["teams teams:aad-1"] != 2 {
		t.Errorf("sources = %v", sources)
	}

	// Disabled hooks are refused; replacing the token invalidates the old one.
	expectStatus(t, admin.do("PATCH", "/api/v1/inbound-hooks/"+ci.ID.String(), map[string]any{"name": "CI", "enabled": false}, nil), http.StatusOK)
	expectStatus(t, post("/api/v1/hooks/"+ci.ID.String(), `{"problem":"vpn","minutes":10}`, "application/json", bearer), http.StatusUnauthorized)
	expectStatus(t, admin.do("PATCH", "/api/v1/inbound-hooks/"+ci.ID.String(), map[string]any{"name": "CI", "enabled": true}, nil), http.StatusOK)
	fresh := decodeBody[api.InboundHook](t, admin.do("POST", "/api/v1/inbound-hooks/"+ci.ID.String()+"/credential", map[string]any{}, nil))
	expectStatus(t, post("/api/v1/hooks/"+ci.ID.String(), `{"problem":"vpn","minutes":10}`, "application/json", bearer), http.StatusUnauthorized)
	expectStatus(t, post("/api/v1/hooks/"+ci.ID.String(), `{"problem":"vpn","minutes":10}`, "application/json", map[string]string{"Authorization": "Bearer " + fresh.Token}), http.StatusCreated)
	hooks := decodeBody[struct{ Items []api.InboundHook }](t, admin.do("GET", "/api/v1/inbound-hooks", nil, nil))
	if len(hooks.Items) != 3 || hooks.Items[0].LastUsedAt == nil || hooks.Items[0].Token != "" {
		t.Errorf("hooks = %+v", hooks.Items)
	}
	expectStatus(t, admin.do("DELETE", "/api/v1/inbound-hooks/"+mm.ID.String(), nil, nil), http.StatusNoContent)
}

func TestAdminAuditLog(t *testing.T) {
	h := newHarness(t)
	alice := h.person("10.8.0.12")
	admin := h.admin()
	alice.createProblem("Not audited", "")
	expectStatus(t, admin.do("PUT", "/api/v1/settings", putSettings, nil), http.StatusOK)
	expectStatus(t, admin.do("GET", "/api/v1/turnbacks", nil, nil), http.StatusOK)
	expectStatus(t, admin.do("GET", "/api/v1/analytics/summary", nil, nil), http.StatusOK) // not audited
	expectStatus(t, admin.do("GET", "/api/v1/changes", nil, nil), http.StatusOK)           // shows marker creators
	expectStatus(t, admin.do("PATCH", "/api/v1/teams/"+uuid.NewString(), map[string]any{"name": "x"}, nil), http.StatusNotFound)

	expectStatus(t, alice.do("GET", "/api/v1/audit", nil, nil), http.StatusForbidden)
	expectStatus(t, alice.do("GET", "/api/v1/audit/export", nil, nil), http.StatusForbidden)

	l := decodeBody[struct {
		Items []api.AuditEntry
		Total int64
	}](t, admin.do("GET", "/api/v1/audit", nil, nil))
	// Newest first: the PATCH (404), the GETs of reporter data, the PUT.
	if l.Total != 4 || len(l.Items) != 4 {
		t.Fatalf("audit = %+v", l)
	}
	if l.Items[0].Action != "/api/v1/teams/{id}" || l.Items[0].Status != 404 || !strings.HasPrefix(l.Items[0].Target, "id=") {
		t.Errorf("patch entry = %+v", l.Items[0])
	}
	if l.Items[1].Method != "GET" || l.Items[1].Action != "/api/v1/changes" {
		t.Errorf("read entry = %+v", l.Items[1])
	}
	if l.Items[2].Method != "GET" || l.Items[2].Action != "/api/v1/turnbacks" {
		t.Errorf("read entry = %+v", l.Items[2])
	}
	put := l.Items[3]
	if put.Admin != "admin-1" || put.Via != "certificate" || put.Method != "PUT" || put.Action != "/api/v1/settings" || put.Status != 200 ||
		put.Client == nil || *put.Client != "ip:10.8.0.1" || put.RequestID == nil {
		t.Errorf("put entry = %+v", put)
	}

	resp := admin.do("GET", "/api/v1/audit/export", nil, nil)
	expectStatus(t, resp, http.StatusOK)
	b, _ := io.ReadAll(resp.Body)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if !strings.HasPrefix(lines[0], "id,at_utc,admin,via,method,action") || len(lines) < 4 {
		t.Errorf("audit csv = %s", b)
	}
	if !strings.Contains(string(b), "admin-1,certificate,PUT,/api/v1/settings,,200,ip:10.8.0.1") {
		t.Errorf("audit csv row missing: %s", b)
	}
}

func TestFullExports(t *testing.T) {
	h := newHarness(t)
	alice := h.person("10.8.0.12")
	admin := h.admin()
	p := alice.createProblem("Exported problem", "exp")
	expectStatus(t, alice.do("POST", "/api/v1/turnbacks", map[string]any{"id": uuid.New(), "problem_id": p.ID, "duration_minutes": 7, "note": "=1+1"}, nil), http.StatusCreated)
	expectStatus(t, alice.do("POST", "/api/v1/changes", map[string]any{"title": "Something changed", "problem_id": p.ID}, nil), http.StatusCreated)
	expectStatus(t, admin.do("POST", "/api/v1/problems/"+p.ID.String()+"/remediations", map[string]any{
		"implemented_on": time.Now().Format(time.DateOnly), "description": "Fixed it", "implementation_cost_minor": 12345,
	}, nil), http.StatusCreated)

	get := func(c *client, ds string) string {
		resp := c.do("GET", "/api/v1/exports/"+ds, nil, nil)
		expectStatus(t, resp, http.StatusOK)
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") {
			t.Errorf("%s content type %q", ds, resp.Header.Get("Content-Type"))
		}
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	public, private := get(alice, "turnbacks"), get(admin, "turnbacks")
	if strings.Contains(public, "reporter") || strings.Contains(public, "10.8.0.12") || !strings.Contains(public, "'=1+1") {
		t.Errorf("public turnbacks = %s", public)
	}
	if !strings.Contains(private, ",reporter\n") || !strings.Contains(private, "ip:10.8.0.12") {
		t.Errorf("admin turnbacks = %s", private)
	}
	if s := get(alice, "problems"); !strings.Contains(s, "Exported problem") || !strings.Contains(s, ",exp,") {
		t.Errorf("problems = %s", s)
	}
	if s := get(alice, "remediations"); !strings.Contains(s, "123.45") || !strings.Contains(s, "Fixed it") {
		t.Errorf("remediations = %s", s)
	}
	if s := get(alice, "changes"); !strings.Contains(s, "Something changed") || strings.Contains(s, "created_by") {
		t.Errorf("changes = %s", s)
	}
	if s := get(admin, "changes"); !strings.Contains(s, "ip:10.8.0.12") {
		t.Errorf("admin changes = %s", s)
	}
	expectStatus(t, alice.do("GET", "/api/v1/exports/secrets", nil, nil), http.StatusNotFound)
}

func TestTeamsHelpers(t *testing.T) {
	if got := api.TeamsCommandText("<at>Turnbacks</at>&nbsp;vpn 10 <b>very</b> &amp; slow again"); got != "vpn 10 very & slow again" {
		t.Errorf("TeamsCommandText = %q", got)
	}
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	mac := hmac.New(sha256.New, []byte("0123456789abcdef"))
	mac.Write([]byte("body"))
	good := "HMAC " + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if !api.VerifyTeamsSignature(key, good, []byte("body")) || api.VerifyTeamsSignature(key, good, []byte("other")) ||
		api.VerifyTeamsSignature(key, "Bearer x", []byte("body")) {
		t.Error("teams signature verification wrong")
	}
}

func TestHugeOffsetIsClamped(t *testing.T) {
	h := newHarness(t)
	alice := h.person("10.8.0.12")
	expectStatus(t, alice.do("GET", "/api/v1/turnbacks?offset=3000000000", nil, nil), http.StatusOK)
	expectStatus(t, alice.do("GET", "/api/v1/problems?offset=9223372036854775807", nil, nil), http.StatusOK)
}
