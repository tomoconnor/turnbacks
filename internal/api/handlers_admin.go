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

	"turnbacks/internal/auth"
	"turnbacks/internal/service"
)

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.Service.GetSettings(r.Context())
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := toSettings(st)
	out.DemoDataPresent, _ = s.Store.DemoDataPresent(r.Context())
	writeJSON(w, http.StatusOK, out)
}

type settingsRequest struct {
	OrgName                string `json:"org_name"`
	Currency               string `json:"currency"`
	DefaultHourlyRateMinor int64  `json:"default_hourly_rate_minor"`
	ReportingTimezone      string `json:"reporting_timezone"`
	RecoverablePct         int    `json:"recoverable_pct"`
	LargeDurationMinutes   int    `json:"large_duration_minutes"`
	NoteRetentionDays      *int   `json:"note_retention_days"`
	ReporterRetentionDays  *int   `json:"reporter_retention_days"`
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	st, err := s.Service.UpdateSettings(r.Context(), auth.ActorFrom(r.Context()), service.SettingsInput(req))
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettings(st))
}

func (s *Server) listTeams(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Service.ListTeams(r.Context(), r.URL.Query().Get("include_inactive") != "true")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := make([]Team, 0, len(rows))
	for _, t := range rows {
		out = append(out, toTeam(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

type teamRequest struct {
	Name            string `json:"name"`
	Active          *bool  `json:"active"`
	HourlyRateMinor *int64 `json:"hourly_rate_minor"`
	Headcount       *int32 `json:"headcount"`
}

func (req teamRequest) input() service.TeamInput {
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	return service.TeamInput{Name: req.Name, Active: active, HourlyRateMinor: req.HourlyRateMinor, Headcount: req.Headcount}
}

func (s *Server) createTeam(w http.ResponseWriter, r *http.Request) {
	var req teamRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	t, err := s.Service.CreateTeam(r.Context(), auth.ActorFrom(r.Context()), req.input())
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toTeam(t))
}

func (s *Server) updateTeam(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var req teamRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	t, err := s.Service.UpdateTeam(r.Context(), auth.ActorFrom(r.Context()), id, req.input())
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTeam(t))
}

func (s *Server) listCategories(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Service.ListCategories(r.Context(), r.URL.Query().Get("include_inactive") != "true")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := make([]Category, 0, len(rows))
	for _, c := range rows {
		out = append(out, toCategory(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

type categoryRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      *bool  `json:"active"`
	SortOrder   int    `json:"sort_order"`
}

func (req categoryRequest) input() service.CategoryInput {
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	return service.CategoryInput{Name: req.Name, Description: req.Description, Active: active, SortOrder: req.SortOrder}
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	c, err := s.Service.CreateCategory(r.Context(), auth.ActorFrom(r.Context()), req.input())
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toCategory(c))
}

func (s *Server) updateCategory(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var req categoryRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	c, err := s.Service.UpdateCategory(r.Context(), auth.ActorFrom(r.Context()), id, req.input())
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toCategory(c))
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
