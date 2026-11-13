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

package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
	"turnbacks/internal/webhook"
)

// ---- Events -----------------------------------------------------------------

// enqueuer is satisfied by the store and by a transaction's queries, so events
// can be queued in the same transaction as the change they describe.
type enqueuer interface {
	EnqueueDeliveries(ctx context.Context, arg sqlc.EnqueueDeliveriesParams) (int64, error)
}

// emit queues an event for every enabled webhook subscribed to it.
func (s *Service) emit(ctx context.Context, q enqueuer, env webhook.Envelope) error {
	env.OccurredAt = s.now().UTC()
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	n, err := q.EnqueueDeliveries(ctx, sqlc.EnqueueDeliveriesParams{Event: env.Event, Payload: b})
	if err != nil {
		return fmt.Errorf("queue webhook event: %w", err)
	}
	if n > 0 {
		s.nudge()
	}
	return nil
}

// emitLogged queues an event outside a transaction; failures are logged, not
// returned, because the change itself has already been saved.
func (s *Service) emitLogged(ctx context.Context, env webhook.Envelope) {
	if err := s.emit(ctx, s.store, env); err != nil {
		s.log.ErrorContext(ctx, "webhook event not queued", "event", env.Event, "error", err)
	}
}

func (s *Service) nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

type eventProblem struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	URL   string    `json:"url,omitempty"`
}

func problemRef(link func(string) string, id uuid.UUID, title string) eventProblem {
	return eventProblem{ID: id, Title: title, URL: link("/problems/" + id.String())}
}

// turnbackEvent describes a newly recorded turnback. The reporter is never
// included: outbound payloads leave Turnbacks.
func turnbackEvent(ctx context.Context, q *sqlc.Queries, link func(string) string, t sqlc.Turnback, title string, quantity int) webhook.Envelope {
	var team *string
	if t.TeamID != nil {
		if tm, err := q.GetTeam(ctx, *t.TeamID); err == nil {
			team = &tm.Name
		}
	}
	summary := fmt.Sprintf("Turnback: %d min lost to “%s”", t.DurationMinutes, title)
	if quantity > 1 {
		summary = fmt.Sprintf("Turnbacks: %d × %d min lost to “%s”", quantity, t.DurationMinutes, title)
	}
	if team != nil {
		summary += " (" + *team + ")"
	}
	p := problemRef(link, t.ProblemID, title)
	return webhook.Envelope{
		Event: webhook.EventTurnbackRecorded, Summary: summary, URL: p.URL,
		Data: map[string]any{
			"turnback_id": t.ID, "batch_id": t.BatchID, "problem": p, "duration_minutes": t.DurationMinutes,
			"quantity": quantity, "team": team, "source": t.Source, "note": t.Note, "occurred_at": t.OccurredAt,
		},
	}
}

func problemEvent(link func(string) string, p sqlc.Problem, category string) webhook.Envelope {
	ref := problemRef(link, p.ID, p.Title)
	return webhook.Envelope{
		Event: webhook.EventProblemCreated, Summary: fmt.Sprintf("New problem: “%s” (%s)", p.Title, category), URL: ref.URL,
		Data: map[string]any{"problem": ref, "category": category, "alias": p.Alias, "description": p.Description},
	}
}

func remediationEvent(link func(string) string, r sqlc.Remediation, title string) webhook.Envelope {
	ref := problemRef(link, r.ProblemID, title)
	return webhook.Envelope{
		Event:   webhook.EventRemediationRecorded,
		Summary: fmt.Sprintf("Remediation for “%s” on %s: %s", title, r.ImplementedOn.Format(time.DateOnly), firstLine(r.Description)),
		URL:     ref.URL,
		Data: map[string]any{
			"remediation_id": r.ID, "problem": ref, "implemented_on": r.ImplementedOn.Format(time.DateOnly),
			"description": r.Description, "expected_reduction_pct": r.ExpectedReductionPct,
		},
	}
}

func markerEvent(link func(string) string, m sqlc.ChangeMarker, problemTitle *string) webhook.Envelope {
	data := map[string]any{
		"marker_id": m.ID, "title": m.Title, "description": m.Description, "url": m.Url,
		"occurred_at": m.OccurredAt, "problem": nil, "source": m.Source,
	}
	summary := "Change: " + m.Title
	url := link("/dashboard")
	if m.ProblemID != nil && problemTitle != nil {
		ref := problemRef(link, *m.ProblemID, *problemTitle)
		data["problem"] = ref
		summary = fmt.Sprintf("Change to “%s”: %s", *problemTitle, m.Title)
		url = ref.URL
	}
	return webhook.Envelope{Event: webhook.EventChangeMarked, Summary: summary, URL: url, Data: data}
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len([]rune(s)) > 140 {
		s = string([]rune(s)[:139]) + "…"
	}
	return s
}

// ---- Outbound webhook administration ---------------------------------------

// WebhookInput creates or updates an outbound webhook.
type WebhookInput struct {
	Name    string
	URL     string
	Format  string
	Events  []string
	Enabled bool
}

