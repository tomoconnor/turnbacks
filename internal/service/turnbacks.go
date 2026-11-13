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
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"turnbacks/db/sqlc"
	"turnbacks/internal/domain"
	"turnbacks/internal/store"
)

// Submission time-window limits for occurred_at.
const (
	maxClockSkew     = 5 * time.Minute
	maxBackdateRange = 366 * 24 * time.Hour
)

// Quantity limits for recording several identical occurrences at once.
const (
	MaxQuantity = 100
	// LargeQuantity is the quantity above which explicit confirmation is
	// required, to catch typos such as 150 instead of 15.
	LargeQuantity = 20
)

// UndoWindow is how long after recording a submitter may undo it with the
// one-time undo token returned at submission.
const UndoWindow = 15 * time.Minute

// BatchMemberID derives the ID of the i-th occurrence (i >= 1) of a batch
// whose first occurrence has id. Deterministic, so retries are idempotent.
func BatchMemberID(id uuid.UUID, i int) uuid.UUID {
	return uuid.NewSHA1(id, []byte(strconv.Itoa(i)))
}

// TurnbackInput is a request to record an occurrence.
type TurnbackInput struct {
	// ID is a client-generated UUID that doubles as the idempotency key.
	ID                   uuid.UUID
	ProblemID            uuid.UUID
	TeamID               *uuid.UUID
	DurationMinutes      int
	DurationEstimated    bool
	Note                 *string
	OccurredAt           *time.Time // defaults to now
	Source               string
	ConfirmLargeDuration bool // confirms unusually large durations and quantities
	IsDemo               bool
	// Quantity records this many identical occurrences (default 1). Each is
	// its own row; they share a batch_id equal to ID.
	Quantity int
}

// TurnbackResult is the outcome of a submission.
type TurnbackResult struct {
	Turnback     sqlc.Turnback
	ProblemTitle string
	// Created is false when an identical submission with the same ID already
	// existed (an idempotent replay).
	Created bool
	// Quantity is the number of occurrences recorded (or replayed).
	Quantity int
	// UndoToken lets the submitter undo a newly created submission within
	// UndoWindow. Empty for replays.
	UndoToken string
}

func validateTurnbackFields(v *domain.Validator, minutes int, note *string, occurred time.Time, now time.Time, largeThreshold int, confirmLarge bool) error {
	v.Check(minutes >= domain.MinDurationMinutes && minutes <= domain.MaxDurationMinutes,
		"duration_minutes", fmt.Sprintf("must be between %d and %d minutes", domain.MinDurationMinutes, domain.MaxDurationMinutes))
	if note != nil {
		v.Check(utf8.RuneCountInString(*note) <= domain.MaxNoteLength, "note", fmt.Sprintf("must be at most %d characters", domain.MaxNoteLength))
	}
	v.Check(!occurred.After(now.Add(maxClockSkew)), "occurred_at", "must not be in the future")
	v.Check(!occurred.Before(now.Add(-maxBackdateRange)), "occurred_at", "must be within the last year")
	if err := v.Err(); err != nil {
		return err
	}
	if minutes > largeThreshold && !confirmLarge {
		return &domain.Error{Kind: domain.KindConfirmationRequired, Code: "duration_confirmation_required",
			Message: fmt.Sprintf("%d minutes is unusually long (over %d). Please confirm the duration is correct.", minutes, largeThreshold),
			Fields:  []domain.FieldError{{Field: "duration_minutes", Message: "requires confirmation"}}}
	}
	return nil
}

func validTeam(ctx context.Context, q *sqlc.Queries, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	t, err := q.GetTeam(ctx, *id)
	if err != nil || !t.Active {
		return &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "unknown team",
			Fields: []domain.FieldError{{Field: "team_id", Message: "does not exist or is inactive"}}}
	}
	return nil
}

func hashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

