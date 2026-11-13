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
	"net/http"

	"turnbacks/internal/auth"
)

// SessionInfo tells the client who it is, as far as Turnbacks can tell, and
// whether (and how) administration is available.
type SessionInfo struct {
	// IdentityMode is "ip" (people are network addresses) or "header" (single
	// sign-on users).
	IdentityMode string `json:"identity_mode"`
	// User is the single sign-on user, when known.
	User         string `json:"user,omitempty"`
	Admin        bool   `json:"admin"`
	AdminName    string `json:"admin_name,omitempty"`
	AdminVia     string `json:"admin_via,omitempty"`
	AdminEnabled bool   `json:"admin_enabled"`
	// AdminMethods lists how administration can be granted: "certificate",
	// "sso".
	AdminMethods []string `json:"admin_methods"`
	SlackEnabled bool     `json:"slack_enabled"`
}

func (s *Server) sessionInfo(w http.ResponseWriter, r *http.Request) {
	a := auth.ActorFrom(r.Context())
	methods := []string{}
	if s.Admin.CertificatesEnabled() {
		methods = append(methods, auth.ViaCertificate)
	}
	if s.Admin.SSOEnabled() {
		methods = append(methods, auth.ViaSSO)
	}
	writeJSON(w, http.StatusOK, SessionInfo{
		IdentityMode: s.Admin.IdentityMode(), User: a.User,
		Admin: a.Admin, AdminName: a.AdminName, AdminVia: a.AdminVia, AdminEnabled: s.Admin.Enabled(), AdminMethods: methods,
		SlackEnabled: s.Slack != nil,
	})
}