func (in *WebhookInput) validate() error {
	in.Name, in.URL = strings.TrimSpace(in.Name), strings.TrimSpace(in.URL)
	var v domain.Validator
	v.Check(len(in.Name) >= 1 && len(in.Name) <= 100, "name", "must be between 1 and 100 characters")
	if err := webhook.ValidateURL(in.URL); err != nil {
		v.Add("url", err.Error())
	}
	v.Check(slices.Contains(webhook.Formats, in.Format), "format", "must be one of "+strings.Join(webhook.Formats, ", "))
	slices.Sort(in.Events)
	in.Events = slices.Compact(in.Events)
	ok := len(in.Events) > 0
	for _, e := range in.Events {
		ok = ok && slices.Contains(webhook.Events, e)
	}
	v.Check(ok, "events", "choose at least one of "+strings.Join(webhook.Events, ", "))
	return v.Err()
}

// WebhookView is an outbound webhook with its most recent delivery.
type WebhookView struct {
	Webhook sqlc.Webhook
	Last    *sqlc.WebhookDelivery
}

func newSecret(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// ListWebhooks lists outbound webhooks (administrators only).
func (s *Service) ListWebhooks(ctx context.Context, a domain.Actor) ([]WebhookView, error) {
	if err := requireAdmin(a); err != nil {
		return nil, err
	}
	hooks, err := s.store.ListWebhooks(ctx)
	if err != nil {
		return nil, err
	}
	latest, err := s.store.LatestDeliveries(ctx)
	if err != nil {
		return nil, err
	}
	byHook := map[uuid.UUID]*sqlc.WebhookDelivery{}
	for i := range latest {
		byHook[latest[i].WebhookID] = &latest[i]
	}
	out := make([]WebhookView, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, WebhookView{Webhook: h, Last: byHook[h.ID]})
	}
	return out, nil
}

// CreateWebhook adds an outbound webhook (administrators only). The returned
// webhook includes its signing secret.
func (s *Service) CreateWebhook(ctx context.Context, a domain.Actor, in WebhookInput) (sqlc.Webhook, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.Webhook{}, err
	}
	if err := in.validate(); err != nil {
		return sqlc.Webhook{}, err
	}
	secret, err := newSecret("whsec_")
	if err != nil {
		return sqlc.Webhook{}, err
	}
	return s.store.CreateWebhook(ctx, sqlc.CreateWebhookParams{
		Name: in.Name, Url: in.URL, Format: in.Format, Events: in.Events, Secret: secret, Enabled: in.Enabled,
	})
}

// UpdateWebhook changes an outbound webhook (administrators only).
func (s *Service) UpdateWebhook(ctx context.Context, a domain.Actor, id uuid.UUID, in WebhookInput) (sqlc.Webhook, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.Webhook{}, err
	}
	if err := in.validate(); err != nil {
		return sqlc.Webhook{}, err
	}
	w, err := s.store.UpdateWebhook(ctx, sqlc.UpdateWebhookParams{
		ID: id, Name: in.Name, Url: in.URL, Format: in.Format, Events: in.Events, Enabled: in.Enabled,
	})
	return w, notFoundOr(err, "webhook")
}

// RotateWebhookSecret replaces a webhook's signing secret (administrators only).
func (s *Service) RotateWebhookSecret(ctx context.Context, a domain.Actor, id uuid.UUID) (sqlc.Webhook, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.Webhook{}, err
	}
	secret, err := newSecret("whsec_")
	if err != nil {
		return sqlc.Webhook{}, err
	}
	w, err := s.store.RotateWebhookSecret(ctx, sqlc.RotateWebhookSecretParams{ID: id, Secret: secret})
	return w, notFoundOr(err, "webhook")
}

// DeleteWebhook removes an outbound webhook and its delivery history
// (administrators only).
func (s *Service) DeleteWebhook(ctx context.Context, a domain.Actor, id uuid.UUID) error {
	if err := requireAdmin(a); err != nil {
		return err
	}
	n, err := s.store.DeleteWebhook(ctx, id)
	if err == nil && n == 0 {
		return domain.NotFound("webhook")
	}
	return err
}

// TestWebhook queues a test event for one webhook, whatever its
// subscriptions (administrators only).
func (s *Service) TestWebhook(ctx context.Context, a domain.Actor, id uuid.UUID) (sqlc.WebhookDelivery, error) {
	if err := requireAdmin(a); err != nil {
		return sqlc.WebhookDelivery{}, err
	}
	w, err := s.store.GetWebhook(ctx, id)
	if err != nil {
		return sqlc.WebhookDelivery{}, notFoundOr(err, "webhook")
	}
	b, err := json.Marshal(webhook.Envelope{
		Event: webhook.EventTest, OccurredAt: s.now().UTC(), URL: s.link("/"),
		Summary: fmt.Sprintf("Test message from Turnbacks for webhook “%s”.", w.Name),
		Data:    map[string]any{"webhook_id": w.ID},
	})
	if err != nil {
		return sqlc.WebhookDelivery{}, err
	}
	d, err := s.store.EnqueueDelivery(ctx, sqlc.EnqueueDeliveryParams{WebhookID: w.ID, Event: webhook.EventTest, Payload: b})
	if err == nil {
		s.nudge()
	}
	return d, err
}

