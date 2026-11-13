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

// Package api exposes the versioned REST API under /api/v1, health endpoints
// and the embedded single-page application.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"turnbacks/internal/auth"
	"turnbacks/internal/config"
	"turnbacks/internal/service"
	"turnbacks/internal/store"
)

// Deps are the server's dependencies.
type Deps struct {
	Config  *config.Config
	Log     *slog.Logger
	Store   *store.Store
	Service *service.Service
	Admin   *auth.Admin
	Slack   http.Handler // nil when Slack is not configured
	Static  fs.FS        // built SPA assets
	OpenAPI []byte       // OpenAPI document
}

// Server holds handler state.
type Server struct {
	Deps
	errs errorResponder
}

// New builds the HTTP handler.
func New(d Deps) http.Handler {
	s := &Server{Deps: d, errs: errorResponder{log: d.Log}}
	return s.routes()
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	publicOrigin := ""
	hsts := false
	if s.Config.PublicURL != nil {
		publicOrigin = s.Config.PublicURL.Scheme + "://" + s.Config.PublicURL.Host
		hsts = s.Config.PublicURL.Scheme == "https"
	}
	r.Use(requestID, trustedProxies(s.Config.TrustedProxies), accessLog(s.Log), recoverer(s.Log), secureHeaders(hsts))

	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(noStore, requestTimeout(s.Config.RequestTimeout))
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeErr(w, r, http.StatusNotFound, "not_found", "no such endpoint", nil)
		})
		r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			writeErr(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		})
		r.Get("/openapi.yaml", s.openAPI)

		// Slack verifies its own request signatures; no session or CSRF.
		if s.Slack != nil {
			r.Method(http.MethodPost, "/integrations/slack/commands", s.Slack)
		}
		// Inbound webhooks authenticate with their own tokens or signatures.
		r.Post("/hooks/{id}", s.receiveHook)

		r.Group(func(r chi.Router) {
			r.Use(s.Admin.Middleware, csrfGuard(publicOrigin), s.auditLog)
			if s.Config.WriteRateLimit > 0 {
				r.Use(newWriteLimiter(s.Config.WriteRateLimit).middleware)
			}

			r.Get("/session", s.sessionInfo)

			r.Get("/settings", s.getSettings)
			r.Put("/settings", s.updateSettings)

			r.Get("/teams", s.listTeams)
			r.Post("/teams", s.createTeam)
			r.Patch("/teams/{id}", s.updateTeam)

			r.Get("/categories", s.listCategories)
			r.Post("/categories", s.createCategory)
			r.Patch("/categories/{id}", s.updateCategory)

			r.Get("/problems", s.listProblems)
			r.Post("/problems", s.createProblem)
			r.Get("/problems/quick", s.quickProblems)
			r.Get("/problems/similar", s.similarProblems)
			r.Get("/problems/{id}", s.getProblem)
			r.Patch("/problems/{id}", s.updateProblem)
			r.Post("/problems/{id}/merge", s.mergeProblem)
			r.Get("/problems/{id}/business-case", s.businessCase)
			r.Get("/problems/{id}/business-case/export", s.businessCaseCSV)
			r.Get("/problems/{id}/remediations", s.listRemediations)
			r.Post("/problems/{id}/remediations", s.createRemediation)

			r.Patch("/remediations/{id}", s.updateRemediation)
			r.Delete("/remediations/{id}", s.deleteRemediation)
			r.Get("/remediations/{id}/comparison", s.remediationComparison)

			r.Post("/turnbacks", s.submitTurnback)
			r.Get("/turnbacks", s.listTurnbacks)
			r.Patch("/turnbacks/{id}", s.updateTurnback)
			r.Delete("/turnbacks/{id}", s.deleteTurnback)

			r.Get("/analytics/summary", s.analyticsSummary)
			r.Get("/analytics/timeseries", s.analyticsTimeseries)
			r.Get("/analytics/problems", s.analyticsProblems)
			r.Get("/analytics/categories", s.analyticsCategories)
			r.Get("/analytics/teams", s.analyticsTeams)
			r.Get("/analytics/remediations", s.analyticsRemediations)
			r.Get("/analytics/export", s.analyticsExport)
			r.Get("/analytics/changes", s.analyticsChanges)

			r.Get("/changes", s.listMarkers)
			r.Post("/changes", s.createMarker)
			r.Patch("/changes/{id}", s.updateMarker)
			r.Delete("/changes/{id}", s.deleteMarker)
			r.Get("/changes/{id}/comparison", s.markerComparison)

			r.Get("/exports/{dataset}", s.fullExport)

			r.Get("/webhooks", s.listWebhooks)
			r.Post("/webhooks", s.createWebhook)
			r.Patch("/webhooks/{id}", s.updateWebhook)
			r.Delete("/webhooks/{id}", s.deleteWebhook)
			r.Post("/webhooks/{id}/test", s.testWebhook)
			r.Post("/webhooks/{id}/rotate-secret", s.rotateWebhookSecret)
			r.Get("/webhooks/{id}/deliveries", s.listDeliveries)

			r.Get("/inbound-hooks", s.listInboundHooks)
			r.Post("/inbound-hooks", s.createInboundHook)
			r.Patch("/inbound-hooks/{id}", s.updateInboundHook)
			r.Delete("/inbound-hooks/{id}", s.deleteInboundHook)
			r.Post("/inbound-hooks/{id}/credential", s.replaceInboundCredential)

			r.Get("/audit", s.listAudit)
			r.Get("/audit/export", s.exportAudit)
		})
	})

	if s.Static != nil {
		r.Handle("/*", spaHandler(s.Static))
	}
	return r
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Store.Pool.Ping(ctx); err != nil {
		s.Log.WarnContext(r.Context(), "readiness check failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "database": "unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "database": "ok"})
}

func (s *Server) openAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(s.OpenAPI)
}

// Routes lists registered "METHOD /path" pairs (used to check the OpenAPI
// document stays in sync with the router).
func Routes(d Deps) []string {
	s := &Server{Deps: d}
	h := s.routes().(chi.Routes)
	var out []string
	_ = chi.Walk(h, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out = append(out, method+" "+strings.TrimSuffix(strings.ReplaceAll(route, "/*/", "/"), "/"))
		return nil
	})
	return out
}
