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

// Command tb records and inspects Turnbacks from the terminal.
//
//	tb vpn 10 disconnected again        record 10 minutes against the problem aliased "vpn"
//	tb record vpn 10 -n 15 --ago 2h     15 occurrences of 10 minutes, two hours ago
//	tb again                            repeat the last report
//	tb undo                             undo the last report (within 15 minutes)
//	tb problems [search]                list problems and aliases
//	tb stats [--period 7d|30d|90d|year] headline numbers
//	tb completion bash|zsh              shell completion script
//
// Reports made while the server is unreachable are queued locally and sent
// automatically next time (or with `tb sync`). Each carries a fixed UUID, so a
// retried report is never recorded twice.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

//go:embed completions/tb.bash
var bashCompletion string

//go:embed completions/_tb
var zshCompletion string

const usage = `tb — record the friction that gets in your way (Turnbacks CLI)

Usage:
  tb <problem> <minutes> [note…]          record a turnback (shorthand for "tb record")
  tb record <problem> <minutes> [note…] [flags]
      -n, --times N     it happened N times (each N × minutes)
      --note TEXT       note (or put words after the minutes)
      --ago DURATION    it happened this long ago (e.g. 30m, 2h)
      --at TIME         it happened at HH:MM today or an RFC 3339 time
      --team NAME       your team (default: tb config set team …)
      --exact           the duration is exact, not an estimate
      -y, --yes         confirm unusually long durations or large counts
  tb again [-n N]                         repeat your last report
  tb undo                                 undo your last report (within 15 minutes)
  tb problems [search]                    list problems and their aliases
  tb new <title> [--alias A] [--category NAME]   add a problem
  tb stats [--period 7d|30d|90d|year]     headline numbers and top problems
  tb teams                                list teams
  tb sync                                 send reports queued while offline
  tb config [show | set url URL | set team NAME | unset team]
  tb completion bash|zsh                  print a shell completion script
  tb version

<problem> is an alias (e.g. vpn), a problem ID, or words from the title.
<minutes> accepts 10, 10m, 1h or 1h30m.
The server is set with "tb config set url https://turnbacks.example.com" or TURNBACKS_URL.
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tb:", err)
		os.Exit(1)
	}
}

var commands = map[string]bool{
	"record": true, "again": true, "undo": true, "problems": true, "new": true, "stats": true,
	"teams": true, "sync": true, "config": true, "completion": true, "version": true, "help": true, "__complete": true,
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	if !commands[cmd] {
		if strings.HasPrefix(cmd, "-") {
			if cmd == "-h" || cmd == "--help" {
				_, _ = fmt.Fprint(stdout, usage)
				return nil
			}
			return fmt.Errorf("unknown flag %s (see tb help)", cmd)
		}
		cmd, rest = "record", args // tb vpn 10 …
	}
	switch cmd {
	case "help":
		_, _ = fmt.Fprint(stdout, usage)
		return nil
	case "version":
		_, _ = fmt.Fprintln(stdout, version)
		return nil
	case "completion":
		return cmdCompletion(rest, stdout)
	case "config":
		return cmdConfig(rest, stdout)
	case "__complete":
		cmdComplete(rest, stdout)
		return nil
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.URL == "" {
		return errors.New(`no server configured: run "tb config set url https://turnbacks.example.com" (or set TURNBACKS_URL)`)
	}
	ctx := context.Background()
	app := &App{cfg: cfg, client: newClient(cfg.URL, 10*time.Second), out: stdout, errOut: stderr}
	if cmd != "sync" {
		app.flushQueue(ctx, true)
	}
	switch cmd {
	case "record":
		return app.record(ctx, rest)
	case "again":
		return app.again(ctx, rest)
	case "undo":
		return app.undo(ctx)
	case "problems":
		return app.problems(ctx, rest)
	case "new":
		return app.newProblem(ctx, rest)
	case "stats":
		return app.stats(ctx, rest)
	case "teams":
		return app.teams(ctx)
	case "sync":
		return app.sync(ctx)
	}
	return fmt.Errorf("unknown command %q", cmd)
}

// App holds per-invocation state.
type App struct {
	cfg    Config
	client *Client
	out    io.Writer
	errOut io.Writer
	// confirm asks the user a yes/no question; replaced in tests.
	confirm func(prompt string) bool
}

func (a *App) printf(format string, args ...any) { _, _ = fmt.Fprintf(a.out, format, args...) }

func (a *App) ask(prompt string) bool {
	if a.confirm != nil {
		return a.confirm(prompt)
	}
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false // not interactive: require --yes
	}
	_, _ = fmt.Fprintf(a.errOut, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func uuidv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

var minutesRe = regexp.MustCompile(`^(\d{1,4})(m|min|mins)?$`)

// parseMinutes accepts 10, 10m, 1h, 1h30m.
func parseMinutes(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if m := minutesRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n < 1 {
			return 0, fmt.Errorf("duration must be at least 1 minute")
		}
		return n, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Minute {
		return 0, fmt.Errorf("can't read %q as a duration (try 10, 10m, 1h or 1h30m)", s)
	}
	return int(d.Round(time.Minute) / time.Minute), nil
}

// parseWhen resolves --ago / --at into an occurrence time.
func parseWhen(ago, at string, now time.Time) (time.Time, error) {
	switch {
	case ago != "" && at != "":
		return time.Time{}, errors.New("use either --ago or --at, not both")
	case ago != "":
		d, err := time.ParseDuration(ago)
		if err != nil || d < 0 {
			return time.Time{}, fmt.Errorf("can't read --ago %q (try 30m or 2h)", ago)
		}
		return now.Add(-d), nil
	case at != "":
		if t, err := time.Parse(time.RFC3339, at); err == nil {
			return t, nil
		}
		if t, err := time.ParseInLocation("15:04", at, now.Location()); err == nil {
			y, m, d := now.Date()
			return time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, now.Location()), nil
		}
		return time.Time{}, fmt.Errorf("can't read --at %q (use HH:MM or an RFC 3339 time)", at)
	}
	return now, nil
}

// recordFlags parses flags that may appear anywhere among positional args.
type recordFlags struct {
	times               int
	note, ago, at, team string
	exact, yes          bool
	positional          []string
}

func parseRecordFlags(args []string) (recordFlags, error) {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var f recordFlags
	fs.IntVar(&f.times, "n", 1, "")
	fs.IntVar(&f.times, "times", 1, "")
	fs.StringVar(&f.note, "note", "", "")
	fs.StringVar(&f.ago, "ago", "", "")
	fs.StringVar(&f.at, "at", "", "")
	fs.StringVar(&f.team, "team", "", "")
	fs.BoolVar(&f.exact, "exact", false, "")
	fs.BoolVar(&f.yes, "y", false, "")
	fs.BoolVar(&f.yes, "yes", false, "")
	// Interleaved flags: parse, collect one positional, repeat.
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return f, err
		}
		args = fs.Args()
		if len(args) > 0 {
			f.positional = append(f.positional, args[0])
			args = args[1:]
		}
	}
	return f, nil
}
