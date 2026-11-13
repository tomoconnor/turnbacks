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

// Package config loads application configuration from environment variables.
//
// Secrets may alternatively be supplied via a file by appending _FILE to the
// variable name (e.g. TURNBACKS_SLACK_SIGNING_SECRET_FILE=/run/secrets/slack).
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environments.
const (
	EnvProduction  = "production"
	EnvDevelopment = "development"
	EnvTest        = "test"
)

// Config is the complete application configuration.
type Config struct {
	Env             string
	HTTPAddr        string
	PublicURL       *url.URL
	DatabaseURL     string
	DBMaxConns      int32
	DBWait          time.Duration // how long to wait for the database at start-up
	MigrateOnStart  bool
	LogLevel        slog.Level
	LogFormat       string
	TrustedProxies  []netip.Prefix
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration

	Admin    AdminConfig
	Identity IdentityConfig

	Slack SlackConfig

	Webhooks WebhookConfig
}

// IdentityConfig chooses where a request's identity comes from: the client
// address ("ip", the default) or a user header set by a single sign-on proxy
// ("header"), believed only from TrustedProxies.
type IdentityConfig struct {
	Mode         string
	UserHeader   string
	GroupsHeader string
	// AdminUsers and AdminGroups make single sign-on users administrators.
	AdminUsers  []string
	AdminGroups []string
}

// WebhookConfig configures outbound webhook delivery.
type WebhookConfig struct {
	// AllowLoopback permits webhook URLs that resolve to loopback addresses
	// (development and tests). Link-local addresses (cloud metadata) are
	// always refused.
	AllowLoopback bool
}

// AdminConfig configures client-certificate administration. TLS is
// terminated by a reverse proxy, which forwards the client certificate in
// CertHeader; it is believed only from TrustedProxies.
type AdminConfig struct {
	// ClientCAFile is the admin CA certificate (PEM). Empty disables administration.
	ClientCAFile string
	// CertHeader carries the forwarded client certificate.
	CertHeader string
	// AllowedNames optionally restricts administrators by certificate common name.
	AllowedNames []string
	// DevLoopback makes local requests administrators (development only).
	DevLoopback bool
}

// SlackConfig configures the Slack slash-command integration.
type SlackConfig struct {
	Enabled       bool
	SigningSecret string
}

// IsDevelopment reports whether development-only features (demo data) may be used.
func (c *Config) IsDevelopment() bool { return c.Env == EnvDevelopment || c.Env == EnvTest }

// Load reads configuration from the environment.
func Load() (*Config, error) { return load(os.Getenv) }

