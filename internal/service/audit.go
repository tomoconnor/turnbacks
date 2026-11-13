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

package service

import (
	"context"
	"time"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
)

// AuditEntry is one administrative action.
type AuditEntry struct {
	Admin     string
	Via       string
	Method    string
	Action    string // route pattern, e.g. "/api/v1/problems/{id}/merge"
	Target    string // route parameters, e.g. "id=…"
	Status    int
	Client    string
	RequestID string
}

// RecordAudit stores an audit entry. It is called by the HTTP layer after an
// administrator's request has been handled.
func (s *Service) RecordAudit(ctx context.Context, e AuditEntry) error {
	return s.store.InsertAudit(ctx, sqlc.InsertAuditParams{
		Admin: e.Admin, Via: e.Via, Method: e.Method, Action: e.Action, Target: e.Target,
		Status: int32(e.Status), //nolint:gosec // HTTP status codes are small
		Client: strPtr(e.Client), RequestID: strPtr(e.RequestID),
	})
}

// AuditFilter selects audit entries.
type AuditFilter struct {
	From, To *time.Time
	BeforeID *int64
	Limit    int32
}

// ListAudit returns audit entries, newest first (administrators only).
func (s *Service) ListAudit(ctx context.Context, a domain.Actor, f AuditFilter) ([]sqlc.AdminAudit, int64, error) {
	if err := requireAdmin(a); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	rows, err := s.store.ListAudit(ctx, sqlc.ListAuditParams{FromTs: f.From, ToTs: f.To, BeforeID: f.BeforeID, Lim: f.Limit})
	if err != nil {
		return nil, 0, err
	}
	n, err := s.store.CountAudit(ctx, sqlc.CountAuditParams{FromTs: f.From, ToTs: f.To})
	return rows, n, err
}

// ExportAudit streams every audit entry in the range, newest first
// (administrators only).
func (s *Service) ExportAudit(ctx context.Context, a domain.Actor, from, to *time.Time, fn func(sqlc.AdminAudit) error) error {
	if err := requireAdmin(a); err != nil {
		return err
	}
	var before *int64
	for {
		rows, err := s.store.ListAudit(ctx, sqlc.ListAuditParams{FromTs: from, ToTs: to, BeforeID: before, Lim: 1000})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if err := fn(r); err != nil {
				return err
			}
		}
		if len(rows) < 1000 {
			return nil
		}
		before = &rows[len(rows)-1].ID
	}
}