// SubmitTurnback records an occurrence idempotently.
//
// If a turnback with the same ID already exists and matches the request, it is
// returned with Created=false. If it exists but differs, a conflict error is
// returned. The reporter is not compared: an offline report may be retried
// from a different network.
func (s *Service) SubmitTurnback(ctx context.Context, a domain.Actor, in TurnbackInput) (TurnbackResult, error) {
	var res TurnbackResult
	settings, _, err := s.Settings(ctx)
	if err != nil {
		return res, err
	}
	now := s.now().UTC()
	in.Note = trimPtr(in.Note)
	occurred := now
	if in.OccurredAt != nil {
		occurred = in.OccurredAt.UTC().Truncate(time.Microsecond)
	}

	var v domain.Validator
	v.Check(in.ID != uuid.Nil, "id", "is required (client-generated UUID)")
	v.Check(in.ProblemID != uuid.Nil, "problem_id", "is required")
	switch in.Source {
	case domain.SourceWeb, domain.SourcePWAOffline, domain.SourceAPI, domain.SourceSlack, domain.SourceCLI,
		domain.SourceWebhook, domain.SourceMattermost, domain.SourceTeams:
	default:
		v.Add("source", "must be web, pwa_offline, api, cli, slack, webhook, mattermost or teams")
	}
	if in.Quantity == 0 {
		in.Quantity = 1
	}
	v.Check(in.Quantity >= 1 && in.Quantity <= MaxQuantity, "quantity", fmt.Sprintf("must be between 1 and %d", MaxQuantity))
	if err := validateTurnbackFields(&v, in.DurationMinutes, in.Note, occurred, now, int(settings.LargeDurationMinutes), in.ConfirmLargeDuration); err != nil {
		return res, err
	}
	if in.Quantity > LargeQuantity && !in.ConfirmLargeDuration {
		return res, &domain.Error{Kind: domain.KindConfirmationRequired, Code: "quantity_confirmation_required",
			Message: fmt.Sprintf("%d occurrences at once is unusually many (over %d). Please confirm the number is correct.", in.Quantity, LargeQuantity),
			Fields:  []domain.FieldError{{Field: "quantity", Message: "requires confirmation"}}}
	}
	var batchID *uuid.UUID
	if in.Quantity > 1 {
		batchID = uuidPtr(in.ID)
	}
	tokenBytes := make([]byte, 18)
	if _, err := rand.Read(tokenBytes); err != nil {
		return res, err
	}
	undoToken := base64.RawURLEncoding.EncodeToString(tokenBytes)

	err = s.store.InTx(ctx, func(q *sqlc.Queries) error {
		problem, err := resolveProblemForSubmission(ctx, q, in.ProblemID)
		if err != nil {
			return err
		}
		if err := validTeam(ctx, q, in.TeamID); err != nil {
			return err
		}
		res.ProblemTitle = problem.Title
		params := sqlc.InsertTurnbackParams{
			ID: in.ID, ProblemID: problem.ID, Reporter: strPtr(a.Reporter), TeamID: in.TeamID,
			DurationMinutes: int32(in.DurationMinutes), DurationEstimated: in.DurationEstimated, Note: in.Note,
			OccurredAt: occurred, Source: in.Source, IsDemo: in.IsDemo, BatchID: batchID,
			UndoTokenHash: hashToken(undoToken), SubmittedAt: now,
		}
		tb, err := q.InsertTurnback(ctx, params)
		if err == nil {
			params.UndoTokenHash = nil
			for i := 1; i < in.Quantity; i++ {
				params.ID = BatchMemberID(in.ID, i)
				if _, err := q.InsertTurnback(ctx, params); err != nil {
					if store.IsNoRows(err) {
						return domain.Conflict("idempotency_conflict", "a turnback with a derived batch id already exists")
					}
					return err
				}
			}
			res.Turnback, res.Created, res.Quantity, res.UndoToken = tb, true, in.Quantity, undoToken
			if in.IsDemo {
				return nil
			}
			return s.emit(ctx, q, turnbackEvent(ctx, q, s.link, tb, problem.Title, in.Quantity))
		}
		if !store.IsNoRows(err) {
			return err
		}
		// ID already exists: idempotent replay or conflicting reuse.
		existing, err := q.GetTurnback(ctx, in.ID)
		if err != nil {
			return err
		}
		e := existing.Turnback
		same := e.ProblemID == problem.ID &&
			int(e.DurationMinutes) == in.DurationMinutes &&
			sameUUID(e.TeamID, in.TeamID) &&
			strPtrEq(e.Note, in.Note) &&
			(in.OccurredAt == nil || e.OccurredAt.Equal(occurred))
		if same {
			switch {
			case in.Quantity == 1:
				same = e.BatchID == nil
			case e.BatchID == nil || *e.BatchID != in.ID:
				same = false
			default:
				n, err := q.CountTurnbackBatch(ctx, &in.ID)
				if err != nil {
					return err
				}
				same = int(n) == in.Quantity
			}
		}
		if !same {
			return domain.Conflict("idempotency_conflict", "a different turnback with this id already exists")
		}
		res.Turnback, res.ProblemTitle, res.Created, res.Quantity = e, existing.ProblemTitle, false, in.Quantity
		return nil
	})
	if err != nil {
		return TurnbackResult{}, err
	}
	if res.Created {
		s.log.InfoContext(ctx, "turnback recorded", "turnback_id", res.Turnback.ID, "problem_id", res.Turnback.ProblemID,
			"source", res.Turnback.Source, "duration_minutes", res.Turnback.DurationMinutes, "quantity", res.Quantity)
	} else {
		s.log.InfoContext(ctx, "turnback submission replayed", "turnback_id", res.Turnback.ID)
	}
	return res, nil
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func strPtrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return strings.TrimSpace(*a) == strings.TrimSpace(*b)
}

