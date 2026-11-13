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

package domain

import "regexp"

// Problem statuses.
const (
	StatusActive     = "active"
	StatusMonitoring = "monitoring"
	StatusResolved   = "resolved"
	StatusArchived   = "archived"
)

// ValidProblemStatus reports whether s is a known problem status.
func ValidProblemStatus(s string) bool {
	switch s {
	case StatusActive, StatusMonitoring, StatusResolved, StatusArchived:
		return true
	}
	return false
}

// Submission sources.
const (
	SourceWeb        = "web"
	SourcePWAOffline = "pwa_offline"
	SourceAPI        = "api"
	SourceSlack      = "slack"
	SourceCLI        = "cli"
	SourceWebhook    = "webhook"
	SourceMattermost = "mattermost"
	SourceTeams      = "teams"
)

// ClientSubmittableSource reports whether an API client may claim the source.
// "slack", "webhook", "mattermost" and "teams" are reserved for the
// integrations themselves.
func ClientSubmittableSource(s string) bool {
	switch s {
	case SourceWeb, SourcePWAOffline, SourceAPI, SourceCLI:
		return true
	}
	return false
}

// Duration limits, in minutes.
const (
	MinDurationMinutes = 1
	MaxDurationMinutes = 1440
	MaxNoteLength      = 1000
)

var aliasRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidAlias reports whether s is a valid short alias (lowercase, digits, hyphens).
func ValidAlias(s string) bool { return aliasRe.MatchString(s) }

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

// ValidCurrency reports whether s looks like an ISO 4217 code.
func ValidCurrency(s string) bool { return currencyRe.MatchString(s) }
