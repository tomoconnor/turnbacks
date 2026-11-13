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

// Command devport is a tiny development helper used by scripts/dev.sh.
//
//	devport free <start> [max]   print the first port >= start that is free
//	devport wait <port> <secs>   exit 0 once something accepts connections on the port
//
// A port is considered free only if nothing accepts connections on it (IPv4
// or IPv6 loopback) *and* it can be bound. Both checks are needed: on macOS a
// specific-address bind can succeed even when another process holds a
// wildcard bind on the same port.
package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

func listening(port int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 300*time.Millisecond) //nolint:gosec // loopback-only dev probe
		if err == nil {
			_ = c.Close()
			return true
		}
	}
	return false
}

func bindable(port int) bool {
	for _, addr := range []string{"127.0.0.1", ""} {
		l, err := net.Listen("tcp", net.JoinHostPort(addr, strconv.Itoa(port)))
		if err != nil {
			return false
		}
		_ = l.Close()
	}
	return true
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "devport: "+format+"\n", args...)
	os.Exit(1)
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		fail("invalid port %q", s)
	}
	return n
}

func main() {
	if len(os.Args) < 3 {
		fail("usage: devport free <start> [max] | devport wait <port> <seconds>")
	}
	switch os.Args[1] {
	case "free":
		start := atoi(os.Args[2])
		limit := 100
		if len(os.Args) > 3 {
			limit = atoi(os.Args[3])
		}
		for p := start; p < start+limit && p <= 65535; p++ {
			if !listening(p) && bindable(p) {
				fmt.Println(p)
				return
			}
		}
		fail("no free port in %d..%d", start, start+limit-1)
	case "wait":
		port := atoi(os.Args[2])
		secs, err := strconv.Atoi(os.Args[3])
		if err != nil {
			fail("invalid seconds %q", os.Args[3])
		}
		deadline := time.Now().Add(time.Duration(secs) * time.Second)
		for time.Now().Before(deadline) {
			if listening(port) {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		os.Exit(1)
	default:
		fail("unknown command %q", os.Args[1])
	}
}
