# Command-line client: `tb`

`tb` records turnbacks from the terminal: `tb vpn 10` and you're done. It is a single static binary
with no dependencies, and it keeps working when the server (or the VPN) is down by queueing reports
locally.

## Install

```sh
make install-cli        # builds tb, installs to ~/.local/bin, plus bash and zsh completions
# or build only:  make cli   (bin/tb)    cross-compile:  make cli-all   (bin/tb-<os>-<arch>)
```

Then point it at your server and (optionally) choose your team:

```sh
tb config set url https://turnbacks.example.com
tb config set team "Platform Engineering"
```

`TURNBACKS_URL` overrides the configured URL.

### Shell completion

```sh
# bash: one of
tb completion bash > ~/.local/share/bash-completion/completions/tb
echo 'eval "$(tb completion bash)"' >> ~/.bashrc     # works with macOS's bash 3.2

# zsh: one of
tb completion zsh > "${fpath[1]}/_tb"                 # then restart zsh
echo 'eval "$(tb completion zsh)"' >> ~/.zshrc        # after compinit
```

Completion covers commands, problem aliases (with titles in zsh), duration presets, flags, team and
category names, and `--period` values. Alias and team data comes from a local cache that is refreshed
at most hourly with a short timeout, so completion stays instant and works offline.

## Usage

```text
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
tb new <title> [--alias A] [--category NAME]
tb stats [--period 7d|30d|90d|year]     headline numbers and top problems
tb teams                                list teams (* marks yours)
tb sync                                 send reports queued while offline
tb config [show | set url URL | set team NAME | unset team]
tb completion bash|zsh
```

`<problem>` is an alias (`vpn`), a problem ID, or words from the title (`"CI runner"`). If the words
match more than one problem, `tb` lists the candidates. `<minutes>` accepts `10`, `10m`, `1h` or `1h30m`.

```console
$ tb vpn 10 dropped during deploy
✓ Recorded 10 min · Corporate VPN disconnects
$ tb vpn 10 -n 15
✓ Recorded 15 × 10 min · Corporate VPN disconnects
$ tb undo
↶ Undone: 15 × 10 min · Corporate VPN disconnects
$ tb devenv 180
tb: 180 minutes is unusually long (over 120). … Re-run with --yes to confirm
```

In an interactive terminal, `tb` asks before sending an unusually large entry. In scripts, pass `--yes`.

## Offline behaviour

If the server can't be reached (network error or a 5xx response), the report is saved to
`~/.local/state/turnbacks/queue.json` with its client-generated UUID and original occurrence time:

```console
$ tb ci 5
⏸  Saved locally: 5 min · CI runner intermittently unavailable (server unreachable). It will be sent next time; or run tb sync.
```

Every later `tb` command first sends anything queued; `tb sync` does it explicitly. Because the UUID
is the server's idempotency key, a report whose response was lost is never recorded twice. Reports the
server rejects (e.g. validation) stay in the queue, with the reason printed, rather than being dropped.
`tb undo` on a still-queued report simply removes it from the queue.

Problem names and aliases are cached, so `tb vpn 10` works offline once `tb` has talked to the server.

## Identity

Like the web app, the CLI has no login. The server records the address the request came from (one VPN
address per person), which only administrators can see. Reports are tagged `source: cli`.
