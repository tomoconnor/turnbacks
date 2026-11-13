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

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"turnbacks/internal/domain"
	"turnbacks/internal/logx"
)

const maxBodyBytes = 1 << 20

// ErrorBody is the consistent error envelope returned by every endpoint.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail describes an API error.
type ErrorDetail struct {
	Code      string              `json:"code"`
	Message   string              `json:"message"`
	RequestID string              `json:"request_id,omitempty"`
	Fields    []domain.FieldError `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeErr(w http.ResponseWriter, r *http.Request, status int, code, msg string, fields []domain.FieldError) {
	writeJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: msg, RequestID: logx.RequestID(r.Context()), Fields: fields}})
}

// errorResponder maps errors to HTTP responses.
type errorResponder struct{ log *slog.Logger }

func (e errorResponder) respond(w http.ResponseWriter, r *http.Request, err error) {
	if de, ok := domain.AsError(err); ok {
		status := http.StatusInternalServerError
		switch de.Kind {
		case domain.KindInvalid, domain.KindConfirmationRequired:
			status = http.StatusUnprocessableEntity
		case domain.KindNotFound:
			status = http.StatusNotFound
		case domain.KindConflict:
			status = http.StatusConflict
		case domain.KindForbidden:
			status = http.StatusForbidden
		case domain.KindUnauthenticated:
			status = http.StatusUnauthorized
		}
		if status != http.StatusInternalServerError {
			writeErr(w, r, status, de.Code, de.Message, de.Fields)
			return
		}
	}
	var bre *badRequestError
	if errors.As(err, &bre) {
		writeErr(w, r, http.StatusBadRequest, "bad_request", bre.msg, nil)
		return
	}
	if r.Context().Err() != nil {
		writeErr(w, r, http.StatusServiceUnavailable, "timeout", "the request timed out or was cancelled", nil)
		return
	}
	e.log.ErrorContext(r.Context(), "request failed", "error", err, "method", r.Method, "path", r.URL.Path)
	writeErr(w, r, http.StatusInternalServerError, "internal", "an internal error occurred", nil)
}

type badRequestError struct{ msg string }

func (b *badRequestError) Error() string { return b.msg }

func badRequest(format string, args ...any) error {
	return &badRequestError{msg: fmt.Sprintf(format, args...)}
}

// decode reads a JSON body strictly (unknown fields rejected, size limited).
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "application/json" {
		return badRequest("Content-Type must be application/json")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			return badRequest("request body too large")
		case errors.Is(err, io.EOF):
			return badRequest("request body is empty")
		default:
			var ute *json.UnmarshalTypeError
			if errors.As(err, &ute) {
				return &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid field type",
					Fields: []domain.FieldError{{Field: ute.Field, Message: "must be of type " + ute.Type.String()}}}
			}
			return badRequest("invalid JSON: %s", strings.TrimPrefix(err.Error(), "json: "))
		}
	}
	if dec.More() {
		return badRequest("request body must contain a single JSON object")
	}
	return nil
}

func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, domain.NotFound("resource")
	}
	return id, nil
}

func queryUUID(r *http.Request, name string) (*uuid.UUID, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid query parameter",
			Fields: []domain.FieldError{{Field: name, Message: "must be a UUID"}}}
	}
	return &id, nil
}

func queryInt(r *http.Request, name string, def int) (int, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid query parameter",
			Fields: []domain.FieldError{{Field: name, Message: "must be an integer"}}}
	}
	return n, nil
}

func queryInt64Ptr(r *http.Request, name string) (*int64, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, &domain.Error{Kind: domain.KindInvalid, Code: "validation_failed", Message: "invalid query parameter",
			Fields: []domain.FieldError{{Field: name, Message: "must be an integer"}}}
	}
	return &n, nil
}
