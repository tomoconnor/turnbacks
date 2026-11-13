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
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// cacheTTL bounds how stale completion data may be before a refresh.
const cacheTTL = time.Hour

// refreshCache fetches problems and teams for completion and offline use.
func (a *App) refreshCache(ctx context.Context) (Cache, error) {
	var probs struct{ Items []Problem }
	if err := a.client.do(ctx, "GET", "/problems", url.Values{"limit": {"200"}}, nil, &probs); err != nil {
		return loadCache(), err
	}
	var teams struct{ Items []Team }
	if err := a.client.do(ctx, "GET", "/teams", nil, nil, &teams); err != nil {
		return loadCache(), err
	}
	var cats struct{ Items []Category }
	if err := a.client.do(ctx, "GET", "/categories", nil, nil, &cats); err != nil {
		return loadCache(), err
	}
	c := Cache{FetchedAt: time.Now()}
	for _, k := range cats.Items {
		c.Categories = append(c.Categories, CachedRecord{ID: k.ID, Name: k.Name})
	}
	for _, p := range probs.Items {
		r := CachedRecord{ID: p.ID, Name: p.Title}
		if p.Alias != nil {
			r.Alias = *p.Alias
		}
		c.Problems = append(c.Problems, r)
	}
	for _, t := range teams.Items {
		c.Teams = append(c.Teams, CachedRecord{ID: t.ID, Name: t.Name})
	}
	_ = saveCache(c)
	return c, nil
}