// ListDeliveries returns a webhook's recent deliveries (administrators only).
func (s *Service) ListDeliveries(ctx context.Context, a domain.Actor, id uuid.UUID) ([]sqlc.WebhookDelivery, error) {
	if err := requireAdmin(a); err != nil {
		return nil, err
	}
	if _, err := s.store.GetWebhook(ctx, id); err != nil {
		return nil, notFoundOr(err, "webhook")
	}
	return s.store.ListDeliveries(ctx, sqlc.ListDeliveriesParams{WebhookID: id, Lim: 50})
}

// ---- Delivery worker --------------------------------------------------------

// deliveryLease is how long a claimed delivery is reserved for one worker,
// and deliveryBatch how many are claimed at once. Deliveries in a batch are
// sent one after another, each taking up to the client timeout, so a worker
// stops sending from a batch once the remaining lease could not cover another
// send; unsent deliveries are claimed again when their lease ends. This keeps
// another replica from sending a delivery this worker is still sending.
const (
	deliveryLease = 2 * time.Minute
	deliveryBatch = 5
)

// DeliverDueWebhooks sends every due delivery once and returns how many were
// attempted. Safe to run concurrently on several replicas.
func (s *Service) DeliverDueWebhooks(ctx context.Context) (int, error) {
	total := 0
	for {
		claimed := time.Now()
		due, err := s.store.ClaimDueDeliveries(ctx, sqlc.ClaimDueDeliveriesParams{LeaseSeconds: deliveryLease.Seconds(), Lim: deliveryBatch})
		if err != nil {
			return total, err
		}
		for _, d := range due {
			if time.Since(claimed)+s.hooks.Timeout+5*time.Second > deliveryLease {
				break
			}
			s.deliver(ctx, d)
			total++
		}
		if len(due) < deliveryBatch || ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

func (s *Service) deliver(ctx context.Context, d sqlc.WebhookDelivery) {
	w, err := s.store.GetWebhook(ctx, d.WebhookID)
	if err != nil {
		s.log.ErrorContext(ctx, "webhook delivery: webhook not loaded", "delivery_id", d.ID, "error", err)
		return
	}
	if !w.Enabled {
		// Disabled after the event was queued: never send it.
		msg := "webhook disabled"
		if err := s.store.MarkDeliveryFailedAttempt(ctx, sqlc.MarkDeliveryFailedAttemptParams{
			ID: d.ID, LastError: &msg, GiveUp: true, NextAttemptAt: s.now(),
		}); err != nil {
			s.log.ErrorContext(ctx, "webhook delivery: status not saved", "delivery_id", d.ID, "error", err)
		}
		return
	}
	res := webhook.Send(ctx, s.hooks, webhook.Delivery{
		ID: d.ID.String(), Event: d.Event, URL: w.Url, Format: w.Format, Secret: w.Secret, Stored: d.Payload,
	}, s.now())
	var code *int32
	if res.StatusCode != 0 {
		c := int32(res.StatusCode) //nolint:gosec // HTTP status codes are small
		code = &c
	}
	if res.OK() {
		if err := s.store.MarkDeliveryDelivered(ctx, sqlc.MarkDeliveryDeliveredParams{ID: d.ID, StatusCode: code}); err != nil {
			s.log.ErrorContext(ctx, "webhook delivery: status not saved", "delivery_id", d.ID, "error", err)
		}
		return
	}
	attempt := int(d.Attempts) + 1
	giveUp := !res.Retry || attempt >= webhook.MaxAttempts
	msg := res.Err.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	s.log.WarnContext(ctx, "webhook delivery failed", "webhook", w.Name, "delivery_id", d.ID, "event", d.Event,
		"attempt", attempt, "error", msg, "giving_up", giveUp)
	if err := s.store.MarkDeliveryFailedAttempt(ctx, sqlc.MarkDeliveryFailedAttemptParams{
		ID: d.ID, StatusCode: code, LastError: &msg, GiveUp: giveUp, NextAttemptAt: s.now().Add(webhook.Backoff(attempt)),
	}); err != nil {
		s.log.ErrorContext(ctx, "webhook delivery: status not saved", "delivery_id", d.ID, "error", err)
	}
}

// WebhookLoop delivers queued webhook events until ctx is cancelled: promptly
// after an event is queued on this replica, and at least every poll interval
// (for retries and events queued by other replicas).
func (s *Service) WebhookLoop(ctx context.Context, poll time.Duration) {
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		if _, err := s.DeliverDueWebhooks(ctx); err != nil && ctx.Err() == nil {
			s.log.ErrorContext(ctx, "webhook delivery loop", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wake:
		}
	}
}