func load(getenv func(string) string) (*Config, error) {
	var errs []error
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	secret := func(key string) string {
		if v := getenv(key); v != "" {
			return v
		}
		if path := getenv(key + "_FILE"); path != "" {
			b, err := os.ReadFile(path) //nolint:gosec // operator-supplied secret file path
			if err != nil {
				errs = append(errs, fmt.Errorf("%s_FILE: %w", key, err))
				return ""
			}
			return strings.TrimSpace(string(b))
		}
		return ""
	}
	boolean := func(key string, def bool) bool {
		v := get(key, "")
		if v == "" {
			return def
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: invalid boolean %q", key, v))
		}
		return b
	}
	duration := func(key string, def time.Duration) time.Duration {
		v := get(key, "")
		if v == "" {
			return def
		}
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s: invalid duration %q", key, v))
			return def
		}
		return d
	}
	list := func(key string) []string {
		var out []string
		for _, s := range strings.Split(get(key, ""), ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}

	c := &Config{
		Env:             get("TURNBACKS_ENV", EnvProduction),
		HTTPAddr:        get("TURNBACKS_HTTP_ADDR", ":8080"),
		DatabaseURL:     secret("TURNBACKS_DATABASE_URL"),
		MigrateOnStart:  boolean("TURNBACKS_MIGRATE_ON_START", true),
		DBWait:          duration("TURNBACKS_DB_WAIT", 60*time.Second),
		LogFormat:       get("TURNBACKS_LOG_FORMAT", "json"),
		RequestTimeout:  duration("TURNBACKS_REQUEST_TIMEOUT", 30*time.Second),
		ShutdownTimeout: duration("TURNBACKS_SHUTDOWN_TIMEOUT", 20*time.Second),
		Admin: AdminConfig{
			ClientCAFile: get("TURNBACKS_ADMIN_CLIENT_CA", ""),
			CertHeader:   get("TURNBACKS_ADMIN_CERT_HEADER", "X-Client-Cert"),
			AllowedNames: list("TURNBACKS_ADMIN_NAMES"),
			DevLoopback:  boolean("TURNBACKS_DEV_ADMIN_LOOPBACK", false),
		},
		Identity: IdentityConfig{
			Mode:         get("TURNBACKS_IDENTITY", "ip"),
			UserHeader:   get("TURNBACKS_IDENTITY_USER_HEADER", "X-Forwarded-Email"),
			GroupsHeader: get("TURNBACKS_IDENTITY_GROUPS_HEADER", "X-Forwarded-Groups"),
			AdminUsers:   list("TURNBACKS_ADMIN_USERS"),
			AdminGroups:  list("TURNBACKS_ADMIN_GROUPS"),
		},
		Webhooks: WebhookConfig{
			AllowLoopback: boolean("TURNBACKS_WEBHOOK_ALLOW_LOOPBACK", false),
		},
	}
	switch c.Identity.Mode {
	case "ip", "header":
	default:
		errs = append(errs, fmt.Errorf("TURNBACKS_IDENTITY: must be ip or header, got %q", c.Identity.Mode))
	}
	if c.Identity.Mode != "header" && (len(c.Identity.AdminUsers) > 0 || len(c.Identity.AdminGroups) > 0) {
		errs = append(errs, errors.New("TURNBACKS_ADMIN_USERS and TURNBACKS_ADMIN_GROUPS need TURNBACKS_IDENTITY=header"))
	}
	switch c.Env {
	case EnvProduction, EnvDevelopment, EnvTest:
	default:
		errs = append(errs, fmt.Errorf("TURNBACKS_ENV: must be production, development or test, got %q", c.Env))
	}

	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("TURNBACKS_DATABASE_URL is required"))
	}
	maxConns, err := strconv.Atoi(get("TURNBACKS_DB_MAX_CONNS", "10"))
	if err != nil || maxConns < 1 || maxConns > 1000 {
		errs = append(errs, errors.New("TURNBACKS_DB_MAX_CONNS: must be an integer between 1 and 1000"))
	}
	c.DBMaxConns = int32(maxConns) //nolint:gosec // range-checked above (1..1000)

	if err := c.LogLevel.UnmarshalText([]byte(get("TURNBACKS_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("TURNBACKS_LOG_LEVEL: %w", err))
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, errors.New("TURNBACKS_LOG_FORMAT: must be json or text"))
	}

	if raw := get("TURNBACKS_PUBLIC_URL", ""); raw != "" {
		u, err := url.Parse(strings.TrimRight(raw, "/"))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("TURNBACKS_PUBLIC_URL: invalid URL %q", raw))
		} else {
			c.PublicURL = u
		}
	}

	for _, s := range list("TURNBACKS_TRUSTED_PROXIES") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			a, aerr := netip.ParseAddr(s)
			if aerr != nil {
				errs = append(errs, fmt.Errorf("TURNBACKS_TRUSTED_PROXIES: invalid CIDR or address %q", s))
				continue
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		c.TrustedProxies = append(c.TrustedProxies, p.Masked())
	}

	c.Slack = SlackConfig{
		SigningSecret: secret("TURNBACKS_SLACK_SIGNING_SECRET"),
	}
	c.Slack.Enabled = c.Slack.SigningSecret != ""

	if c.Admin.DevLoopback && c.Env == EnvProduction {
		errs = append(errs, errors.New("TURNBACKS_DEV_ADMIN_LOOPBACK is not allowed in production"))
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return c, nil
}
