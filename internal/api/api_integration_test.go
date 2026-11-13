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
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	apispec "turnbacks/api"
	"turnbacks/internal/api"
	"turnbacks/internal/auth"
	"turnbacks/internal/ca"
	"turnbacks/internal/config"
	"turnbacks/internal/domain"
	"turnbacks/internal/integrations/slack"
	"turnbacks/internal/seed"
	"turnbacks/internal/service"
	"turnbacks/internal/store"
	"turnbacks/internal/testdb"
)

const slackSecret = "test-signing-secret"

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	st     *store.Store
	svc    *service.Service
	caDir  string
	issued int
}

// newHarness starts a test server. An identity configuration may be given;
// the default identifies people by address.
func newHarness(t *testing.T, identity ...auth.IdentityConfig) *harness {
	t.Helper()
	var idc auth.IdentityConfig
	if len(identity) > 0 {
		idc = identity[0]
	}
	t.Parallel()
	st := testdb.New(t)
	log := testdb.Logger()
	svc := service.New(st, log)
	cfg := &config.Config{
		Env: config.EnvTest, RequestTimeout: 10 * time.Second,
		// The test client connects from loopback; trusting it lets each client
		// present its own address via X-Forwarded-For (one address = one person).
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
	}
	static := fstest.MapFS{
		"index.html":      {Data: []byte("<!doctype html><title>Turnbacks</title>")},
		"assets/app-1.js": {Data: []byte("console.log(1)")},
		"sw.js":           {Data: []byte("// sw")},
	}
	caDir := t.TempDir()
	if _, err := ca.Init(caDir, "", 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	admin, err := auth.NewAdmin(auth.AdminConfig{ClientCAFile: filepath.Join(caDir, ca.CertFile), CertHeader: "X-Client-Cert"}, idc, log)
	if err != nil {
		t.Fatal(err)
	}
	h := api.New(api.Deps{
		Config: cfg, Log: log, Store: st, Service: svc, Admin: admin,
		Static: static, OpenAPI: apispec.OpenAPI,
		Slack: slack.New(slackSecret, st, svc, log),
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &harness{t: t, srv: srv, st: st, svc: svc, caDir: caDir}
}

// issueCert issues an admin client certificate from caDir and returns it as
// the proxy would forward it (base64 DER, as Caddy does).
func issueCert(t *testing.T, caDir, name string) string {
	t.Helper()
	iss, err := ca.Issue(caDir, name, time.Hour, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(iss.CertPath)
	blk, _ := pem.Decode(b)
	return base64.StdEncoding.EncodeToString(blk.Bytes)
}

type client struct {
	h    *harness
	c    *http.Client
	ip   string
	cert string // forwarded client certificate, if any
}

// person returns a client that appears to come from the given VPN address.
func (h *harness) person(ip string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{h: h, ip: ip, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// admin returns a client presenting an admin client certificate, as
// forwarded by the TLS-terminating proxy.
func (h *harness) admin() *client {
	h.issued++
	c := h.person("10.8.0.1")
	c.cert = issueCert(h.t, h.caDir, fmt.Sprintf("admin-%d", h.issued))
	return c
}

func (c *client) do(method, path string, body any, hdr map[string]string) *http.Response {
	c.h.t.Helper()
	var rdr io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rdr = strings.NewReader(b)
		default:
			buf, _ := json.Marshal(b)
			rdr = bytes.NewReader(buf)
		}
	}
	req, _ := http.NewRequest(method, c.h.srv.URL+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.ip != "" {
		req.Header.Set("X-Forwarded-For", c.ip)
	}
	if c.cert != "" {
		req.Header.Set("X-Client-Cert", c.cert)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.h.t.Fatal(err)
	}
	return resp
}

func decodeBody[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var v T
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode %q: %v", b, err)
	}
	return v
}

func expectStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: status %d, want %d: %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, want, b)
	}
}

func (c *client) categoryID() string {
	cats := decodeBody[struct{ Items []api.Category }](c.h.t, c.do("GET", "/api/v1/categories", nil, nil))
	return cats.Items[0].ID.String()
}

func (c *client) createProblem(title, alias string) api.Problem {
	body := map[string]any{"title": title, "category_id": c.categoryID()}
	if alias != "" {
		body["alias"] = alias
	}
	resp := c.do("POST", "/api/v1/problems", body, nil)
	expectStatus(c.h.t, resp, http.StatusCreated)
	return decodeBody[api.Problem](c.h.t, resp)
}

func TestErrorEnvelopeAndRequestIDs(t *testing.T) {
	h := newHarness(t)
	alice := h.person("10.8.0.12")

	resp := alice.do("PUT", "/api/v1/settings", map[string]any{}, map[string]string{"X-Request-ID": "trace-abc-12345"})
	expectStatus(t, resp, http.StatusForbidden)
	if resp.Header.Get("X-Request-ID") != "trace-abc-12345" {
		t.Errorf("request id not echoed: %q", resp.Header.Get("X-Request-ID"))
	}
	e := decodeBody[api.ErrorBody](t, resp)
	if e.Error.Code != "admin_required" || e.Error.RequestID != "trace-abc-12345" {
		t.Errorf("error = %+v", e)
	}
	resp = alice.do("GET", "/healthz", nil, map[string]string{"X-Request-ID": "bad id"})
	if id := resp.Header.Get("X-Request-ID"); id == "" || strings.Contains(id, " ") {
		t.Errorf("request id = %q", id)
	}
	resp = alice.do("GET", "/api/v1/nope", nil, nil)
	expectStatus(t, resp, http.StatusNotFound)
	if decodeBody[api.ErrorBody](t, resp).Error.Code != "not_found" {
		t.Error("expected JSON not_found for unknown API path")
	}
	expectStatus(t, alice.do("POST", "/api/v1/turnbacks", "{not json", nil), http.StatusBadRequest)
	expectStatus(t, alice.do("POST", "/api/v1/turnbacks", map[string]any{"id": uuid.New(), "unknown_field": 1}, nil), http.StatusBadRequest)
	resp = alice.do("POST", "/api/v1/turnbacks", map[string]any{"id": uuid.New(), "problem_id": uuid.New(), "duration_minutes": 0}, nil)
	expectStatus(t, resp, http.StatusUnprocessableEntity)
	if e := decodeBody[api.ErrorBody](t, resp); e.Error.Code != "validation_failed" || len(e.Error.Fields) == 0 {
		t.Errorf("validation error = %+v", e)
	}
	expectStatus(t, alice.do("POST", "/api/v1/turnbacks", map[string]any{"id": uuid.New(), "problem_id": uuid.New(), "duration_minutes": 5, "source": "slack"}, nil), http.StatusUnprocessableEntity)
	expectStatus(t, alice.do("GET", "/api/v1/problems/not-a-uuid", nil, nil), http.StatusNotFound)
	expectStatus(t, alice.do("GET", "/api/v1/analytics/summary?period=fortnight", nil, nil), http.StatusUnprocessableEntity)
}

func TestSubmitUndoAndIdempotencyOverHTTP(t *testing.T) {
	h := newHarness(t)
	alice := h.person("10.8.0.12")
	bob := h.person("10.8.0.13")
	p := alice.createProblem("Corporate VPN disconnects", "vpn")
	id := uuid.New()
	at := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Second)
	body := map[string]any{"id": id, "problem_id": p.ID, "duration_minutes": 10, "note": "dropped again", "occurred_at": at, "source": "pwa_offline"}

	resp := alice.do("POST", "/api/v1/turnbacks", body, nil)
	expectStatus(t, resp, http.StatusCreated)
	first := decodeBody[api.SubmitResult](t, resp)
	if first.Replayed || first.UndoToken == "" || first.Turnback.Reporter != nil || !first.Turnback.OccurredAt.Equal(at) {
		t.Errorf("first = %+v", first)
	}
	resp = alice.do("POST", "/api/v1/turnbacks", body, nil)
	expectStatus(t, resp, http.StatusOK)
	if r := decodeBody[api.SubmitResult](t, resp); !r.Replayed || r.UndoToken != "" {
		t.Errorf("replay = %+v", r)
	}
	body["duration_minutes"] = 20
	expectStatus(t, alice.do("POST", "/api/v1/turnbacks", body, nil), http.StatusConflict)

	// Every client-submittable source is accepted end to end (API → database).
	for _, src := range []string{"web", "pwa_offline", "api", "cli"} {
		expectStatus(t, bob.do("POST", "/api/v1/turnbacks", map[string]any{"id": uuid.New(), "problem_id": p.ID, "duration_minutes": 5, "source": src}, nil), http.StatusCreated)
	}
	sum := decodeBody[api.Summary](t, alice.do("GET", "/api/v1/analytics/summary?period=last_7_days", nil, nil))
	if sum.Current.Turnbacks != 5 || sum.Current.DistinctReporters != 2 || sum.Current.CostMinor != 2500 {
		t.Errorf("summary = %+v", sum.Current)
	}

	// Undo needs the token; without it, deletion is refused.
	expectStatus(t, bob.do("DELETE", "/api/v1/turnbacks/"+id.String(), nil, nil), http.StatusForbidden)
	expectStatus(t, alice.do("DELETE", "/api/v1/turnbacks/"+id.String()+"?undo_token="+url.QueryEscape(first.UndoToken), nil, nil), http.StatusNoContent)

	// Quantity records individual occurrences; undo removes the batch.
	resp = alice.do("POST", "/api/v1/turnbacks", map[string]any{"id": uuid.New(), "problem_id": p.ID, "duration_minutes": 10, "quantity": 15}, nil)
	expectStatus(t, resp, http.StatusCreated)
	batch := decodeBody[api.SubmitResult](t, resp)
	if batch.Quantity != 15 || batch.Turnback.BatchID == nil {
		t.Errorf("batch = %+v", batch)
	}
	expectStatus(t, alice.do("DELETE", "/api/v1/turnbacks/"+batch.Turnback.ID.String()+"?batch=true&undo_token="+url.QueryEscape(batch.UndoToken), nil, nil), http.StatusNoContent)

	resp = alice.do("POST", "/api/v1/turnbacks", map[string]any{"id": uuid.New(), "problem_id": p.ID, "duration_minutes": 300}, nil)
	expectStatus(t, resp, http.StatusUnprocessableEntity)
	if decodeBody[api.ErrorBody](t, resp).Error.Code != "duration_confirmation_required" {
		t.Error("expected duration_confirmation_required")
	}
}

