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
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/auth"
	"turnbacks/internal/domain"
	"turnbacks/internal/integrations/chatcmd"
	"turnbacks/internal/service"
)

// maxHookBody bounds inbound webhook bodies.
const maxHookBody = 64 << 10

// Namespace for idempotency keys derived from Teams activity IDs.
var teamsIDNamespace = uuid.MustParse("1d1f5c0a-3c39-4f0e-9b0b-6a8f6f0e2d11")

// receiveHook serves POST /api/v1/hooks/{id}: inbound webhooks from other
// systems. Each kind authenticates in its own way; unknown, disabled and
// unauthenticated hooks all get the same 401 so hook IDs cannot be probed.
func (s *Server) receiveHook(w http.ResponseWriter, r *http.Request) {
	unauthorised := func(reason string) {
		s.Log.WarnContext(r.Context(), "inbound webhook rejected", "reason", reason)
		writeErr(w, r, http.StatusUnauthorized, "unauthorised", "unknown webhook or invalid credentials", nil)
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		unauthorised("malformed id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxHookBody+1))
	if err != nil || len(body) > maxHookBody {
		writeErr(w, r, http.StatusRequestEntityTooLarge, "bad_request", "request body too large", nil)
		return
	}
	hook, err := s.Service.InboundHook(r.Context(), id)
	if err != nil {
		if domain.IsKind(err, domain.KindNotFound) {
			unauthorised("unknown or disabled hook")
			return
		}
		s.errs.respond(w, r, err)
		return
	}
	switch hook.Kind {
	case service.InboundJSON:
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !service.CheckInboundToken(hook, strings.TrimSpace(token)) {
			unauthorised("invalid bearer token")
			return
		}
		s.Service.TouchInboundHook(r.Context(), hook.ID)
		s.receiveJSON(w, r, hook, body)
	case service.InboundMattermost:
		form, err := url.ParseQuery(string(body))
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, "bad_request", "expected a form-encoded slash command", nil)
			return
		}
		token := form.Get("token")
		if h, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Token "); ok {
			token = strings.TrimSpace(h)
		}
		if !service.CheckInboundToken(hook, token) {
			unauthorised("invalid Mattermost token")
			return
		}
		s.Service.TouchInboundHook(r.Context(), hook.ID)
		text := chatcmd.Handler{Svc: s.Service, Log: s.Log, Style: chatcmd.Style{
			Invocation: firstNonEmpty(form.Get("command"), "/turnback"), Bold: chatcmd.MarkdownBold,
		}}.Handle(r.Context(), chatcmd.Request{
			Reporter: "mattermost:" + form.Get("team_id") + "/" + form.Get("user_id"), Text: form.Get("text"),
			ID: uuid.New(), Source: domain.SourceMattermost,
		})
		writeJSON(w, http.StatusOK, map[string]string{"response_type": "ephemeral", "text": text})
	case service.InboundTeams:
		if hook.HmacSecret == nil || !VerifyTeamsSignature(*hook.HmacSecret, r.Header.Get("Authorization"), body) {
			unauthorised("invalid Teams signature")
			return
		}
		s.Service.TouchInboundHook(r.Context(), hook.ID)
		var act struct {
			ID   string `json:"id"`
			Text string `json:"text"`
			From struct {
				ID          string `json:"id"`
				AADObjectID string `json:"aadObjectId"`
			} `json:"from"`
		}
		if err := json.Unmarshal(body, &act); err != nil {
			writeErr(w, r, http.StatusBadRequest, "bad_request", "expected a Teams activity", nil)
			return
		}
		text := chatcmd.Handler{Svc: s.Service, Log: s.Log, Style: chatcmd.Style{
			Invocation: "@" + hook.Name, Bold: chatcmd.MarkdownBold,
		}}.Handle(r.Context(), chatcmd.Request{
			Reporter: "teams:" + firstNonEmpty(act.From.AADObjectID, act.From.ID), Text: TeamsCommandText(act.Text),
			ID: uuid.NewSHA1(teamsIDNamespace, []byte(hook.ID.String()+"/"+act.ID)), Source: domain.SourceTeams,
		})
		writeJSON(w, http.StatusOK, map[string]string{"type": "message", "text": text})
	default:
		unauthorised("unknown kind")
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// VerifyTeamsSignature checks a Teams outgoing-webhook "Authorization: HMAC
// <base64>" header: HMAC-SHA256 of the body keyed with the base64-decoded
// security token.
func VerifyTeamsSignature(secret, header string, body []byte) bool {
	sig, ok := strings.CutPrefix(header, "HMAC ")
	if !ok {
		return false
	}
	key, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return false
	}
	got, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

var (
	teamsMention = regexp.MustCompile(`(?s)<at>.*?</at>`)
	htmlTag      = regexp.MustCompile(`<[^>]*>`)
)

// TeamsCommandText extracts the command from a Teams message: the bot
// mention and HTML markup are removed and entities decoded.
func TeamsCommandText(s string) string {
	s = teamsMention.ReplaceAllString(s, " ")
	s = htmlTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, " ", " ")
	return strings.Join(strings.Fields(s), " ")
}

