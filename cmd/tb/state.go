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
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Config is the user's CLI configuration (~/.config/turnbacks/config.json).
type Config struct {
	URL  string `json:"url,omitempty"`
	Team string `json:"team,omitempty"` // team ID
}

// LastReport remembers the most recent submission for `tb again` and `tb undo`.
type LastReport struct {
	ID        string    `json:"id"`
	ProblemID string    `json:"problem_id"`
	Problem   string    `json:"problem"`
	Minutes   int       `json:"minutes"`
	Quantity  int       `json:"quantity"`
	UndoToken string    `json:"undo_token,omitempty"`
	At        time.Time `json:"at"`
	// Undone is set by tb undo; the report is kept so tb again still works.
	Undone bool `json:"undone,omitempty"`
}

// Cache holds non-personal reference data for completion and offline use.
type Cache struct {
	FetchedAt  time.Time      `json:"fetched_at"`
	Problems   []CachedRecord `json:"problems"`
	Teams      []CachedRecord `json:"teams"`
	Categories []CachedRecord `json:"categories"`
}

// CachedRecord is a problem or team.
type CachedRecord struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Alias string `json:"alias,omitempty"`
}

func configDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "turnbacks")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "turnbacks")
}

func stateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "turnbacks")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "turnbacks")
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path) //nolint:gosec // paths are under the user's config/state directories
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// writeJSON writes atomically with user-only permissions.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadConfig() (Config, error) {
	var c Config
	err := readJSON(filepath.Join(configDir(), "config.json"), &c)
	if u := os.Getenv("TURNBACKS_URL"); u != "" {
		c.URL = u
	}
	return c, err
}

func saveConfig(c Config) error { return writeJSON(filepath.Join(configDir(), "config.json"), c) }

func loadLast() (*LastReport, error) {
	var l LastReport
	if err := readJSON(filepath.Join(stateDir(), "last.json"), &l); err != nil || l.ID == "" {
		return nil, err
	}
	return &l, nil
}

func saveLast(l LastReport) error { return writeJSON(filepath.Join(stateDir(), "last.json"), l) }

func loadQueue() ([]queuedSubmission, error) {
	var q []queuedSubmission
	err := readJSON(filepath.Join(stateDir(), "queue.json"), &q)
	return q, err
}

func saveQueue(q []queuedSubmission) error {
	if len(q) == 0 {
		err := os.Remove(filepath.Join(stateDir(), "queue.json"))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return writeJSON(filepath.Join(stateDir(), "queue.json"), q)
}

func loadCache() Cache {
	var c Cache
	_ = readJSON(filepath.Join(stateDir(), "cache.json"), &c)
	return c
}

func saveCache(c Cache) error { return writeJSON(filepath.Join(stateDir(), "cache.json"), c) }
