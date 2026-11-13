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
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"turnbacks/db/sqlc"
	"turnbacks/internal/auth"
	"turnbacks/internal/logx"
	"turnbacks/internal/service"
)

// auditedReads are GET routes recorded in the audit log because they show
// administrators who reported what.
var auditedReads = map[string]bool{
	"/api/v1/turnbacks":                true,
	"/api/v1/exports/{dataset}":        true,
	"/api/v1/analytics/export":         true,
	"/api/v1/audit":                    true,
	"/api/v1/audit/export":             true,
	"/api/v1/webhooks/{id}/deliveries": true,
}

// auditLog records administrators' state-changing requests, and their reads
// of reporter data, in the audit log once the request has been handled.
func (s *Server) auditLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := auth.ActorFrom(r.Context())
		if !a.Admin {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		rc := chi.RouteContext(r.Context())
		pattern := ""
		if rc != nil {
			pattern = rc.RoutePattern()
		}
		read := r.Method == http.MethodGet || r.Method == http.MethodHead
		if read && !auditedReads[pattern] {
			return
		}
		if pattern == "/api/v1/exports/{dataset}" && chi.URLParam(r, "dataset") != "turnbacks" && chi.URLParam(r, "dataset") != "changes" {
			return
		}
		if pattern == "/api/v1/analytics/export" && r.URL.Query().Get("dataset") != "turnbacks" {
			return
		}
		var target []string
		if rc != nil {
			for i, k := range rc.URLParams.Keys {
				if k != "*" && i < len(rc.URLParams.Values) {
					target = append(target, k+"="+rc.URLParams.Values[i])
				}
			}
		}
		if q := r.URL.RawQuery; read && q != "" {
			target = append(target, q)
		}
		sort.Strings(target)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		// Recorded even if the client has gone away.
		ctx := context.WithoutCancel(r.Context())
		if err := s.Service.RecordAudit(ctx, service.AuditEntry{
			Admin: a.AdminName, Via: a.AdminVia, Method: r.Method, Action: pattern, Target: strings.Join(target, " "),
			Status: status, Client: a.Reporter, RequestID: logx.RequestID(r.Context()),
		}); err != nil {
			s.Log.ErrorContext(ctx, "audit log entry not saved", "error", err, "admin", a.AdminName, "method", r.Method, "path", r.URL.Path)
		}
	})
}

// AuditEntry is one administrative action.
type AuditEntry struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	Admin     string    `json:"admin"`
	Via       string    `json:"via"`
	Method    string    `json:"method"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Status    int32     `json:"status"`
	Client    *string   `json:"client"`
	RequestID *string   `json:"request_id"`
}

func toAudit(a sqlc.AdminAudit) AuditEntry {
	return AuditEntry{ID: a.ID, At: a.At, Admin: a.Admin, Via: a.Via, Method: a.Method, Action: a.Action,
		Target: a.Target, Status: a.Status, Client: a.Client, RequestID: a.RequestID}
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	from, e1 := queryTime(r, "from")
	to, e2 := queryTime(r, "to")
	before, e3 := queryInt64Ptr(r, "before_id")
	limit, e4 := queryInt(r, "limit", 100)
	if err := firstErr(e1, e2, e3, e4); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	rows, total, err := s.Service.ListAudit(r.Context(), auth.ActorFrom(r.Context()), service.AuditFilter{
		From: from, To: to, BeforeID: before, Limit: int32(min(max(limit, 1), 1000)), //nolint:gosec // clamped
	})
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := make([]AuditEntry, 0, len(rows))
	for _, a := range rows {
		out = append(out, toAudit(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

func (s *Server) exportAudit(w http.ResponseWriter, r *http.Request) {
	from, e1 := queryTime(r, "from")
	to, e2 := queryTime(r, "to")
	if err := firstErr(e1, e2); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	a := auth.ActorFrom(r.Context())
	if !a.Admin {
		// Fail before any CSV headers are written.
		if _, _, err := s.Service.ListAudit(r.Context(), a, service.AuditFilter{Limit: 1}); err != nil {
			s.errs.respond(w, r, err)
			return
		}
	}
	cw := startCSV(w, "turnbacks-audit_"+time.Now().UTC().Format("2006-01-02"))
	_ = cw.Write([]string{"id", "at_utc", "admin", "via", "method", "action", "target", "status", "client", "request_id"})
	err := s.Service.ExportAudit(r.Context(), a, from, to, func(e sqlc.AdminAudit) error {
		return cw.Write([]string{strconv.FormatInt(e.ID, 10), e.At.UTC().Format(time.RFC3339), csvSafe(e.Admin), e.Via, e.Method,
			e.Action, csvSafe(e.Target), strconv.Itoa(int(e.Status)), csvSafe(deref(e.Client)), deref(e.RequestID)})
	})
	if err != nil {
		s.Log.ErrorContext(r.Context(), "audit export failed mid-stream", "error", err)
	}
	cw.Flush()
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
