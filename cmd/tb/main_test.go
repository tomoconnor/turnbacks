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

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAPI implements the endpoints the CLI uses.
type fakeAPI struct {
	mu      sync.Mutex
	posts   []map[string]any
	deletes []string
	// confirm: when true, large entries need confirm_large_duration.
	requireConfirm bool
}

func (f *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/problems", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"id": "p-vpn", "title": "Corporate VPN disconnects", "alias": "vpn", "category_name": "Networking", "recent_count": 3},
			{"id": "p-ci", "title": "CI runner intermittently unavailable", "alias": "ci", "category_name": "CI/CD"},
			{"id": "p-dns1", "title": "Internal DNS failures", "category_name": "Networking"},
			{"id": "p-dns2", "title": "External DNS failures", "category_name": "Networking"},
		}})
	})
	mux.HandleFunc("GET /api/v1/teams", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "t-pay", "name": "Payments", "active": true}}})
	})
	mux.HandleFunc("GET /api/v1/categories", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "c-1", "name": "Networking"}}})
	})
	mux.HandleFunc("POST /api/v1/turnbacks", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.requireConfirm && body["confirm_large_duration"] != true {
			w.WriteHeader(422)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "duration_confirmation_required", "message": "300 minutes is unusually long."}})
			return
		}
		f.posts = append(f.posts, body)
		q, _ := body["quantity"].(float64)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"turnback": map[string]any{"id": body["id"], "problem_title": "Corporate VPN disconnects"},
			"replayed": false, "quantity": int(q), "undo_token": "tok-" + body["id"].(string)[:4],
		})
	})
	mux.HandleFunc("DELETE /api/v1/turnbacks/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.deletes = append(f.deletes, r.PathValue("id")+"?"+r.URL.RawQuery)
		f.mu.Unlock()
		w.WriteHeader(204)
	})
	return mux
}

func setup(t *testing.T) (*fakeAPI, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/config")
	t.Setenv("XDG_STATE_HOME", dir+"/state")
	f := &fakeAPI{}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	t.Setenv("TURNBACKS_URL", srv.URL)
	return f, srv
}

func tb(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := run(args, &out, &errOut)
	return out.String() + errOut.String(), err
}

