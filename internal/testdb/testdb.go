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

// Package testdb provides isolated PostgreSQL databases for integration tests.
//
// By default a PostgreSQL container is started with testcontainers-go (once
// per test binary). Set TURNBACKS_TEST_DATABASE_URL to an existing server
// (with permission to CREATE DATABASE) to skip Docker. Each test gets its own
// database cloned from a migrated template, so tests can run in parallel.
package testdb

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"turnbacks/internal/store"
)

const templateDB = "turnbacks_template"

var (
	once    sync.Once
	baseURL string
	initErr error
	counter atomic.Int64
)

func withDB(raw, db string) string {
	u, _ := url.Parse(raw)
	u.Path = "/" + db
	return u.String()
}

func setup() {
	ctx := context.Background()
	baseURL = os.Getenv("TURNBACKS_TEST_DATABASE_URL")
	if baseURL == "" {
		c, err := postgres.Run(ctx, "postgres:18-alpine",
			postgres.WithDatabase("postgres"), postgres.WithUsername("postgres"), postgres.WithPassword("postgres"),
			postgres.BasicWaitStrategies(),
		)
		if err != nil {
			initErr = fmt.Errorf("start postgres container (set TURNBACKS_TEST_DATABASE_URL to use an existing server): %w", err)
			return
		}
		baseURL, initErr = c.ConnectionString(ctx, "sslmode=disable")
		if initErr != nil {
			return
		}
	}
	admin, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		initErr = err
		return
	}
	defer func() { _ = admin.Close(ctx) }()
	_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+templateDB+" WITH (FORCE)")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+templateDB); err != nil {
		initErr = err
		return
	}
	st, err := store.Open(ctx, withDB(baseURL, templateDB), 2, 0)
	if err != nil {
		initErr = err
		return
	}
	defer st.Close()
	initErr = st.MigrateUp(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// New returns a store connected to a fresh, migrated database that is dropped
// when the test finishes.
func New(t testing.TB) *store.Store {
	t.Helper()
	once.Do(setup)
	if initErr != nil {
		t.Fatalf("test database unavailable: %v", initErr)
	}
	ctx := context.Background()
	name := fmt.Sprintf("tb_test_%d_%d", os.Getpid(), counter.Add(1))
	admin, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, templateDB)); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, withDB(baseURL, name), 4, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		st.Close()
		c, err := pgx.Connect(context.Background(), baseURL)
		if err != nil {
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return st
}

// Logger returns a quiet logger for tests (set TURNBACKS_TEST_LOG=1 to see logs).
func Logger() *slog.Logger {
	if strings.TrimSpace(os.Getenv("TURNBACKS_TEST_LOG")) != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
