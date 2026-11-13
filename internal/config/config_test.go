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

package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRequiresDatabase(t *testing.T) {
	_, err := load(env(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "TURNBACKS_DATABASE_URL") {
		t.Fatalf("got %v", err)
	}
}

func TestProductionDefaults(t *testing.T) {
	c, err := load(env(map[string]string{
		"TURNBACKS_DATABASE_URL":    "postgres://x",
		"TURNBACKS_PUBLIC_URL":      "https://turnbacks.example.com/",
		"TURNBACKS_TRUSTED_PROXIES": "10.0.0.0/8, 192.168.1.1",
		"TURNBACKS_ADMIN_CLIENT_CA": "/etc/turnbacks/admin-ca.crt",
		"TURNBACKS_ADMIN_NAMES":     "jane, raj",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != EnvProduction || c.PublicURL.String() != "https://turnbacks.example.com" {
		t.Errorf("unexpected %+v", c)
	}
	if c.Admin.ClientCAFile != "/etc/turnbacks/admin-ca.crt" || c.Admin.CertHeader != "X-Client-Cert" || strings.Join(c.Admin.AllowedNames, "|") != "jane|raj" {
		t.Errorf("admin = %+v", c.Admin)
	}
	if len(c.TrustedProxies) != 2 || c.TrustedProxies[1].String() != "192.168.1.1/32" {
		t.Errorf("proxies = %v", c.TrustedProxies)
	}
}

func TestDevAdminRefusedInProduction(t *testing.T) {
	_, err := load(env(map[string]string{"TURNBACKS_DATABASE_URL": "postgres://x", "TURNBACKS_DEV_ADMIN_LOOPBACK": "true"}))
	if err == nil || !strings.Contains(err.Error(), "TURNBACKS_DEV_ADMIN_LOOPBACK") {
		t.Fatalf("got %v", err)
	}
	c, err := load(env(map[string]string{"TURNBACKS_ENV": "development", "TURNBACKS_DATABASE_URL": "postgres://x", "TURNBACKS_DEV_ADMIN_LOOPBACK": "true"}))
	if err != nil || !c.Admin.DevLoopback {
		t.Fatalf("%v %+v", err, c)
	}
}

func TestAdminDisabledWithoutCA(t *testing.T) {
	c, err := load(env(map[string]string{"TURNBACKS_DATABASE_URL": "postgres://x"}))
	if err != nil || c.Admin.ClientCAFile != "" {
		t.Fatalf("%v %+v", err, c)
	}
}

func TestIdentityConfig(t *testing.T) {
	c, err := load(env(map[string]string{"TURNBACKS_DATABASE_URL": "postgres://x"}))
	if err != nil || c.Identity.Mode != "ip" || c.Identity.UserHeader != "X-Forwarded-Email" || c.Webhooks.AllowLoopback {
		t.Fatalf("defaults: %v %+v", err, c.Identity)
	}
	c, err = load(env(map[string]string{
		"TURNBACKS_DATABASE_URL": "postgres://x", "TURNBACKS_IDENTITY": "header", "TURNBACKS_IDENTITY_USER_HEADER": "Remote-User",
		"TURNBACKS_ADMIN_GROUPS": "ops, turnbacks-admins", "TURNBACKS_ADMIN_USERS": "boss@example.com",
	}))
	if err != nil || c.Identity.Mode != "header" || c.Identity.UserHeader != "Remote-User" || len(c.Identity.AdminGroups) != 2 || len(c.Identity.AdminUsers) != 1 {
		t.Fatalf("header: %v %+v", err, c.Identity)
	}
	if _, err := load(env(map[string]string{"TURNBACKS_DATABASE_URL": "postgres://x", "TURNBACKS_IDENTITY": "oidc"})); err == nil {
		t.Error("unknown identity mode accepted")
	}
	if _, err := load(env(map[string]string{"TURNBACKS_DATABASE_URL": "postgres://x", "TURNBACKS_ADMIN_GROUPS": "ops"})); err == nil {
		t.Error("admin groups accepted without header identity")
	}
}