// hookRequest is the body of a json inbound webhook.
type hookRequest struct {
	// Action is "turnback" (default) or "change".
	Action string `json:"action"`
	// Problem is a problem alias; ProblemID may be given instead.
	Problem   string     `json:"problem"`
	ProblemID *uuid.UUID `json:"problem_id"`
	// Turnbacks.
	ID         *uuid.UUID `json:"id"`
	Minutes    int        `json:"minutes"`
	Quantity   int        `json:"quantity"`
	Note       *string    `json:"note"`
	Reporter   string     `json:"reporter"`
	OccurredAt *time.Time `json:"occurred_at"`
	Confirm    bool       `json:"confirm"`
	// Changes.
	Title       string  `json:"title"`
	Description string  `json:"description"`
	URL         *string `json:"url"`
}

func (s *Server) receiveJSON(w http.ResponseWriter, r *http.Request, hook sqlc.InboundHook, body []byte) {
	var req hookRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, "bad_request", "invalid JSON: "+strings.TrimPrefix(err.Error(), "json: "), nil)
		return
	}
	reporter := "hook:" + hook.Name
	if u := auth.NormaliseUser(req.Reporter); u != "" {
		reporter += ":" + u
	}
	if len(reporter) > 200 {
		reporter = reporter[:200]
	}
	actor := domain.Actor{Reporter: reporter}

	problemID := req.ProblemID
	if problemID == nil && req.Problem != "" {
		p, err := s.Service.ProblemByAlias(r.Context(), req.Problem)
		if err != nil {
			s.errs.respond(w, r, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "unknown problem alias",
				Fields: []domain.FieldError{{Field: "problem", Message: "no problem has this alias"}}})
			return
		}
		problemID = &p.ID
	}

	switch req.Action {
	case "", "turnback":
		if problemID == nil {
			s.errs.respond(w, r, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "problem is required",
				Fields: []domain.FieldError{{Field: "problem", Message: "give a problem alias or problem_id"}}})
			return
		}
		id := uuid.New()
		if req.ID != nil {
			id = *req.ID
		}
		res, err := s.Service.SubmitTurnback(r.Context(), actor, service.TurnbackInput{
			ID: id, ProblemID: *problemID, DurationMinutes: req.Minutes, DurationEstimated: true, Note: req.Note,
			OccurredAt: req.OccurredAt, Source: domain.SourceWebhook, ConfirmLargeDuration: req.Confirm, Quantity: req.Quantity,
		})
		if err != nil {
			s.errs.respond(w, r, err)
			return
		}
		status := http.StatusCreated
		if !res.Created {
			status = http.StatusOK
		}
		writeJSON(w, status, map[string]any{
			"action": "turnback", "id": res.Turnback.ID, "problem_id": res.Turnback.ProblemID, "problem_title": res.ProblemTitle,
			"quantity": res.Quantity, "replayed": !res.Created,
		})
	case "change":
		m, err := s.Service.CreateMarker(r.Context(), actor, service.MarkerInput{
			OccurredAt: req.OccurredAt, Title: req.Title, Description: req.Description, URL: req.URL,
			ProblemID: problemID, Source: service.MarkerSourceWebhook,
		})
		if err != nil {
			s.errs.respond(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"action": "change", "marker": toMarker(m, false)})
	default:
		s.errs.respond(w, r, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "unknown action",
			Fields: []domain.FieldError{{Field: "action", Message: "must be turnback or change"}}})
	}
}