func TestAdminClientCertificate(t *testing.T) {
	h := newHarness(t)
	alice := h.person("10.8.0.12")
	info := decodeBody[api.SessionInfo](t, alice.do("GET", "/api/v1/session", nil, nil))
	if info.Admin || !info.AdminEnabled {
		t.Errorf("session = %+v", info)
	}

	admin := h.admin()
	info = decodeBody[api.SessionInfo](t, admin.do("GET", "/api/v1/session", nil, nil))
	if !info.Admin || info.AdminName != "admin-1" {
		t.Errorf("admin certificate not recognised: %+v", info)
	}
	put := map[string]any{"org_name": "Acme", "currency": "GBP", "default_hourly_rate_minor": 6000, "reporting_timezone": "Europe/London",
		"recoverable_pct": 80, "large_duration_minutes": 120, "note_retention_days": nil, "reporter_retention_days": nil}
	expectStatus(t, alice.do("PUT", "/api/v1/settings", put, nil), http.StatusForbidden)
	expectStatus(t, admin.do("PUT", "/api/v1/settings", put, nil), http.StatusOK)

	// A certificate from some other CA is not an admin certificate.
	otherCA := t.TempDir()
	if _, err := ca.Init(otherCA, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	mallory := h.person("10.8.0.66")
	mallory.cert = issueCert(t, otherCA, "admin-1")
	expectStatus(t, mallory.do("PUT", "/api/v1/settings", put, nil), http.StatusForbidden)

}

func TestAuditViewAndExports(t *testing.T) {
	h := newHarness(t)
	alice := h.person("10.8.0.12")
	admin := h.admin()
	p := alice.createProblem("=HYPERLINK(\"http://x\")", "")
	expectStatus(t, alice.do("POST", "/api/v1/turnbacks", map[string]any{"id": uuid.New(), "problem_id": p.ID, "duration_minutes": 5, "note": "+cmd|' /C calc'!A0"}, nil), http.StatusCreated)

	// Everyone sees reports; only admins see who reported them.
	l := decodeBody[api.List[api.Turnback]](t, alice.do("GET", "/api/v1/turnbacks", nil, nil))
	if l.Total != 1 || l.Items[0].Reporter != nil {
		t.Errorf("public list = %+v", l)
	}
	l = decodeBody[api.List[api.Turnback]](t, admin.do("GET", "/api/v1/turnbacks?reporter=ip:10.8.0.12", nil, nil))
	if l.Total != 1 || l.Items[0].Reporter == nil || *l.Items[0].Reporter != "ip:10.8.0.12" {
		t.Errorf("admin audit = %+v", l)
	}
	expectStatus(t, alice.do("GET", "/api/v1/turnbacks?reporter=ip:10.8.0.12", nil, nil), http.StatusForbidden)

	for _, path := range []string{"/api/v1/analytics/teams", "/api/v1/problems/" + p.ID.String() + "/business-case", "/api/v1/analytics/export?dataset=problems"} {
		expectStatus(t, alice.do("GET", path, nil, nil), http.StatusOK)
	}
	csvOf := func(c *client) string {
		resp := c.do("GET", "/api/v1/analytics/export?dataset=turnbacks&period=last_7_days", nil, nil)
		expectStatus(t, resp, http.StatusOK)
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	public, private := csvOf(alice), csvOf(admin)
	if !strings.Contains(public, `'=HYPERLINK`) || !strings.Contains(public, `'+cmd`) {
		t.Errorf("formula not neutralised: %s", public)
	}
	if strings.Contains(public, "10.8.0.12") || !strings.Contains(private, "ip:10.8.0.12") {
		t.Errorf("reporter column must be admin-only:\npublic=%s\nadmin=%s", public, private)
	}
}

func TestCSRFProtection(t *testing.T) {
	h := newHarness(t)
	alice := h.person("10.8.0.12")
	body := map[string]any{"title": "Some problem", "category_id": alice.categoryID()}
	expectStatus(t, alice.do("POST", "/api/v1/problems", body, map[string]string{"Sec-Fetch-Site": "cross-site"}), http.StatusForbidden)
	expectStatus(t, alice.do("POST", "/api/v1/problems", body, map[string]string{"Origin": "https://evil.example.com"}), http.StatusForbidden)
	expectStatus(t, alice.do("POST", "/api/v1/problems", "title=x", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}), http.StatusUnsupportedMediaType)
	expectStatus(t, alice.do("POST", "/api/v1/problems", body, map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": h.srv.URL}), http.StatusCreated)
}

func TestSPAAndSecurityHeaders(t *testing.T) {
	h := newHarness(t)
	c := h.person("")
	resp := c.do("GET", "/dashboard/anything", nil, nil)
	expectStatus(t, resp, http.StatusOK)
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "<title>Turnbacks</title>") || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("SPA fallback: %q %q", b, resp.Header.Get("Cache-Control"))
	}
	for _, hdr := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if resp.Header.Get(hdr) == "" {
			t.Errorf("missing %s", hdr)
		}
	}
	if resp := c.do("GET", "/assets/app-1.js", nil, nil); !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("asset cache = %q", resp.Header.Get("Cache-Control"))
	}
	expectStatus(t, c.do("GET", "/assets/missing.js", nil, nil), http.StatusNotFound)
	if resp := c.do("GET", "/api/v1/session", nil, nil); resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("api cache = %q", resp.Header.Get("Cache-Control"))
	}
	expectStatus(t, c.do("GET", "/readyz", nil, nil), http.StatusOK)
	expectStatus(t, c.do("GET", "/api/v1/openapi.yaml", nil, nil), http.StatusOK)
}

