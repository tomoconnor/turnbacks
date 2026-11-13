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
)

// RunMaintenance applies retention settings and removes expired ephemeral
// records (the Slack replay log and webhook deliveries older than 30 days). It is safe to
// run repeatedly and concurrently with normal traffic.
func (s *Service) RunMaintenance(ctx context.Context) error {
	st, _, err := s.Settings(ctx)
	if err != nil {
		return err
	}
	if st.NoteRetentionDays != nil {
		n, err := s.store.ApplyNoteRetention(ctx, *st.NoteRetentionDays)
		if err != nil {
			return err
		}
		if n > 0 {
			s.log.InfoContext(ctx, "retention: cleared notes", "count", n, "older_than_days", *st.NoteRetentionDays)
		}
	}
	if st.ReporterRetentionDays != nil {
		n, err := s.store.ApplyReporterRetention(ctx, *st.ReporterRetentionDays)
		if err != nil {
			return err
		}
		if n > 0 {
			s.log.InfoContext(ctx, "retention: removed reporter addresses", "count", n, "older_than_days", *st.ReporterRetentionDays)
		}
		// Change markers record their creator the same way turnbacks record
		// their reporter, so the same retention applies.
		n, err = s.store.ApplyMarkerCreatorRetention(ctx, *st.ReporterRetentionDays)
		if err != nil {
			return err
		}
		if n > 0 {
			s.log.InfoContext(ctx, "retention: removed change marker creators", "count", n, "older_than_days", *st.ReporterRetentionDays)
		}
	}
	for name, fn := range map[string]func(context.Context) (int64, error){
		"slack_requests":     s.store.DeleteOldSlackRequests,
		"webhook_deliveries": s.store.DeleteOldDeliveries,
	} {
		if _, err := fn(ctx); err != nil {
			s.log.ErrorContext(ctx, "maintenance cleanup failed", "table", name, "error", err)
		}
	}
	return nil
}

// MaintenanceLoop runs RunMaintenance periodically until ctx is cancelled.
func (s *Service) MaintenanceLoop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.RunMaintenance(ctx); err != nil && ctx.Err() == nil {
			s.log.ErrorContext(ctx, "maintenance failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
