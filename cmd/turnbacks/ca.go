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

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"turnbacks/internal/ca"
)

const caUsage = `usage:
  turnbacks ca init  [--dir ./ca] [--name "Turnbacks admin CA"] [--years 10]
  turnbacks ca issue [--dir ./ca] [--days 365] [--out <dir>/issued] <admin-name>

Keep ca.key on an administrator's machine. The server and Caddy need only ca.crt.`

var errCAUsage = errors.New("see usage above")

func runCA(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, caUsage)
		return errCAUsage
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("ca "+sub, flag.ContinueOnError)
	dir := fs.String("dir", "ca", "CA directory")
	switch sub {
	case "init":
		name := fs.String("name", "Turnbacks admin CA", "CA common name")
		years := fs.Int("years", 10, "CA validity in years")
		if err := fs.Parse(args); err != nil {
			return err
		}
		cert, err := ca.Init(*dir, *name, time.Duration(*years)*365*24*time.Hour)
		if err != nil {
			return err
		}
		fmt.Printf("Created CA %q (valid until %s)\n", cert.Subject.CommonName, cert.NotAfter.Format("2006-01-02"))
		fmt.Printf("  %s  private key: keep it secret, back it up, never copy it to the server\n", filepath.Join(*dir, ca.KeyFile))
		fmt.Printf("  %s  give this to Caddy and Turnbacks (TURNBACKS_ADMIN_CLIENT_CA)\n", filepath.Join(*dir, ca.CertFile))
		fmt.Printf("Next: turnbacks ca issue --dir %s <admin-name>\n", *dir)
		return nil
	case "issue":
		days := fs.Int("days", 365, "certificate validity in days")
		out := fs.String("out", "", "output directory (default <dir>/issued)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, caUsage)
			return errCAUsage
		}
		if *out == "" {
			*out = filepath.Join(*dir, "issued")
		}
		iss, err := ca.Issue(*dir, fs.Arg(0), time.Duration(*days)*24*time.Hour, *out)
		if err != nil {
			return err
		}
		fmt.Printf("Issued admin certificate for %q (valid until %s)\n", iss.Name, iss.NotAfter.Format("2006-01-02"))
		fmt.Printf("  bundle:   %s\n  password: %s\n", iss.P12Path, iss.P12Password)
		fmt.Printf("  serial:   %s\n  sha256:   %s\n", iss.Serial, iss.Fingerprint)
		fmt.Println("Send the bundle and the password to the administrator by different channels.")
		fmt.Println("They import it into their browser or keychain (docs/admin-certificates.md).")
		return nil
	}
	fmt.Fprintln(os.Stderr, caUsage)
	return fmt.Errorf("unknown ca command %q", sub)
}