func slackPost(t *testing.T, h *harness, user, text string, ts time.Time) *http.Response {
	t.Helper()
	form := url.Values{"team_id": {"T1"}, "user_id": {user}, "command": {"/turnback"}, "text": {text}}.Encode()
	tsStr := strconv.FormatInt(ts.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(slackSecret))
	_, _ = fmt.Fprintf(mac, "v0:%s:%s", tsStr, form)
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/integrations/slack/commands", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", tsStr)
	req.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func slackText(t *testing.T, resp *http.Response) string {
	t.Helper()
	expectStatus(t, resp, http.StatusOK)
	return decodeBody[struct{ Text string }](t, resp).Text
}

func TestSlackCommand(t *testing.T) {
	h := newHarness(t)
	alice := h.person("10.8.0.12")
	alice.createProblem("Corporate VPN disconnects", "vpn")
	now := time.Now()

	ts := now.Add(time.Second)
	if txt := slackText(t, slackPost(t, h, "U1", "vpn 10 disconnected again", ts)); !strings.Contains(txt, "Recorded 10 min") {
		t.Fatalf("record reply = %q", txt)
	}
	expectStatus(t, slackPost(t, h, "U1", "vpn 10 disconnected again", ts), http.StatusUnauthorized)   // replay
	expectStatus(t, slackPost(t, h, "U1", "vpn 5", now.Add(-10*time.Minute)), http.StatusUnauthorized) // stale
	if txt := slackText(t, slackPost(t, h, "U2", "vpn 10 x3 all morning", now.Add(2*time.Second))); !strings.Contains(txt, "Recorded 3 × 10 min") {
		t.Errorf("quantity reply = %q", txt)
	}
	if txt := slackText(t, slackPost(t, h, "U2", "nope 10", now.Add(3*time.Second))); !strings.Contains(txt, "No problem has the alias") {
		t.Errorf("unknown alias reply = %q", txt)
	}
	if txt := slackText(t, slackPost(t, h, "U2", "vpn 300 all afternoon", now.Add(4*time.Second))); !strings.Contains(txt, "confirm") {
		t.Errorf("large reply = %q", txt)
	}
	// Slack users count as people affected.
	sum := decodeBody[api.Summary](t, alice.do("GET", "/api/v1/analytics/summary?period=last_7_days", nil, nil))
	if sum.Current.Turnbacks != 4 || sum.Current.DistinctReporters != 2 {
		t.Errorf("summary = %+v", sum.Current)
	}
	l := decodeBody[api.List[api.Turnback]](t, h.admin().do("GET", "/api/v1/turnbacks?reporter=slack:T1/U1", nil, nil))
	if l.Total != 1 || l.Items[0].Source != "slack" || *l.Items[0].Note != "disconnected again" {
		t.Errorf("slack audit = %+v", l)
	}
}

