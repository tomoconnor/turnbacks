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

// Command turnbacks runs the Turnbacks server and its operational tasks.
//
//	turnbacks [serve]                 run the HTTP server (default)
//	turnbacks migrate up|down|status  manage database migrations
//	turnbacks seed [--purge]          load (or remove) demonstration data; never in production
//	turnbacks healthcheck [--wait D]  probe /readyz, retrying for up to D (container health checks, helm test)
//	turnbacks ca init|issue           tiny CA for admin client certificates (see docs/admin-certificates.md)
//	turnbacks version                 print the version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	apispec "turnbacks/api"
	"turnbacks/internal/api"
	"turnbacks/internal/auth"
	"turnbacks/internal/config"
	"turnbacks/internal/integrations/slack"
	"turnbacks/internal/logx"
	"turnbacks/internal/seed"
	"turnbacks/internal/service"
	"turnbacks/internal/store"
	"turnbacks/internal/webhook"
	"turnbacks/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "migrate":
		err = migrate(args)
	case "seed":
		err = runSeed(args)
	case "healthcheck":
		err = healthcheck(args)
	case "ca":
		err = runCA(args)
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		fmt.Println("usage: turnbacks [serve | migrate up|down|status | seed [--purge] | healthcheck | ca init|issue | version]")
	default:
		err = fmt.Errorf("unknown command %q (try `turnbacks help`)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func setup(ctx context.Context) (*config.Config, *slog.Logger, *store.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, err
	}
	log := logx.New(os.Stdout, cfg.LogFormat, cfg.LogLevel)
	slog.SetDefault(log)
	st, err := store.Open(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBWait)
	if err != nil {
		return nil, nil, nil, err
	}
	return cfg, log, st, nil
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, log, st, err := setup(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	log.Info("starting turnbacks", "version", version, "env", cfg.Env, "addr", cfg.HTTPAddr)

	if cfg.MigrateOnStart {
		if err := st.MigrateUp(ctx, log); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	admin, err := auth.NewAdmin(auth.AdminConfig(cfg.Admin), auth.IdentityConfig(cfg.Identity), log)
	if err != nil {
		return err
	}
	svc := service.New(st, log)
	if cfg.PublicURL != nil {
		svc.SetPublicURL(cfg.PublicURL.String())
	}
	svc.SetWebhookClient(webhook.NewClient(10*time.Second, cfg.Webhooks.AllowLoopback))
	deps := api.Deps{
		Config: cfg, Log: log, Store: st, Service: svc, Admin: admin,
		Static: web.Assets(), OpenAPI: apispec.OpenAPI,
	}
	if cfg.Slack.Enabled {
		deps.Slack = slack.New(cfg.Slack.SigningSecret, st, svc, log)
		log.Info("slack integration enabled")
	}
	socket, isUnix := strings.CutPrefix(cfg.HTTPAddr, "unix:")
	if len(cfg.TrustedProxies) == 0 && !isUnix {
		log.Warn("TURNBACKS_TRUSTED_PROXIES is empty: if Turnbacks runs behind a reverse proxy or ingress, every report will be attributed to the proxy's address")
		if cfg.Admin.ClientCAFile != "" {
			log.Warn("TURNBACKS_TRUSTED_PROXIES is empty: forwarded admin client certificates will be ignored, so nobody can administer")
		}
		if cfg.Identity.Mode == auth.IdentityHeader {
			log.Warn("TURNBACKS_TRUSTED_PROXIES is empty: single sign-on user headers will be ignored, so everyone is identified by address")
		}
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.New(deps),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      cfg.RequestTimeout + 30*time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		// Connections on a Unix socket come from the reverse proxy that shares it.
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if _, ok := c.(*net.UnixConn); ok {
				return auth.MarkUnixSocket(ctx)
			}
			return ctx
		},
	}
	ln, err := listen(cfg.HTTPAddr, socket, isUnix)
	if err != nil {
		return err
	}

	go svc.MaintenanceLoop(ctx, time.Hour)
	go svc.WebhookLoop(ctx, 15*time.Second)

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}

// listen opens a TCP address or, for "unix:/path", a Unix socket (replacing a
// stale socket left by a previous run) that the reverse proxy can use.
func listen(addr, socket string, isUnix bool) (net.Listener, error) {
	if !isUnix {
		return net.Listen("tcp", addr)
	}
	if fi, err := os.Lstat(socket); err == nil && fi.Mode()&os.ModeSocket != 0 {
		_ = os.Remove(socket)
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	// Access is controlled by who can reach the socket's directory (the volume
	// shared only with the reverse proxy). The proxy may run as a different
	// user without capabilities, so the socket itself is world-writable.
	if err := os.Chmod(socket, 0o666); err != nil { //nolint:gosec // see above
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

func migrate(args []string) error {
	ctx := context.Background()
	_, log, st, err := setup(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	dir := "up"
	if len(args) > 0 {
		dir = args[0]
	}
	switch dir {
	case "up":
		return st.MigrateUp(ctx, log)
	case "down":
		return st.MigrateDown(ctx, log)
	case "status":
		lines, err := st.MigrationStatus(ctx)
		for _, l := range lines {
			fmt.Println(l)
		}
		return err
	}
	return fmt.Errorf("unknown migrate direction %q (up, down, status)", dir)
}

func runSeed(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "remove all demonstration data instead of adding it")
	seedVal := fs.Uint64("seed", 42, "random seed for reproducible data")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	cfg, log, st, err := setup(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	if *purge {
		n, err := seed.Purge(ctx, st)
		if err == nil {
			log.Info("demo data purged", "rows", n)
		}
		return err
	}
	if !cfg.IsDevelopment() {
		return errors.New("refusing to load demonstration data: TURNBACKS_ENV must be development or test")
	}
	if err := st.MigrateUp(ctx, log); err != nil {
		return err
	}
	res, err := seed.Run(ctx, st, time.Now(), *seedVal)
	if err != nil {
		return err
	}
	log.Info("demo data loaded", "teams", res.Teams, "people", res.People, "problems", res.Problems,
		"turnbacks", res.Turnbacks, "remediations", res.Remediations)
	return nil
}

// healthcheck probes the server's readiness endpoint. It needs no
// configuration beyond TURNBACKS_HTTP_ADDR, so it works in minimal images.
// With --wait it retries until ready or the duration elapses (useful where
// networking, e.g. NetworkPolicy enforcement, takes a moment to converge).
func healthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	wait := fs.Duration("wait", 0, "keep retrying for up to this long")
	if err := fs.Parse(args); err != nil {
		return err
	}
	deadline := time.Now().Add(*wait)
	for {
		err := probeReady()
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(time.Second)
	}
}

func probeReady() error {
	addr := os.Getenv("TURNBACKS_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	if socket, ok := strings.CutPrefix(addr, "unix:"); ok {
		client.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}
		addr = "turnbacks"
	} else if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	resp, err := client.Get("http://" + addr + "/readyz") //nolint:gosec // local readiness probe of this process
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: %s", resp.Status)
	}
	return nil
}
