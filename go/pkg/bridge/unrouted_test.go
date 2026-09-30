package bridge

import (
	"encoding/base64"
	"fmt"
	"sync"
	"testing"
)

// All the mix-mode methods must hard-fail on a direct-only node — the
// anonymity downgrade is always explicit, never implicit.
func TestUnroutedNodeHardFailsOnMixMethods(t *testing.T) {
	m := NewManager()
	res := startEphemeralNode(t, m)
	id := res["handle"].(int64)
	peer := res["peer_id"].(string)

	cases := map[string]string{
		"list_peers":   fmt.Sprintf(`{"method":"list_peers","args":{}}`),
		"publish_keys": `{"method":"publish_keys"}`,
		"serve_relay":  `{"method":"serve_relay"}`,
		"send": fmt.Sprintf(
			`{"method":"send","args":{"dest":"%s","content_b64":"aGk="}}`, peer),
	}
	for name, req := range cases {
		if _, err := m.Call(id, []byte(req)); err == nil {
			t.Errorf("%s on an unrouted node must fail", name)
		}
	}
}

// TestSendOversizeContentHardFails: content beyond the cell capacity must
// fail loudly through the bridge (chunking is [v2]) — never truncate or drop.
// On an unrouted node the mix-unavailable guard fires first (also correct);
// the cell-capacity bound itself is pinned by the cell package tests.
func TestSendOversizeContentHardFails(t *testing.T) {
	m := NewManager()
	res := startEphemeralNode(t, m)
	id := res["handle"].(int64)

	oversize := base64.StdEncoding.EncodeToString(make([]byte, 8192))
	_, err := m.Call(id, []byte(
		`{"method":"send","args":{"dest":"`+res["peer_id"].(string)+`","content_b64":"`+oversize+`"}}`))
	if err == nil {
		t.Fatal("oversize content accepted")
	}
	t.Logf("rejected with: %v", err)
}

// TestManagerConcurrentUse hammers Create/Call/Stop from many goroutines;
// run under -race this pins the Manager's locking. Handles are independent,
// so cross-talk would surface as wrong peer_ids or unknown-handle errors.
func TestManagerConcurrentUse(t *testing.T) {
	m := NewManager()
	const goroutines = 8

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			res, err := m.Create([]byte(`{}`))
			if err != nil {
				t.Errorf("g%d create: %v", g, err)
				return
			}
			handle := res["handle"].(int64)
			peer := res["peer_id"].(string)
			for i := 0; i < 5; i++ {
				info, err := m.Call(handle, []byte(`{"method":"info"}`))
				if err != nil {
					t.Errorf("g%d info: %v", g, err)
					return
				}
				if info["peer_id"] != peer {
					t.Errorf("g%d: handle returned foreign peer_id %v", g, info["peer_id"])
					return
				}
				if _, err := m.Call(handle, []byte(`{"method":"poll"}`)); err != nil {
					t.Errorf("g%d poll: %v", g, err)
					return
				}
			}
			if err := m.Stop(handle); err != nil {
				t.Errorf("g%d stop: %v", g, err)
			}
		}(g)
	}
	wg.Wait()
}