// resolveProblem finds a problem by alias, ID or title words.
func (a *App) resolveProblem(ctx context.Context, ref string) (CachedRecord, error) {
	ref = strings.TrimSpace(ref)
	c, err := a.refreshCache(ctx)
	offline := errors.Is(err, errUnreachable)
	if err != nil && !offline {
		return CachedRecord{}, err
	}
	lower := strings.ToLower(ref)
	var matches []CachedRecord
	for _, p := range c.Problems {
		if p.Alias == lower || p.ID == ref {
			return p, nil
		}
		if strings.Contains(strings.ToLower(p.Name), lower) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 && !offline {
		// Fall back to the server's fuzzy search.
		var res struct{ Items []Problem }
		if err := a.client.do(ctx, "GET", "/problems", url.Values{"q": {ref}, "limit": {"5"}}, nil, &res); err == nil {
			for _, p := range res.Items {
				matches = append(matches, CachedRecord{ID: p.ID, Name: p.Title})
			}
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		if offline && len(c.Problems) == 0 {
			return CachedRecord{}, fmt.Errorf("the server is unreachable and no problems are cached yet")
		}
		return CachedRecord{}, fmt.Errorf("no problem matches %q (see tb problems, or add one with tb new)", ref)
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "%q matches several problems — use an alias or more words:", ref)
		for i, m := range matches {
			if i == 8 {
				b.WriteString("\n  …")
				break
			}
			if m.Alias != "" {
				fmt.Fprintf(&b, "\n  %-12s %s", m.Alias, m.Name)
			} else {
				fmt.Fprintf(&b, "\n  %-12s %s", "", m.Name)
			}
		}
		return CachedRecord{}, errors.New(b.String())
	}
}

func (a *App) resolveTeam(ctx context.Context, name string) (string, error) {
	c, err := a.refreshCache(ctx)
	if err != nil && !errors.Is(err, errUnreachable) {
		return "", err
	}
	for _, t := range c.Teams {
		if strings.EqualFold(t.Name, name) || t.ID == name {
			return t.ID, nil
		}
	}
	return "", fmt.Errorf("no team named %q (see tb teams)", name)
}

func (a *App) record(ctx context.Context, args []string) error {
	f, err := parseRecordFlags(args)
	if err != nil {
		return err
	}
	if len(f.positional) < 2 {
		return errors.New("usage: tb <problem> <minutes> [note…]  (e.g. tb vpn 10 disconnected again)")
	}
	minutes, err := parseMinutes(f.positional[1])
	if err != nil {
		return err
	}
	if f.times < 1 || f.times > 100 {
		return errors.New("--times must be between 1 and 100")
	}
	note := f.note
	if extra := strings.Join(f.positional[2:], " "); extra != "" {
		note = strings.TrimSpace(note + " " + extra)
	}
	when, err := parseWhen(f.ago, f.at, time.Now())
	if err != nil {
		return err
	}
	problem, err := a.resolveProblem(ctx, f.positional[0])
	if err != nil {
		return err
	}
	team := a.cfg.Team
	if f.team != "" {
		if team, err = a.resolveTeam(ctx, f.team); err != nil {
			return err
		}
	}
	sub := Submission{
		ID: uuidv4(), ProblemID: problem.ID, DurationMinutes: minutes, Quantity: f.times,
		OccurredAt: when.UTC().Format(time.RFC3339), Source: "cli", ConfirmLargeDuration: f.yes,
	}
	if team != "" {
		sub.TeamID = &team
	}
	if note != "" {
		sub.Note = &note
	}
	return a.submit(ctx, sub, problem.Name, f.exact)
}

func describe(minutes, quantity int) string {
	if quantity > 1 {
		return fmt.Sprintf("%d × %d min", quantity, minutes)
	}
	return fmt.Sprintf("%d min", minutes)
}

type submitBody struct {
	Submission
	DurationEstimated bool `json:"duration_estimated"`
}

func (a *App) submit(ctx context.Context, sub Submission, title string, exact bool) error {
	var res SubmitResult
	err := a.client.do(ctx, "POST", "/turnbacks", nil, submitBody{Submission: sub, DurationEstimated: !exact}, &res)
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.Code == "duration_confirmation_required" || apiErr.Code == "quantity_confirmation_required") {
		if !a.ask(apiErr.Message + " Record it anyway?") {
			return fmt.Errorf("%s Re-run with --yes to confirm", apiErr.Message)
		}
		sub.ConfirmLargeDuration = true
		return a.submit(ctx, sub, title, exact)
	}
	if errors.Is(err, errUnreachable) {
		q, _ := loadQueue()
		q = append(q, queuedSubmission{Submission: sub, ProblemTitle: title})
		if serr := saveQueue(q); serr != nil {
			return fmt.Errorf("server unreachable and the report could not be saved locally: %w", serr)
		}
		a.printf("⏸  Saved locally: %s · %s (server unreachable). It will be sent next time; or run tb sync.\n", describe(sub.DurationMinutes, sub.Quantity), title)
		_ = saveLast(LastReport{ID: sub.ID, ProblemID: sub.ProblemID, Problem: title, Minutes: sub.DurationMinutes, Quantity: sub.Quantity, At: time.Now()})
		return nil
	}
	if err != nil {
		return err
	}
	a.printf("✓ Recorded %s · %s\n", describe(sub.DurationMinutes, res.Quantity), res.Turnback.ProblemTitle)
	return saveLast(LastReport{
		ID: sub.ID, ProblemID: sub.ProblemID, Problem: res.Turnback.ProblemTitle, Minutes: sub.DurationMinutes,
		Quantity: res.Quantity, UndoToken: res.UndoToken, At: time.Now(),
	})
}

