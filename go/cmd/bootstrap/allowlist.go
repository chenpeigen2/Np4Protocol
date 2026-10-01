package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// allowlistFile is the hot-reloaded admission list for a single-server
// deployment: one peer ID per line, '#' comments allowed. With no file
// configured admission is disabled and every node may publish — the default
// for development. Once a file is set, peers absent from it cannot publish
// keys (invisible in the directory, unusable as relay or destination).
//
// The operator's own bootstrap peer ID is always admitted regardless of the
// file: the bootstrap must publish its own relay key to function.
type allowlistFile struct {
	path    string
	current atomic.Pointer[map[peer.ID]struct{}]
	self    peer.ID
}

func newAllowlistFile(path string) *allowlistFile {
	a := &allowlistFile{path: path}
	if m, err := loadAllowlist(path); err != nil {
		fmt.Printf("[bootstrap] allowlist load failed (%v); admission starts DISABLED until the file is valid\n", err)
	} else {
		a.current.Store(&m)
	}
	return a
}

// admitted implements np4.WithAdmission.
func (a *allowlistFile) admitted(id peer.ID) bool {
	if a == nil {
		return true
	}
	// The operator's own bootstrap ID is always admitted — it must publish
	// its relay key to function. self is assigned right after NewNode
	// returns and before any publication (ServeRelay) can run.
	if a.self != "" && id == a.self {
		return true
	}
	m := a.current.Load()
	if m == nil {
		return true // no valid file loaded yet: fail open, reload loop may fix it
	}
	_, ok := (*m)[id]
	return ok
}

// enabled reports whether a list is actually in force (for startup logging).
func (a *allowlistFile) enabled() bool {
	m := a.current.Load()
	return m != nil
}

func (a *allowlistFile) size() int {
	if m := a.current.Load(); m != nil {
		return len(*m)
	}
	return 0
}

// watchReloads polls the file every interval and swaps the map on change.
// Polling (not fsnotify) keeps this dependency-free and survives editors
// that replace the file by rename.
func (a *allowlistFile) watchReloads(ctx context.Context, interval time.Duration) {
	if a == nil || a.path == "" {
		return
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
			m, err := loadAllowlist(a.path)
			if err != nil {
				continue // keep the previous list; logged at load/startup
			}
			cur := a.current.Load()
			if cur != nil && samePeerSet(*cur, m) {
				continue
			}
			a.current.Store(&m)
			fmt.Printf("[bootstrap] allowlist reloaded: %d peers admitted\n", len(m))
		}
	}()
}

func loadAllowlist(path string) (map[peer.ID]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := make(map[peer.ID]struct{})
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(strings.SplitN(sc.Text(), "#", 2)[0])
		if text == "" {
			continue
		}
		id, err := peer.Decode(text)
		if err != nil {
			return nil, fmt.Errorf("allowlist line %d: %w", line, err)
		}
		m[id] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

func samePeerSet(a, b map[peer.ID]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}