func TestParseMinutes(t *testing.T) {
	cases := map[string]int{"10": 10, "10m": 10, "1h": 60, "1h30m": 90, "90m": 90, " 5 ": 5}
	for in, want := range cases {
		if got, err := parseMinutes(in); err != nil || got != want {
			t.Errorf("parseMinutes(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"0", "ten", "-5", "30s"} {
		if _, err := parseMinutes(bad); err == nil {
			t.Errorf("parseMinutes(%q) should fail", bad)
		}
	}
}

func TestParseWhen(t *testing.T) {
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	if got, _ := parseWhen("90m", "", now); !got.Equal(now.Add(-90 * time.Minute)) {
		t.Errorf("--ago: %s", got)
	}
	if got, _ := parseWhen("", "09:15", now); got.Hour() != 9 || got.Minute() != 15 || got.Day() != 12 {
		t.Errorf("--at: %s", got)
	}
	if _, err := parseWhen("1h", "09:00", now); err == nil {
		t.Error("both --ago and --at should fail")
	}
}

func TestParseRecordFlagsInterleaved(t *testing.T) {
	f, err := parseRecordFlags([]string{"vpn", "-n", "15", "10", "dropped", "--yes", "again"})
	if err != nil {
		t.Fatal(err)
	}
	if f.times != 15 || !f.yes || strings.Join(f.positional, " ") != "vpn 10 dropped again" {
		t.Errorf("parsed %+v", f)
	}
}

func TestRecordShorthandResolvesAliasAndTeam(t *testing.T) {
	f, _ := setup(t)
	if out, err := tb(t, "config", "set", "team", "payments"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	out, err := tb(t, "vpn", "10", "-n", "3", "dropped", "during", "deploy")
	if err != nil || !strings.Contains(out, "Recorded 3 × 10 min") {
		t.Fatalf("%v %s", err, out)
	}
	p := f.posts[0]
	if p["problem_id"] != "p-vpn" || p["duration_minutes"] != 10.0 || p["quantity"] != 3.0 || p["note"] != "dropped during deploy" ||
		p["team_id"] != "t-pay" || p["source"] != "cli" || p["duration_estimated"] != true {
		t.Errorf("posted %+v", p)
	}
	// Title words resolve too; ambiguous matches are explained.
	if _, err := tb(t, "record", "runner", "5"); err != nil || f.posts[1]["problem_id"] != "p-ci" {
		t.Errorf("title search: %v %+v", err, f.posts)
	}
	if _, err := tb(t, "dns", "5"); err == nil || !strings.Contains(err.Error(), "several problems") {
		t.Errorf("ambiguous: %v", err)
	}
}

func TestUndoAndAgain(t *testing.T) {
	f, _ := setup(t)
	if _, err := tb(t, "vpn", "10", "-n", "2"); err != nil {
		t.Fatal(err)
	}
	id := f.posts[0]["id"].(string)
	if out, err := tb(t, "undo"); err != nil || !strings.Contains(out, "Undone") {
		t.Fatalf("%v %s", err, out)
	}
	if len(f.deletes) != 1 || !strings.HasPrefix(f.deletes[0], id+"?") || !strings.Contains(f.deletes[0], "batch=true") || !strings.Contains(f.deletes[0], "undo_token=tok-") {
		t.Errorf("delete = %v", f.deletes)
	}
	if _, err := tb(t, "undo"); err == nil || !strings.Contains(err.Error(), "already undone") {
		t.Errorf("second undo: %v", err)
	}
	if out, err := tb(t, "again"); err != nil || !strings.Contains(out, "Recorded 10 min") {
		t.Errorf("again: %v %s", err, out)
	}
	if f.posts[1]["id"] == id || f.posts[1]["problem_id"] != "p-vpn" {
		t.Errorf("again posted %+v", f.posts[1])
	}
}

func TestOfflineQueueIsSentLaterWithTheSameID(t *testing.T) {
	f, srv := setup(t)
	if _, err := tb(t, "problems"); err != nil { // warm the cache
		t.Fatal(err)
	}
	t.Setenv("TURNBACKS_URL", "http://127.0.0.1:1") // unreachable
	out, err := tb(t, "vpn", "10")
	if err != nil || !strings.Contains(out, "Saved locally") {
		t.Fatalf("offline: %v %s", err, out)
	}
	q, _ := loadQueue()
	if len(q) != 1 {
		t.Fatalf("queue = %+v", q)
	}
	t.Setenv("TURNBACKS_URL", srv.URL)
	out, err = tb(t, "teams") // any command flushes first
	if err != nil || !strings.Contains(out, "Sent queued report") {
		t.Fatalf("flush: %v %s", err, out)
	}
	if len(f.posts) != 1 || f.posts[0]["id"] != q[0].ID {
		t.Errorf("posted %+v, queued id %s", f.posts, q[0].ID)
	}
	if q, _ := loadQueue(); len(q) != 0 {
		t.Errorf("queue not emptied: %+v", q)
	}
	// The flushed report can still be undone (token captured at flush).
	if _, err := tb(t, "undo"); err != nil {
		t.Errorf("undo after flush: %v", err)
	}
}

func TestLargeEntriesNeedConfirmation(t *testing.T) {
	f, _ := setup(t)
	f.requireConfirm = true
	if _, err := tb(t, "vpn", "300"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected refusal, got %v", err)
	}
	if out, err := tb(t, "vpn", "300", "--yes"); err != nil || !strings.Contains(out, "Recorded") {
		t.Fatalf("%v %s", err, out)
	}
}

func TestCompletion(t *testing.T) {
	setup(t)
	out, err := tb(t, "completion", "bash")
	if err != nil || !strings.Contains(out, "complete -F _tb tb") {
		t.Fatalf("bash: %v", err)
	}
	out, _ = tb(t, "completion", "zsh")
	if !strings.HasPrefix(out, "#compdef tb") {
		t.Errorf("zsh script header")
	}
	if _, err := tb(t, "problems"); err != nil {
		t.Fatal(err)
	}
	out, _ = tb(t, "__complete", "problems")
	if out != "ci\tCI runner intermittently unavailable\nvpn\tCorporate VPN disconnects\n" {
		t.Errorf("problem candidates = %q", out)
	}
	if out, _ := tb(t, "__complete", "teams"); out != "Payments\n" {
		t.Errorf("team candidates = %q", out)
	}
}
