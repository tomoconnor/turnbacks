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
	"time"

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/auth"
	"turnbacks/internal/service"
	"turnbacks/internal/webhook"
)

// Webhook is an outbound webhook. Secret is present only when the webhook is
// created or its secret is rotated.
type Webhook struct {
	ID         uuid.UUID        `json:"id"`
	Name       string           `json:"name"`
	URL        string           `json:"url"`
	Format     string           `json:"format"`
	Events     []string         `json:"events"`
	Enabled    bool             `json:"enabled"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	Secret     string           `json:"secret,omitempty"`
	LastResult *WebhookDelivery `json:"last_delivery,omitempty"`
}

// WebhookDelivery is one queued or attempted delivery.
type WebhookDelivery struct {
	ID             uuid.UUID  `json:"id"`
	Event          string     `json:"event"`
	Status         string     `json:"status"`
	Attempts       int32      `json:"attempts"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	LastStatusCode *int32     `json:"last_status_code"`
	LastError      *string    `json:"last_error"`
	CreatedAt      time.Time  `json:"created_at"`
	DeliveredAt    *time.Time `json:"delivered_at"`
}

func toWebhook(w sqlc.Webhook, withSecret bool) Webhook {
	out := Webhook{ID: w.ID, Name: w.Name, URL: w.Url, Format: w.Format, Events: w.Events, Enabled: w.Enabled,
		CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
	if withSecret {
		out.Secret = w.Secret
	}
	return out
}

func toDelivery(d sqlc.WebhookDelivery) WebhookDelivery {
	out := WebhookDelivery{ID: d.ID, Event: d.Event, Status: d.Status, Attempts: d.Attempts,
		LastStatusCode: d.LastStatusCode, LastError: d.LastError, CreatedAt: d.CreatedAt, DeliveredAt: d.DeliveredAt}
	if d.Status == "pending" {
		out.NextAttemptAt = &d.NextAttemptAt
	}
	return out
}

type webhookRequest struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Format  string   `json:"format"`
	Events  []string `json:"events"`
	Enabled *bool    `json:"enabled"`
}

func (req webhookRequest) input() service.WebhookInput {
	enabled := req.Enabled == nil || *req.Enabled
	return service.WebhookInput{Name: req.Name, URL: req.URL, Format: req.Format, Events: req.Events, Enabled: enabled}
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Service.ListWebhooks(r.Context(), auth.ActorFrom(r.Context()))
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := make([]Webhook, 0, len(rows))
	for _, v := range rows {
		h := toWebhook(v.Webhook, false)
		if v.Last != nil {
			d := toDelivery(*v.Last)
			h.LastResult = &d
		}
		out = append(out, h)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "events": webhook.Events, "formats": webhook.Formats})
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	var req webhookRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	h, err := s.Service.CreateWebhook(r.Context(), auth.ActorFrom(r.Context()), req.input())
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWebhook(h, true))
}

func (s *Server) updateWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var req webhookRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	h, err := s.Service.UpdateWebhook(r.Context(), auth.ActorFrom(r.Context()), id, req.input())
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toWebhook(h, false))
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	if err := s.Service.DeleteWebhook(r.Context(), auth.ActorFrom(r.Context()), id); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	h, err := s.Service.RotateWebhookSecret(r.Context(), auth.ActorFrom(r.Context()), id)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toWebhook(h, true))
}

func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	d, err := s.Service.TestWebhook(r.Context(), auth.ActorFrom(r.Context()), id)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toDelivery(d))
}

func (s *Server) listDeliveries(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	rows, err := s.Service.ListDeliveries(r.Context(), auth.ActorFrom(r.Context()), id)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := make([]WebhookDelivery, 0, len(rows))
	for _, d := range rows {
		out = append(out, toDelivery(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// InboundHook is an inbound webhook. Token is present only when a json hook
// is created or its token replaced.
type InboundHook struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Enabled    bool       `json:"enabled"`
	URL        string     `json:"url"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Token      string     `json:"token,omitempty"`
}

func (s *Server) toInbound(h sqlc.InboundHook, token string) InboundHook {
	path := "/api/v1/hooks/" + h.ID.String()
	if s.Config.PublicURL != nil {
		path = s.Config.PublicURL.String() + path
	}
	return InboundHook{ID: h.ID, Name: h.Name, Kind: h.Kind, Enabled: h.Enabled, URL: path,
		CreatedAt: h.CreatedAt, LastUsedAt: h.LastUsedAt, Token: token}
}

type inboundRequest struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Secret  string `json:"secret"`
	Enabled *bool  `json:"enabled"`
}

func (s *Server) listInboundHooks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Service.ListInboundHooks(r.Context(), auth.ActorFrom(r.Context()))
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	out := make([]InboundHook, 0, len(rows))
	for _, h := range rows {
		out = append(out, s.toInbound(h, ""))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) createInboundHook(w http.ResponseWriter, r *http.Request) {
	var req inboundRequest
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	res, err := s.Service.CreateInboundHook(r.Context(), auth.ActorFrom(r.Context()), service.InboundHookInput{
		Name: req.Name, Kind: req.Kind, Secret: req.Secret, Enabled: req.Enabled == nil || *req.Enabled,
	})
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.toInbound(res.Hook, res.Token))
}

func (s *Server) updateInboundHook(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var req struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	h, err := s.Service.UpdateInboundHook(r.Context(), auth.ActorFrom(r.Context()), id, req.Name, req.Enabled)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toInbound(h, ""))
}

func (s *Server) replaceInboundCredential(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	var req struct {
		Secret string `json:"secret"`
	}
	if err := decode(w, r, &req); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	res, err := s.Service.ReplaceInboundCredential(r.Context(), auth.ActorFrom(r.Context()), id, req.Secret)
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toInbound(res.Hook, res.Token))
}

func (s *Server) deleteInboundHook(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		s.errs.respond(w, r, err)
		return
	}
	if err := s.Service.DeleteInboundHook(r.Context(), auth.ActorFrom(r.Context()), id); err != nil {
		s.errs.respond(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