// TurnbackUpdate corrects a previously recorded occurrence.
type TurnbackUpdate struct {
	ProblemID            uuid.UUID
	DurationMinutes      int
	DurationEstimated    bool
	Note                 *string
	OccurredAt           time.Time
	ConfirmLargeDuration bool
}

// UpdateTurnback corrects a turnback (administrators only).
func (s *Service) UpdateTurnback(ctx context.Context, a domain.Actor, id uuid.UUID, in TurnbackUpdate) (TurnbackResult, error) {
	var res TurnbackResult
	if err := requireAdmin(a); err != nil {
		return res, err
	}
	settings, _, err := s.Settings(ctx)
	if err != nil {
		return res, err
	}
	in.Note = trimPtr(in.Note)
	var v domain.Validator
	if err := validateTurnbackFields(&v, in.DurationMinutes, in.Note, in.OccurredAt.UTC(), s.now(), int(settings.LargeDurationMinutes), in.ConfirmLargeDuration); err != nil {
		return res, err
	}
	err = s.store.InTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.GetTurnback(ctx, id); err != nil {
			return notFoundOr(err, "turnback")
		}
		problem, err := resolveProblemForSubmission(ctx, q, in.ProblemID)
		if err != nil {
			return err
		}
		tb, err := q.UpdateTurnback(ctx, sqlc.UpdateTurnbackParams{
			ID: id, ProblemID: problem.ID, DurationMinutes: int32(in.DurationMinutes),
			DurationEstimated: in.DurationEstimated, Note: in.Note, OccurredAt: in.OccurredAt.UTC(),
		})
		if err != nil {
			return err
		}
		res = TurnbackResult{Turnback: tb, ProblemTitle: problem.Title, Quantity: 1}
		return nil
	})
	return res, err
}

// DeleteTurnback removes a report. Administrators may delete any report;
// otherwise the one-time undo token returned at submission is required and
// is only valid for UndoWindow. With wholeBatch, every occurrence recorded
// together with it is removed too.
func (s *Service) DeleteTurnback(ctx context.Context, a domain.Actor, id uuid.UUID, wholeBatch bool, undoToken string) error {
	existing, err := s.store.GetTurnback(ctx, id)
	if err != nil {
		return notFoundOr(err, "turnback")
	}
	t := existing.Turnback
	if !a.Admin {
		valid := undoToken != "" && t.UndoTokenHash != nil &&
			subtle.ConstantTimeCompare(hashToken(undoToken), t.UndoTokenHash) == 1 &&
			s.now().Sub(t.SubmittedAt) <= UndoWindow
		if !valid {
			return &domain.Error{Kind: domain.KindForbidden, Code: "undo_not_allowed",
				Message: "this report can no longer be undone; ask an administrator to correct it"}
		}
	}
	if wholeBatch && t.BatchID != nil {
		n, err := s.store.DeleteTurnbackBatch(ctx, t.BatchID)
		if err != nil {
			return err
		}
		s.log.InfoContext(ctx, "turnback batch deleted", "batch_id", *t.BatchID, "count", n, "by_admin", a.Admin)
		return nil
	}
	if _, err := s.store.DeleteTurnback(ctx, id); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "turnback deleted", "turnback_id", id, "by_admin", a.Admin)
	return nil
}
