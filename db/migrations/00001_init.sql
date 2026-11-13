-- Copyright (c) 2026 Tom O'Connor <tom@twinhelix.org>
--
-- Permission is hereby granted, free of charge, to any person obtaining a copy
-- of this software and associated documentation files (the "Software"), to deal
-- in the Software without restriction, including without limitation the rights
-- to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
-- copies of the Software, and to permit persons to whom the Software is
-- furnished to do so, subject to the following conditions:
--
-- The above copyright notice and this permission notice shall be included in
-- all copies or substantial portions of the Software.
--
-- THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
-- IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
-- FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
-- AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
-- LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
-- OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
-- SOFTWARE.

-- +goose Up
-- Turnbacks initial schema.
-- All timestamps are timestamptz (stored in UTC). Money is stored as integer
-- minor currency units (e.g. pence). Percentages are whole integers 0..100.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE teams (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name              text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    active            boolean NOT NULL DEFAULT true,
    -- Optional team-specific fully loaded hourly cost, minor units.
    hourly_rate_minor bigint CHECK (hourly_rate_minor IS NULL OR hourly_rate_minor >= 0),
    is_demo           boolean NOT NULL DEFAULT false,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX teams_name_lower_key ON teams (lower(name));

CREATE TABLE users (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name      text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    email             text,
    idp_issuer        text NOT NULL,
    idp_subject       text NOT NULL,
    role              text NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin', 'analyst')),
    team_id           uuid REFERENCES teams (id) ON DELETE SET NULL,
    default_anonymous boolean NOT NULL DEFAULT false,
    is_demo           boolean NOT NULL DEFAULT false,
    created_at        timestamptz NOT NULL DEFAULT now(),
    last_login_at     timestamptz,
    UNIQUE (idp_issuer, idp_subject)
);
CREATE INDEX users_team_id_idx ON users (team_id);

CREATE TABLE categories (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    description text NOT NULL DEFAULT '',
    active      boolean NOT NULL DEFAULT true,
    sort_order  integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX categories_name_lower_key ON categories (lower(name));

INSERT INTO categories (name, description, sort_order) VALUES
    ('Networking', 'VPN, DNS, Wi-Fi and connectivity problems.', 10),
    ('CI/CD', 'Build, test and deployment pipeline failures unrelated to code changes.', 20),
    ('Development Environment', 'Local or remote development environment setup, rebuilds and tooling.', 30),
    ('Identity and Access', 'Authentication, SSO, permissions and access requests.', 40),
    ('Internal Tools', 'Unreliable or slow internal services and tools.', 50),
    ('Processes', 'Approvals, manual workarounds and bureaucratic interruptions.', 60),
    ('Other', 'Anything that does not fit another category.', 1000);

CREATE TABLE problems (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title          text NOT NULL CHECK (length(btrim(title)) BETWEEN 3 AND 120),
    description    text NOT NULL DEFAULT '' CHECK (length(description) <= 4000),
    category_id    uuid NOT NULL REFERENCES categories (id),
    status         text NOT NULL DEFAULT 'active'
                   CHECK (status IN ('active', 'monitoring', 'resolved', 'archived')),
    alias          text CHECK (alias IS NULL OR alias ~ '^[a-z0-9][a-z0-9-]{0,31}$'),
    external_url   text CHECK (external_url IS NULL OR external_url ~* '^https?://'),
    merged_into_id uuid REFERENCES problems (id),
    created_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    is_demo        boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    resolved_at    timestamptz,
    CHECK (merged_into_id IS NULL OR merged_into_id <> id)
);
CREATE UNIQUE INDEX problems_alias_key ON problems (alias) WHERE alias IS NOT NULL;
CREATE INDEX problems_category_id_idx ON problems (category_id);
CREATE INDEX problems_title_trgm_idx ON problems USING gin (lower(title) gin_trgm_ops);

CREATE TABLE turnbacks (
    -- Client-generated UUID; doubles as the idempotency key for submission.
    id                 uuid PRIMARY KEY,
    problem_id         uuid NOT NULL REFERENCES problems (id),
    -- NULL when the submission was anonymous or the reporter was later deleted.
    reporter_id        uuid REFERENCES users (id) ON DELETE SET NULL,
    -- True when the reporter chose not to be linked to the record at all.
    anonymous          boolean NOT NULL DEFAULT false,
    team_id            uuid REFERENCES teams (id) ON DELETE SET NULL,
    duration_minutes   integer NOT NULL CHECK (duration_minutes BETWEEN 1 AND 1440),
    duration_estimated boolean NOT NULL DEFAULT true,
    note               text CHECK (note IS NULL OR length(note) <= 1000),
    occurred_at        timestamptz NOT NULL,
    submitted_at       timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    source             text NOT NULL CHECK (source IN ('web', 'pwa_offline', 'api', 'slack')),
    is_demo            boolean NOT NULL DEFAULT false,
    CHECK (NOT (anonymous AND reporter_id IS NOT NULL))
);
CREATE INDEX turnbacks_occurred_at_idx ON turnbacks (occurred_at);
CREATE INDEX turnbacks_problem_occurred_idx ON turnbacks (problem_id, occurred_at);
CREATE INDEX turnbacks_team_occurred_idx ON turnbacks (team_id, occurred_at);
CREATE INDEX turnbacks_reporter_occurred_idx ON turnbacks (reporter_id, occurred_at) WHERE reporter_id IS NOT NULL;

CREATE TABLE remediations (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    problem_id               uuid NOT NULL REFERENCES problems (id),
    implemented_on           date NOT NULL,
    description              text NOT NULL CHECK (length(btrim(description)) BETWEEN 1 AND 4000),
    implementation_cost_minor bigint NOT NULL DEFAULT 0 CHECK (implementation_cost_minor >= 0),
    annual_cost_minor        bigint NOT NULL DEFAULT 0 CHECK (annual_cost_minor >= 0),
    expected_reduction_pct   integer NOT NULL DEFAULT 0 CHECK (expected_reduction_pct BETWEEN 0 AND 100),
    created_by               uuid REFERENCES users (id) ON DELETE SET NULL,
    is_demo                  boolean NOT NULL DEFAULT false,
    created_at               timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX remediations_problem_id_idx ON remediations (problem_id, implemented_on);

-- Single-row organisation settings.
CREATE TABLE org_settings (
    id                        integer PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    org_name                  text NOT NULL DEFAULT 'My Organisation',
    currency                  text NOT NULL DEFAULT 'GBP' CHECK (currency ~ '^[A-Z]{3}$'),
    default_hourly_rate_minor bigint NOT NULL DEFAULT 5000 CHECK (default_hourly_rate_minor >= 0),
    reporting_timezone        text NOT NULL DEFAULT 'Europe/London',
    -- Share of reported lost time assumed to be recoverable productive capacity
    -- when projecting benefits. Editable assumption, not a fact.
    recoverable_pct           integer NOT NULL DEFAULT 100 CHECK (recoverable_pct BETWEEN 0 AND 100),
    -- Durations above this require explicit confirmation on submission.
    large_duration_minutes    integer NOT NULL DEFAULT 120 CHECK (large_duration_minutes BETWEEN 1 AND 1440),
    -- Retention: free-text notes are cleared after N days (NULL = keep).
    note_retention_days       integer CHECK (note_retention_days IS NULL OR note_retention_days >= 1),
    -- Retention: reporter links are removed after N days (NULL = keep).
    reporter_retention_days   integer CHECK (reporter_retention_days IS NULL OR reporter_retention_days >= 1),
    -- Whether users with the 'user' role may see aggregated dashboards.
    dashboard_visible_to_users boolean NOT NULL DEFAULT true,
    updated_at                timestamptz NOT NULL DEFAULT now()
);
INSERT INTO org_settings (id) VALUES (1);

CREATE TABLE sessions (
    -- SHA-256 of the opaque session token; the raw token only lives in the cookie.
    token_hash   bytea PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- Pending OIDC authorisation requests (state, nonce, PKCE verifier).
CREATE TABLE auth_flows (
    state         text PRIMARY KEY,
    nonce         text NOT NULL,
    code_verifier text NOT NULL,
    return_to     text NOT NULL DEFAULT '/',
    expires_at    timestamptz NOT NULL
);

CREATE TABLE slack_identities (
    slack_team_id text NOT NULL,
    slack_user_id text NOT NULL,
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (slack_team_id, slack_user_id)
);

CREATE TABLE slack_link_codes (
    code_hash  bytea PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

-- Signatures of recently accepted Slack requests, for replay rejection.
CREATE TABLE slack_request_log (
    signature_hash bytea PRIMARY KEY,
    received_at    timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE slack_request_log;
DROP TABLE slack_link_codes;
DROP TABLE slack_identities;
DROP TABLE auth_flows;
DROP TABLE sessions;
DROP TABLE org_settings;
DROP TABLE remediations;
DROP TABLE turnbacks;
DROP TABLE problems;
DROP TABLE categories;
DROP TABLE users;
DROP TABLE teams;
