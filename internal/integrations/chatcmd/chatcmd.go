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

// Package chatcmd implements the chat command shared by the Slack, Mattermost
// and Microsoft Teams integrations:
//
//	<alias> <minutes> [note]            record, e.g. "vpn 10 disconnected again"
//	<alias> <minutes>! [note]           "!" confirms an unusually long duration
//	<alias> <minutes> x<count> [note]   several occurrences, e.g. "vpn 10 x15"
//	list | help
package chatcmd

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"turnbacks/internal/domain"
	"turnbacks/internal/service"
)

// Style adapts replies to a chat system's formatting.
type Style struct {
	// Invocation is how users invoke the command, e.g. "/turnback" or
	// "@Turnbacks".
	Invocation string
	// Bold wraps text in the chat system's bold markup.
	Bold func(string) string
}

// Slack-style ("*bold*") and Markdown ("**bold**") formatting.
var (
	SlackBold    = func(s string) string { return "*" + s + "*" }
	MarkdownBold = func(s string) string { return "**" + s + "**" }
)

// Request is one command invocation.
type Request struct {
	// Reporter is the pseudonymous reporter key, e.g. "slack:T1/U2".
	Reporter string
	// Text is the command text after the invocation.
	Text string
	// ID is the idempotency key for the submission (derive it from something
	// unique to the request so retries are not recorded twice).
	ID uuid.UUID
	// Source is the turnback source, e.g. domain.SourceSlack.
	Source string
}

// Handler runs chat commands against the service.
type Handler struct {
	Svc   *service.Service
	Log   *slog.Logger
	Style Style
}

func (h Handler) usage() string {
	c := "`" + h.Style.Invocation
	return "Usage:\n" +
		"• " + c + " <alias> <minutes> [note]` — e.g. " + c + " vpn 10 disconnected again`\n" +
		"• " + c + " <alias> <minutes> x<count> [note]` — several times, e.g. " + c + " vpn 10 x15`\n" +
		"• " + c + " list` — show problem aliases"
}

var minutesRe = regexp.MustCompile(`^(\d{1,4})(m|min|mins|h|hr|hrs)?(!)?$`)

var quantityRe = regexp.MustCompile(`^(?:x(\d{1,3})|(\d{1,3})x)(!)?$`)

// ParseQuantity parses "x15" or "15x", optionally suffixed with "!".
func ParseQuantity(s string) (n int, confirmed bool, ok bool) {
	m := quantityRe.FindStringSubmatch(strings.ToLower(s))
	if m == nil {
		return 0, false, false
	}
	d := m[1]
	if d == "" {
		d = m[2]
	}
	n, _ = strconv.Atoi(d)
	return n, m[3] == "!", n >= 1
}

// ParseDuration parses "10", "10m", "2h", optionally suffixed with "!".
func ParseDuration(s string) (minutes int, confirmed bool, ok bool) {
	m := minutesRe.FindStringSubmatch(strings.ToLower(s))
	if m == nil {
		return 0, false, false
	}
	n, _ := strconv.Atoi(m[1])
	if strings.HasPrefix(m[2], "h") {
		n *= 60
	}
	return n, m[3] == "!", true
}

// Handle runs a command and returns the reply text.
func (h Handler) Handle(ctx context.Context, req Request) string {
	fields := strings.Fields(req.Text)
	if len(fields) == 0 || strings.EqualFold(fields[0], "help") {
		return h.usage()
	}
	if strings.EqualFold(fields[0], "list") {
		return h.list(ctx)
	}
	if len(fields) < 2 {
		return h.usage()
	}
	minutes, confirmed, ok := ParseDuration(fields[1])
	if !ok {
		return fmt.Sprintf("I couldn't read `%s` as a number of minutes.\n%s", fields[1], h.usage())
	}
	rest := fields[2:]
	quantity := 1
	if len(rest) > 0 {
		if q, qConfirmed, ok := ParseQuantity(rest[0]); ok {
			quantity, confirmed, rest = q, confirmed || qConfirmed, rest[1:]
		}
	}
	note := strings.TrimSpace(strings.Join(rest, " "))

	problem, err := h.Svc.ProblemByAlias(ctx, fields[0])
	if err != nil {
		return fmt.Sprintf("No problem has the alias `%s`. Try `%s list`.", fields[0], h.Style.Invocation)
	}
	var notePtr *string
	if note != "" {
		notePtr = &note
	}
	res, err := h.Svc.SubmitTurnback(ctx, domain.Actor{Reporter: req.Reporter}, service.TurnbackInput{
		ID: req.ID, ProblemID: problem.ID, DurationMinutes: minutes, DurationEstimated: true, Note: notePtr,
		Source: req.Source, ConfirmLargeDuration: confirmed, Quantity: quantity,
	})
	if err != nil {
		if de, ok := domain.AsError(err); ok {
			if de.Kind == domain.KindConfirmationRequired {
				return de.Message + " Add `!` to the number to confirm, e.g. `180!` or `x30!`."
			}
			if de.Kind != domain.KindInternal {
				return "Not recorded: " + de.Message
			}
		}
		h.Log.ErrorContext(ctx, "chat command submission failed", "source", req.Source, "error", err)
		return "Sorry, something went wrong recording that turnback."
	}
	if quantity > 1 {
		return fmt.Sprintf("✅ Recorded %d × %d min against %s. Thanks — it all adds up.", quantity, minutes, h.Style.Bold(res.ProblemTitle))
	}
	return fmt.Sprintf("✅ Recorded %d min against %s. Thanks — it all adds up.", minutes, h.Style.Bold(res.ProblemTitle))
}

func (h Handler) list(ctx context.Context) string {
	items, _, err := h.Svc.ListProblems(ctx, domain.Actor{}, service.ProblemListParams{Page: service.Page{Limit: 100}})
	if err != nil {
		return "Sorry, something went wrong."
	}
	var b strings.Builder
	b.WriteString("Problem aliases:\n")
	n := 0
	for _, it := range items {
		if it.Problem.Alias != nil {
			fmt.Fprintf(&b, "• `%s` — %s\n", *it.Problem.Alias, it.Problem.Title)
			n++
		}
	}
	if n == 0 {
		return "No problems have aliases yet. An administrator can add them in Turnbacks."
	}
	return b.String()
}
