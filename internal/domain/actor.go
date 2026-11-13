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

// Actor describes who is making a request. Turnbacks has no user accounts of
// its own: everyone may record and read reports. Who someone is comes from the
// configured identity source (network address by default, or a user header
// set by a single sign-on proxy); administration needs an admin client
// certificate or, with single sign-on, an admin user or group.
type Actor struct {
	// Reporter is a pseudonymous key identifying the source of a report, used
	// to count distinct people affected and, for administrators only, as an
	// audit trail: "ip:<address>" or "user:<sso user>" for web and CLI
	// requests, "slack:<team>/<user>" for Slack, and so on. Empty when unknown.
	Reporter string
	// User is the single sign-on user name when identity comes from a proxy
	// header; empty otherwise.
	User string
	// Admin is true when the request comes from an administrator.
	Admin bool
	// AdminName identifies the administrator in audit logs: the certificate
	// common name or the single sign-on user.
	AdminName string
	// AdminVia says how administration was granted: "certificate", "sso" or
	// "dev".
	AdminVia string
}
