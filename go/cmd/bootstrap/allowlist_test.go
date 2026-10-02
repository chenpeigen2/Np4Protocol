package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func testID(t *testing.T) peer.ID {
	t.Helper()
	priv, _, err := ic.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// TestAllowlistFailClosed pins the failure mode of the admission control: a
// configured but INVALID list must fail closed — nobody except the bootstrap
// is admitted — never silently open the network. A later-valid file is
// picked up by the hot-reload loop.
func TestAllowlistFailClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist")
	stranger := testID(t)
	self := testID(t)

	// Invalid initial content: a peer ID line that does not parse.
	if err := os.WriteFile(path, []byte("not-a-peer-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newAllowlistFile(path)
	a.self = self
	if !a.enabled() {
		t.Fatal("configured allowlist reported disabled")
	}
	if !a.loadFailed {
		t.Fatal("initial load failure not recorded")
	}
	if !a.admitted(self) {
		t.Fatal("bootstrap must always admit itself")
	}
	if a.admitted(stranger) {
		t.Fatal("FAIL-OPEN: corrupt list admitted an unlisted peer")
	}

	// Operator fixes the file; the hot-reload loop must admit the peer.
	a.watchReloads(context.Background(), 10*time.Millisecond)
	if err := os.WriteFile(path, []byte(stranger.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a.admitted(stranger) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("hot reload never admitted the fixed file's peer")
}

// TestAllowlistEmptyListFailsClosedToo: an operator configuring an EMPTY
// (but valid) file means "admit nobody yet" — that must be honored, not
// treated as a load failure.
func TestAllowlistEmptyListFailsClosedToo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allowlist")
	if err := os.WriteFile(path, []byte("# no peers yet\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newAllowlistFile(path)
	a.self = testID(t)
	if a.loadFailed {
		t.Fatal("valid empty file must not count as a load failure")
	}
	if !a.enabled() {
		t.Fatal("configured allowlist reported disabled")
	}
	if a.admitted(testID(t)) {
		t.Fatal("empty allowlist admitted an outsider")
	}
}
