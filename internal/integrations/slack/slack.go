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

// Package slack implements the /turnback Slack slash command.
//
//	/turnback vpn 10 disconnected again     record 10 minutes against alias "vpn"
//	/turnback vpn 180! long outage           "!" confirms an unusually long duration
//	/turnback vpn 10 x15 all day             15 occurrences of 10 minutes
//	/turnback list | help
//
// Every request's signature is verified with the Slack signing secret,
// requests older than five minutes are rejected, and each signature may only
// be used once (replay protection). The reporter is recorded as
// "slack:<team>/<user>" so Slack reports count towards people affected; it is
// visible to administrators only.
package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"turnbacks/internal/domain"
	"turnbacks/internal/integrations/chatcmd"
	"turnbacks/internal/service"
	"turnbacks/internal/store"
)

const (
	maxSkew = 5 * time.Minute
	maxBody = 64 << 10
)

// Namespace for deterministic turnback IDs derived from request signatures.
var idNamespace = uuid.MustParse("6f1c2d0e-8a4b-4f43-9a51-4e0bd8f4c2a7")

// Handler serves Slack slash commands.
type Handler struct {
	secret []byte
	store  *store.Store
	svc    *service.Service
	log    *slog.Logger
	now    func() time.Time
}

// New constructs the Slack handler.
func New(secret string, st *store.Store, svc *service.Service, log *slog.Logger) *Handler {
	return &Handler{secret: []byte(secret), store: st, svc: svc, log: log, now: time.Now}
}

// Verify checks the Slack v0 signature and timestamp freshness.
func Verify(secret []byte, ts, sig string, body []byte, now time.Time) error {
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return errors.New("invalid timestamp")
	}
	if d := now.Sub(time.Unix(sec, 0)); d > maxSkew || d < -maxSkew {
		return errors.New("stale timestamp")
	}
	if !strings.HasPrefix(sig, "v0=") {
		return errors.New("unsupported signature version")
	}
	got, err := hex.DecodeString(strings.TrimPrefix(sig, "v0="))
	if err != nil {
		return errors.New("malformed signature")
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "v0:%s:", ts)
	mac.Write(body)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return errors.New("signature mismatch")
	}
	return nil
}

type response struct {
	ResponseType string `json:"response_type"`
	Text         string `json:"text"`
}

func reply(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response{ResponseType: "ephemeral", Text: text})
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	sig := r.Header.Get("X-Slack-Signature")
	if err := Verify(h.secret, r.Header.Get("X-Slack-Request-Timestamp"), sig, body, h.now()); err != nil {
		h.log.WarnContext(r.Context(), "slack request rejected", "reason", err.Error())
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	sigHash := sha256.Sum256([]byte(sig))
	n, err := h.store.RecordSlackRequest(r.Context(), sigHash[:])
	if err != nil {
		h.log.ErrorContext(r.Context(), "slack replay log failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if n == 0 {
		h.log.WarnContext(r.Context(), "slack request rejected", "reason", "replayed signature")
		http.Error(w, "replayed request", http.StatusUnauthorized)
		return
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	reply(w, h.handle(r.Context(), form.Get("team_id"), form.Get("user_id"), form.Get("text"), sig))
}

// ParseDuration and ParseQuantity are kept here for compatibility.
var (
	ParseDuration = chatcmd.ParseDuration
	ParseQuantity = chatcmd.ParseQuantity
)

func (h *Handler) handle(ctx context.Context, teamID, userID, text, sig string) string {
	return chatcmd.Handler{Svc: h.svc, Log: h.log, Style: chatcmd.Style{Invocation: "/turnback", Bold: chatcmd.SlackBold}}.
		Handle(ctx, chatcmd.Request{
			Reporter: "slack:" + teamID + "/" + userID, Text: text,
			ID: uuid.NewSHA1(idNamespace, []byte(sig)), Source: domain.SourceSlack,
		})
}
