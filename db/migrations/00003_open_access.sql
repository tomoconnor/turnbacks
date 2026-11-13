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
-- Open access: no user accounts. A report's reporter is a pseudonymous key
-- ("ip:<address>" for web/CLI, "slack:<team>/<user>" for Slack) used only to
-- count distinct people affected and, for administrators, as an audit trail.
-- Administration is protected by a shared passphrase.

ALTER TABLE turnbacks ADD COLUMN reporter text CHECK (reporter IS NULL OR length(reporter) <= 200);
-- Hash of a one-time token that lets the submitter undo a report shortly after recording it.
ALTER TABLE turnbacks ADD COLUMN undo_token_hash bytea;
UPDATE turnbacks SET reporter = 'user:' || reporter_id::text WHERE reporter_id IS NOT NULL;
DROP INDEX turnbacks_reporter_occurred_idx;
-- Dropping these columns also drops the anonymous/reporter CHECK constraint.
ALTER TABLE turnbacks DROP COLUMN reporter_id, DROP COLUMN anonymous;
CREATE INDEX turnbacks_reporter_occurred_idx ON turnbacks (reporter, occurred_at) WHERE reporter IS NOT NULL;

ALTER TABLE problems DROP COLUMN created_by;
ALTER TABLE remediations DROP COLUMN created_by;

-- Team size, set by administrators, used for reporting participation.
ALTER TABLE teams ADD COLUMN headcount integer CHECK (headcount IS NULL OR headcount >= 1);
UPDATE teams t SET headcount = NULLIF((SELECT count(*) FROM users u WHERE u.team_id = t.id), 0);

ALTER TABLE org_settings DROP COLUMN dashboard_visible_to_users;

DROP TABLE slack_identities;
DROP TABLE slack_link_codes;
DROP TABLE auth_flows;
DROP TABLE sessions;
DROP TABLE users;

CREATE TABLE admin_sessions (
    -- SHA-256 of the opaque admin session token held in an HTTP-only cookie.
    token_hash bytea PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX admin_sessions_expires_at_idx ON admin_sessions (expires_at);

-- +goose Down
-- Structural rollback only: user accounts and reporter links cannot be restored.
DROP TABLE admin_sessions;
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name text NOT NULL,
    email text,
    idp_issuer text NOT NULL,
    idp_subject text NOT NULL,
    role text NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin', 'analyst')),
    team_id uuid REFERENCES teams (id) ON DELETE SET NULL,
    default_anonymous boolean NOT NULL DEFAULT false,
    is_demo boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,
    UNIQUE (idp_issuer, idp_subject)
);
CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE TABLE auth_flows (
    state text PRIMARY KEY, nonce text NOT NULL, code_verifier text NOT NULL,
    return_to text NOT NULL DEFAULT '/', expires_at timestamptz NOT NULL
);
CREATE TABLE slack_identities (
    slack_team_id text NOT NULL, slack_user_id text NOT NULL,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (slack_team_id, slack_user_id)
);
CREATE TABLE slack_link_codes (
    code_hash bytea PRIMARY KEY, user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);
ALTER TABLE org_settings ADD COLUMN dashboard_visible_to_users boolean NOT NULL DEFAULT true;
ALTER TABLE teams DROP COLUMN headcount;
ALTER TABLE remediations ADD COLUMN created_by uuid REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE problems ADD COLUMN created_by uuid REFERENCES users (id) ON DELETE SET NULL;
DROP INDEX turnbacks_reporter_occurred_idx;
ALTER TABLE turnbacks ADD COLUMN reporter_id uuid REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE turnbacks ADD COLUMN anonymous boolean NOT NULL DEFAULT false;
ALTER TABLE turnbacks DROP COLUMN undo_token_hash;
ALTER TABLE turnbacks DROP COLUMN reporter;
CREATE INDEX turnbacks_reporter_occurred_idx ON turnbacks (reporter_id, occurred_at) WHERE reporter_id IS NOT NULL;
