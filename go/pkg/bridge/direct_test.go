package bridge

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

// startEphemeralNode creates a bridge node and registers cleanup.
func startEphemeralNode(t *testing.T, m *Manager) map[string]any {
	t.Helper()
	res, err := m.Create([]byte(`{}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(res["handle"].(int64)) })
	return res
}

// TestSendDirectEndToEnd runs a real two-node exchange through the bridge
// with no DHT: create both nodes, dial B from A via the connect method,
// send_direct, and drain B's event queue. This is the exact path the CLI's
// --insecure mode uses; it must work with zero infrastructure.
func TestSendDirectEndToEnd(t *testing.T) {
	m := NewManager()
	a := startEphemeralNode(t, m)
	b := startEphemeralNode(t, m)

	// connect: A dials B using B's own advertised multiaddrs.
	bAddr := b["addrs"].([]string)[0] + "/p2p/" + b["peer_id"].(string)
	if _, err := m.Call(a["handle"].(int64), []byte(
		`{"method":"connect","args":{"addr":"`+bAddr+`"}}`)); err != nil {
		t.Fatalf("A connect to B: %v", err)
	}

	content := base64.StdEncoding.EncodeToString([]byte("hello direct"))
	if _, err := m.Call(a["handle"].(int64), []byte(
		`{"method":"send_direct","args":{"dest":"`+b["peer_id"].(string)+`","content_b64":"`+content+`"}}`)); err != nil {
		t.Fatalf("send_direct: %v", err)
	}

	// B drains its queue; the message must arrive with the sender REVEALED —
	// direct mode is the explicit anonymity downgrade, the sender is known.
	deadline := time.Now().Add(5 * time.Second)
	for {
		poll, err := m.Call(b["handle"].(int64), []byte(`{"method":"poll"}`))
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		rawEvents, _ := poll["events"].([]json.RawMessage)
		for _, raw := range rawEvents {
			var ev struct {
				Sender     string `json:"sender"`
				ContentB64 string `json:"content_b64"`
			}
			if err := json.Unmarshal(raw, &ev); err != nil {
				t.Fatalf("event json: %v", err)
			}
			if ev.Sender != a["peer_id"].(string) {
				t.Fatalf("direct message sender = %q, want revealed sender %s", ev.Sender, a["peer_id"])
			}
			got, err := base64.StdEncoding.DecodeString(ev.ContentB64)
			if err != nil || string(got) != "hello direct" {
				t.Fatalf("content corrupted across the bridge: %q (%v)", ev.ContentB64, err)
			}
			return // delivered
		}
		if time.Now().After(deadline) {
			t.Fatal("direct message never arrived at B")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestSendDirectToUnreachablePeerFailsFast: an unconnected destination has no
// addresses; the hard-fail must be quick, not burn the 30s send timeout.
func TestSendDirectToUnreachablePeerFailsFast(t *testing.T) {
	m := NewManager()
	a := startEphemeralNode(t, m)
	b := startEphemeralNode(t, m) // B exists but A never connected to it

	start := time.Now()
	_, err := m.Call(a["handle"].(int64), []byte(
		`{"method":"send_direct","args":{"dest":"`+b["peer_id"].(string)+`","content_b64":"aGk="}}`))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("send_direct to an unconnected peer must fail (no silent routing)")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("hard-fail took %s; unconnected peers should error immediately", elapsed)
	}
}

// TestWaitFlushedOnEmptyQueueIsImmediate: a node with nothing queued must
// return from wait_flushed without burning its timeout.
func TestWaitFlushedOnEmptyQueueIsImmediate(t *testing.T) {
	m := NewManager()
	res := startEphemeralNode(t, m)

	start := time.Now()
	if _, err := m.Call(res["handle"].(int64), []byte(`{"method":"wait_flushed","args":{"timeout_ms":30000}}`)); err != nil {
		t.Fatalf("wait_flushed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("empty wait_flushed took %s, want immediate", elapsed)
	}
}