func TestSeedTotalsAgree(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	res, err := seed.Run(t.Context(), h.st, now, 7)
	if err != nil {
		t.Fatal(err)
	}
	if res.Turnbacks < 1000 || res.Remediations != 1 || res.People != 32 {
		t.Fatalf("seed = %+v", res)
	}
	if _, err := seed.Run(t.Context(), h.st, now, 7); err != seed.ErrAlreadySeeded {
		t.Errorf("second seed: %v", err)
	}
	c := h.person("10.8.0.12")
	q := "?period=this_year"
	sum := decodeBody[api.Summary](t, c.do("GET", "/api/v1/analytics/summary"+q, nil, nil))
	rows, err := h.st.Pool.Query(t.Context(), `
		SELECT t.duration_minutes, coalesce(tm.hourly_rate_minor, s.default_hourly_rate_minor)
		FROM turnbacks t LEFT JOIN teams tm ON tm.id = t.team_id CROSS JOIN org_settings s
		WHERE t.occurred_at >= $1 AND t.occurred_at < $2`, sum.Period.Start, sum.Period.End)
	if err != nil {
		t.Fatal(err)
	}
	var n, minutes, minuteMinor int64
	for rows.Next() {
		var m, rate int64
		if err := rows.Scan(&m, &rate); err != nil {
			t.Fatal(err)
		}
		n, minutes, minuteMinor = n+1, minutes+m, minuteMinor+m*rate
	}
	wantCost := domain.RoundRat(domain.Ratio(minuteMinor, 60))
	if sum.Current.Turnbacks != n || sum.Current.Minutes != minutes || sum.Current.CostMinor != wantCost || sum.Current.DistinctReporters != 32 {
		t.Fatalf("summary %+v != recomputed n=%d minutes=%d cost=%d", sum.Current, n, minutes, wantCost)
	}
	ts := decodeBody[api.Timeseries](t, c.do("GET", "/api/v1/analytics/timeseries"+q+"&bucket=week", nil, nil))
	var tsN int64
	for _, p := range ts.Points {
		tsN += p.Turnbacks
	}
	rank := decodeBody[api.Ranking](t, c.do("GET", "/api/v1/analytics/problems"+q+"&limit=100", nil, nil))
	var rN int64
	for _, r := range rank.Items {
		rN += r.Turnbacks
	}
	if tsN != n || rN != n || rank.Items[len(rank.Items)-1].CumulativeShareBP != 10000 {
		t.Errorf("timeseries %d / ranking %d != %d", tsN, rN, n)
	}
	if !sort.SliceIsSorted(rank.Items, func(i, j int) bool { return rank.Items[i].CostMinor > rank.Items[j].CostMinor }) {
		t.Error("ranking not sorted by cost")
	}
	teams := decodeBody[api.Breakdown[api.TeamRow]](t, c.do("GET", "/api/v1/analytics/teams"+q, nil, nil))
	for _, tr := range teams.Items {
		if tr.TeamID != nil && (tr.Headcount == nil || tr.ParticipationBP == nil) {
			t.Errorf("team participation missing: %+v", tr)
		}
	}
	rem := decodeBody[struct{ Items []api.Comparison }](t, c.do("GET", "/api/v1/analytics/remediations", nil, nil))
	if len(rem.Items) != 1 || rem.Items[0].Change.TurnbacksBP == nil || *rem.Items[0].Change.TurnbacksBP > -4000 {
		t.Errorf("remediation comparison = %+v", rem.Items)
	}
	if purged, err := seed.Purge(t.Context(), h.st); err != nil || purged == 0 {
		t.Fatalf("purge: %d %v", purged, err)
	}
}

// TestOpenAPIMatchesRouter ensures every route is documented and every
// documented operation exists.
func TestOpenAPIMatchesRouter(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(apispec.OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for path, ops := range spec.Paths {
		for method := range ops {
			if method != "parameters" {
				documented[strings.ToUpper(method)+" /api/v1"+path] = true
			}
		}
	}
	routes := api.Routes(api.Deps{Config: &config.Config{}, Admin: &auth.Admin{}, Slack: http.NotFoundHandler()})
	registered := map[string]bool{}
	for _, r := range routes {
		if !strings.HasPrefix(r, "GET /api/v1/openapi.yaml") && strings.Contains(r, "/api/v1/") {
			registered[r] = true
		}
	}
	if len(registered) < 30 || len(documented) < 30 {
		t.Fatalf("suspiciously few routes: registered=%d documented=%d", len(registered), len(documented))
	}
	for r := range registered {
		if !documented[r] {
			t.Errorf("route not documented in openapi.yaml: %s", r)
		}
	}
	for d := range documented {
		if !registered[d] {
			t.Errorf("documented operation not implemented: %s", d)
		}
	}
}
