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

// Package domain holds core types, validation rules and error kinds shared by
// the service, API and integration layers. It has no persistence or HTTP
// dependencies.
package domain

import (
	"errors"
	"fmt"
)

// Kind classifies an error so transport layers can map it consistently.
type Kind int

const (
	KindInternal Kind = iota
	KindInvalid
	KindNotFound
	KindConflict
	KindForbidden
	KindUnauthenticated
	// KindConfirmationRequired signals that the request is valid but needs an
	// explicit confirmation flag (e.g. unusually large durations).
	KindConfirmationRequired
)

// FieldError describes a validation problem with a specific input field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error is the domain error type. Code is a stable machine-readable string.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	Fields  []FieldError
}

func (e *Error) Error() string {
	if len(e.Fields) > 0 {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Fields)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Errorf constructs a domain error.
func Errorf(kind Kind, code, format string, args ...any) *Error {
	return &Error{Kind: kind, Code: code, Message: fmt.Sprintf(format, args...)}
}

// NotFound is a convenience constructor.
func NotFound(what string) *Error {
	return &Error{Kind: KindNotFound, Code: "not_found", Message: what + " not found"}
}

// Forbidden is a convenience constructor.
func Forbidden(msg string) *Error {
	return &Error{Kind: KindForbidden, Code: "forbidden", Message: msg}
}

// Conflict is a convenience constructor.
func Conflict(code, msg string) *Error {
	return &Error{Kind: KindConflict, Code: code, Message: msg}
}

// AsError extracts a *Error from err, if any.
func AsError(err error) (*Error, bool) {
	var de *Error
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}

// IsKind reports whether err is a domain error of the given kind.
func IsKind(err error, kind Kind) bool {
	de, ok := AsError(err)
	return ok && de.Kind == kind
}

// Validator accumulates field errors.
type Validator struct {
	fields []FieldError
}

// Add records a field error.
func (v *Validator) Add(field, msg string) {
	v.fields = append(v.fields, FieldError{Field: field, Message: msg})
}

// Check records a field error when ok is false.
func (v *Validator) Check(ok bool, field, msg string) {
	if !ok {
		v.Add(field, msg)
	}
}

// Err returns a validation error, or nil when there were no problems.
func (v *Validator) Err() error {
	if len(v.fields) == 0 {
		return nil
	}
	return &Error{Kind: KindInvalid, Code: "validation_failed", Message: "one or more fields are invalid", Fields: v.fields}
}