func (a *App) again(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("again", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	times := fs.Int("n", 1, "")
	fs.IntVar(times, "times", 1, "")
	yes := fs.Bool("yes", false, "")
	fs.BoolVar(yes, "y", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	last, err := loadLast()
	if err != nil {
		return err
	}
	if last == nil {
		return errors.New("nothing to repeat yet — record something first")
	}
	sub := Submission{
		ID: uuidv4(), ProblemID: last.ProblemID, DurationMinutes: last.Minutes, Quantity: *times,
		OccurredAt: time.Now().UTC().Format(time.RFC3339), Source: "cli", ConfirmLargeDuration: *yes,
	}
	if a.cfg.Team != "" {
		t := a.cfg.Team
		sub.TeamID = &t
	}
	return a.submit(ctx, sub, last.Problem, false)
}

func (a *App) undo(ctx context.Context) error {
	last, err := loadLast()
	if err != nil {
		return err
	}
	if last == nil {
		return errors.New("nothing to undo")
	}
	if last.Undone {
		return fmt.Errorf("already undone: %s · %s", describe(last.Minutes, last.Quantity), last.Problem)
	}
	markUndone := func() error {
		last.Undone, last.UndoToken = true, ""
		return saveLast(*last)
	}
	// Still queued locally? Then simply drop it.
	q, _ := loadQueue()
	for i, s := range q {
		if s.ID == last.ID {
			if err := saveQueue(append(q[:i], q[i+1:]...)); err != nil {
				return err
			}
			a.printf("↶ Removed queued report: %s · %s\n", describe(last.Minutes, last.Quantity), last.Problem)
			return markUndone()
		}
	}
	if last.UndoToken == "" {
		return errors.New("the last report can't be undone from here; ask an administrator to correct it")
	}
	vals := url.Values{"undo_token": {last.UndoToken}}
	if last.Quantity > 1 {
		vals.Set("batch", "true")
	}
	if err := a.client.do(ctx, "DELETE", "/turnbacks/"+last.ID, vals, nil, nil); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Code == "undo_not_allowed" {
			return errors.New("too late to undo (15 minutes have passed); ask an administrator to correct it")
		}
		return err
	}
	a.printf("↶ Undone: %s · %s\n", describe(last.Minutes, last.Quantity), last.Problem)
	return markUndone()
}

// flushQueue sends reports queued while offline. Quiet mode prints only
// successes; it stops at the first network failure.
func (a *App) flushQueue(ctx context.Context, quiet bool) (sent, left int) {
	q, err := loadQueue()
	if err != nil || len(q) == 0 {
		return 0, 0
	}
	var remaining []queuedSubmission
	for i, s := range q {
		var res SubmitResult
		err := a.client.do(ctx, "POST", "/turnbacks", nil, submitBody{Submission: s.Submission, DurationEstimated: true}, &res)
		if errors.Is(err, errUnreachable) {
			remaining = append(remaining, q[i:]...)
			break
		}
		if err != nil {
			_, _ = fmt.Fprintf(a.errOut, "tb: queued report rejected and kept in the queue (%s · %s): %v\n", describe(s.DurationMinutes, s.Quantity), s.ProblemTitle, err)
			remaining = append(remaining, s)
			continue
		}
		sent++
		a.printf("✓ Sent queued report: %s · %s\n", describe(s.DurationMinutes, s.Quantity), s.ProblemTitle)
		if last, _ := loadLast(); last != nil && last.ID == s.ID {
			last.UndoToken = res.UndoToken
			_ = saveLast(*last)
		}
	}
	_ = saveQueue(remaining)
	if !quiet && len(remaining) > 0 {
		a.printf("%d report(s) still queued.\n", len(remaining))
	}
	return sent, len(remaining)
}

func (a *App) sync(ctx context.Context) error {
	q, _ := loadQueue()
	if len(q) == 0 {
		a.printf("Nothing queued.\n")
		return nil
	}
	_, left := a.flushQueue(ctx, false)
	if left > 0 {
		return fmt.Errorf("%d report(s) could not be sent yet", left)
	}
	return nil
}

func (a *App) problems(ctx context.Context, args []string) error {
	vals := url.Values{"limit": {"100"}}
	if len(args) > 0 {
		vals.Set("q", strings.Join(args, " "))
	}
	var res struct{ Items []Problem }
	if err := a.client.do(ctx, "GET", "/problems", vals, nil, &res); err != nil {
		return err
	}
	if len(res.Items) == 0 {
		a.printf("No problems found.\n")
		return nil
	}
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ALIAS\tPROBLEM\tCATEGORY\t30 DAYS")
	for _, p := range res.Items {
		alias := "-"
		if p.Alias != nil {
			alias = *p.Alias
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", alias, p.Title, p.CategoryName, p.RecentCount)
	}
	_ = tw.Flush()
	_, _ = a.refreshCache(ctx)
	return nil
}

func (a *App) newProblem(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	alias := fs.String("alias", "", "")
	category := fs.String("category", "Other", "")
	var title []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		args = fs.Args()
		if len(args) > 0 {
			title, args = append(title, args[0]), args[1:]
		}
	}
	if len(title) == 0 {
		return errors.New(`usage: tb new "Problem title" [--alias vpn] [--category Networking]`)
	}
	var cats struct{ Items []Category }
	if err := a.client.do(ctx, "GET", "/categories", nil, nil, &cats); err != nil {
		return err
	}
	catID := ""
	var names []string
	for _, c := range cats.Items {
		names = append(names, c.Name)
		if strings.EqualFold(c.Name, *category) {
			catID = c.ID
		}
	}
	if catID == "" {
		return fmt.Errorf("unknown category %q (one of: %s)", *category, strings.Join(names, ", "))
	}
	body := map[string]any{"title": strings.Join(title, " "), "category_id": catID}
	if *alias != "" {
		body["alias"] = strings.ToLower(*alias)
	}
	var p Problem
	if err := a.client.do(ctx, "POST", "/problems", nil, body, &p); err != nil {
		return err
	}
	_, _ = a.refreshCache(ctx)
	if p.Alias != nil {
		a.printf("✓ Added %q (alias %s). Record with: tb %s 10\n", p.Title, *p.Alias, *p.Alias)
	} else {
		a.printf("✓ Added %q.\n", p.Title)
	}
	return nil
}

func (a *App) teams(ctx context.Context) error {
	c, err := a.refreshCache(ctx)
	if err != nil {
		return err
	}
	for _, t := range c.Teams {
		marker := "  "
		if t.ID == a.cfg.Team {
			marker = "* "
		}
		a.printf("%s%s\n", marker, t.Name)
	}
	if a.cfg.Team == "" {
		a.printf("\nSet yours with: tb config set team \"<name>\"\n")
	}
	return nil
}

var periods = map[string]string{"7d": "last_7_days", "30d": "last_30_days", "90d": "last_90_days", "year": "this_year"}

func (a *App) stats(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	period := fs.String("period", "7d", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, ok := periods[*period]
	if !ok {
		return fmt.Errorf("--period must be one of 7d, 30d, 90d, year")
	}
	var st Settings
	if err := a.client.do(ctx, "GET", "/settings", nil, nil, &st); err != nil {
		return err
	}
	var sum Summary
	if err := a.client.do(ctx, "GET", "/analytics/summary", url.Values{"period": {p}}, nil, &sum); err != nil {
		return err
	}
	var rank struct{ Items []RankedProblem }
	if err := a.client.do(ctx, "GET", "/analytics/problems", url.Values{"period": {p}, "sort": {"cost"}, "limit": {"5"}}, nil, &rank); err != nil {
		return err
	}
	c := sum.Current
	money := func(v int64) string { return formatMoney(v, st.Currency, st.CurrencyExponent) }
	a.printf("%s · %s – %s (%s)\n\n", st.OrgName, sum.Period.From, sum.Period.To, sum.Period.Timezone)
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintf(tw, "Turnbacks\t%d\t%s\t\n", c.Turnbacks, change(sum.Change.TurnbacksBP))
	_, _ = fmt.Fprintf(tw, "Time lost\t%.1f h\t%s\t\n", float64(c.Minutes)/60, change(sum.Change.MinutesBP))
	_, _ = fmt.Fprintf(tw, "Est. cost\t%s\t%s\t\n", money(c.CostMinor), change(sum.Change.CostBP))
	_, _ = fmt.Fprintf(tw, "People affected\t%d\t\t\n", c.DistinctReporters)
	_, _ = fmt.Fprintf(tw, "Avg per turnback\t%s min\t\t\n", c.AvgMinutes)
	_ = tw.Flush()
	if len(rank.Items) > 0 {
		a.printf("\nTop problems by estimated cost:\n")
		tw = tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
		for _, r := range rank.Items {
			_, _ = fmt.Fprintf(tw, "  %s\t%d×\t%.1f h\t%s\n", r.Title, r.Turnbacks, float64(r.Minutes)/60, money(r.CostMinor))
		}
		_ = tw.Flush()
	}
	a.printf("\nEstimated cost is reported time × hourly cost; it is not cash spent.\n")
	return nil
}

func change(bp *int64) string {
	if bp == nil {
		return ""
	}
	return fmt.Sprintf("%+.0f%% vs previous", float64(*bp)/100)
}

func formatMoney(minor int64, currency string, exp int) string {
	sym := map[string]string{"GBP": "£", "USD": "$", "EUR": "€"}[currency]
	if sym == "" {
		sym = currency + " "
	}
	major := minor
	for range exp {
		major /= 10
	}
	s := fmt.Sprintf("%d", major)
	// thousands separators
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return sym + b.String()
}

func cmdConfig(args []string, out io.Writer) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "show" {
		team := cfg.Team
		if team != "" {
			for _, t := range loadCache().Teams {
				if t.ID == team {
					team = t.Name
				}
			}
		}
		_, _ = fmt.Fprintf(out, "url:  %s\nteam: %s\nfile: %s/config.json\n", orDash(cfg.URL), orDash(team), configDir())
		return nil
	}
	switch {
	case args[0] == "set" && len(args) == 3 && args[1] == "url":
		u, err := url.Parse(args[2])
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("invalid URL %q", args[2])
		}
		cfg.URL = strings.TrimRight(args[2], "/")
		if err := saveConfig(cfg); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "Server set to %s\n", cfg.URL)
		return nil
	case args[0] == "set" && len(args) >= 3 && args[1] == "team":
		if cfg.URL == "" {
			return errors.New(`set the server first: tb config set url https://…`)
		}
		a := &App{cfg: cfg, client: newClient(cfg.URL, 10*time.Second), out: out}
		id, err := a.resolveTeam(context.Background(), strings.Join(args[2:], " "))
		if err != nil {
			return err
		}
		cfg.Team = id
		if err := saveConfig(cfg); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "Team set to %s\n", strings.Join(args[2:], " "))
		return nil
	case args[0] == "unset" && len(args) == 2 && args[1] == "team":
		cfg.Team = ""
		return saveConfig(cfg)
	}
	return errors.New("usage: tb config [show | set url URL | set team NAME | unset team]")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func cmdCompletion(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: tb completion bash|zsh")
	}
	switch args[0] {
	case "bash":
		_, _ = io.WriteString(out, bashCompletion)
	case "zsh":
		_, _ = io.WriteString(out, zshCompletion)
	default:
		return fmt.Errorf("unsupported shell %q (bash or zsh)", args[0])
	}
	return nil
}

// cmdComplete prints dynamic completion candidates (used by the scripts).
// It refreshes the cache at most hourly, with a short timeout, so completion
// stays fast and works offline.
func cmdComplete(args []string, out io.Writer) {
	if len(args) == 0 {
		return
	}
	c := loadCache()
	if time.Since(c.FetchedAt) > cacheTTL {
		if cfg, err := loadConfig(); err == nil && cfg.URL != "" {
			a := &App{cfg: cfg, client: newClient(cfg.URL, 1500*time.Millisecond), out: io.Discard}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if fresh, err := a.refreshCache(ctx); err == nil {
				c = fresh
			}
			cancel()
		}
	}
	switch args[0] {
	case "problems": // "alias<TAB>title"
		var lines []string
		for _, p := range c.Problems {
			if p.Alias != "" {
				lines = append(lines, p.Alias+"\t"+p.Name)
			}
		}
		sort.Strings(lines)
		for _, l := range lines {
			_, _ = fmt.Fprintln(out, l)
		}
	case "teams":
		for _, t := range c.Teams {
			_, _ = fmt.Fprintln(out, t.Name)
		}
	case "categories":
		for _, k := range c.Categories {
			_, _ = fmt.Fprintln(out, k.Name)
		}
	}
}
