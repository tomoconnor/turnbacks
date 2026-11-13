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

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
)

// ExportTurnbacks streams every turnback, oldest first. The reporter is
// included for administrators only.
func (s *Service) ExportTurnbacks(ctx context.Context, a domain.Actor, fn func(sqlc.ExportTurnbacksRow) error) error {
	after, afterID := time.Unix(0, 0).UTC(), uuid.Nil
	const page = 2000
	for {
		rows, err := s.store.ExportTurnbacks(ctx, sqlc.ExportTurnbacksParams{AfterOccurredAt: after, AfterID: afterID, Lim: page})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !a.Admin {
				r.Reporter = nil
			}
			if err := fn(r); err != nil {
				return err
			}
		}
		if len(rows) < page {
			return nil
		}
		after, afterID = rows[len(rows)-1].OccurredAt, rows[len(rows)-1].ID
	}
}

// ExportProblems returns every problem, including merged and archived ones.
func (s *Service) ExportProblems(ctx context.Context) ([]sqlc.ExportProblemsRow, error) {
	return s.store.ExportProblems(ctx)
}

// ExportRemediations returns every remediation.
func (s *Service) ExportRemediations(ctx context.Context) ([]sqlc.ExportRemediationsRow, error) {
	return s.store.ExportRemediations(ctx)
}

// ExportMarkers returns every change marker. Who created a marker is included
// for administrators only.
func (s *Service) ExportMarkers(ctx context.Context, a domain.Actor) ([]sqlc.ExportMarkersRow, error) {
	rows, err := s.store.ExportMarkers(ctx)
	if err != nil {
		return nil, err
	}
	if !a.Admin {
		for i := range rows {
			rows[i].ChangeMarker.CreatedBy = nil
		}
	}
	return rows, nil
}
