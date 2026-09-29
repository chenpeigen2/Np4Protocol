package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCreateEphemeralNode covers the bridge lifecycle without any network:
// an ephemeral node (no bootstrap, no identity file) is created, introspected,
// polled, and stopped.
func TestCreateEphemeralNode(t *testing.T) {
	m := NewManager()

	res, err := m.Create([]byte(`{}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := res["handle"].(int64)
	if id <= 0 {
		t.Fatalf("bad handle: %v", id)
	}
	if res["peer_id"].(string) == "" {
		t.Fatal("empty peer_id")
	}
	if len(res["addrs"].([]string)) == 0 {
		t.Fatal("no listen addrs")
	}

	info, err := m.Call(id, []byte(`{"method":"info"}`))
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info["peer_id"] != res["peer_id"] {
		t.Fatalf("peer_id mismatch: %v vs %v", info["peer_id"], res["peer_id"])
	}

	poll, err := m.Call(id, []byte(`{"method":"poll"}`))
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if events := poll["events"].([]json.RawMessage); len(events) != 0 {
		t.Fatalf("expected empty poll, got %v", events)
	}

	if err := m.Stop(id); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := m.Call(id, []byte(`{"method":"info"}`)); err == nil {
		t.Fatal("call after stop must fail")
	}
}

// TestSendWithoutRoutingHardFails pins the anonymity contract at the bridge
// boundary: a node without DHT must refuse mix sends with a hard error, never
// silently fall back. The destination is a real peer ID (a second ephemeral
// node's) so the failure is the routing check, not peer-ID validation.
func TestSendWithoutRoutingHardFails(t *testing.T) {
	m := NewManager()
	res, err := m.Create([]byte(`{}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := res["handle"].(int64)
	t.Cleanup(func() { _ = m.Stop(id) })

	dest, err := m.Create([]byte(`{}`))
	if err != nil {
		t.Fatalf("create dest: %v", err)
	}
	destID := dest["handle"].(int64)
	t.Cleanup(func() { _ = m.Stop(destID) })
	destPeer := dest["peer_id"].(string)

	_, err = m.Call(id, []byte(`{"method":"send","args":{"dest":"`+destPeer+`","content_b64":"aGk="}}`))
	if err == nil {
		t.Fatal("send without routing must fail")
	}
	if !strings.Contains(err.Error(), "mix unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCreateRejectsBadInput covers config validation at the boundary.
func TestCreateRejectsBadInput(t *testing.T) {
	m := NewManager()

	if _, err := m.Create([]byte(`not-json`)); err == nil {
		t.Fatal("bad config JSON must fail")
	}
	// Valid JSON but a garbage multiaddr.
	if _, err := m.Create([]byte(`{"bootstrap":"not-a-multiaddr"}`)); err == nil {
		t.Fatal("bad bootstrap multiaddr must fail")
	}
}

// TestUnknownMethodAndHandle covers method dispatch errors.
func TestUnknownMethodAndHandle(t *testing.T) {
	m := NewManager()
	res, err := m.Create([]byte(`{}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := res["handle"].(int64)
	t.Cleanup(func() { _ = m.Stop(id) })

	if _, err := m.Call(id, []byte(`{"method":"teleport"}`)); err == nil {
		t.Fatal("unknown method must fail")
	}
	if _, err := m.Call(99999, []byte(`{"method":"info"}`)); err == nil {
		t.Fatal("unknown handle must fail")
	}
}

// TestEventQueueEviction verifies the bounded queue drops oldest first and
// reports the drop count per drain.
func TestEventQueueEviction(t *testing.T) {
	q := newEventQueue(3)
	for i := 0; i < 5; i++ {
		q.push(map[string]any{"i": i})
	}
	if q.len() != 3 {
		t.Fatalf("queue exceeded capacity: %d", q.len())
	}
	events, dropped := q.drain()
	if dropped != 2 {
		t.Fatalf("dropped = %d, want 2", dropped)
	}
	// Oldest two (i=0, i=1) were evicted; first survivor is i=2.
	var first struct {
		I int `json:"i"`
	}
	if err := json.Unmarshal(events[0], &first); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if first.I != 2 {
		t.Fatalf("first event = %d, want 2 (oldest evicted)", first.I)
	}
	// Drop counter resets per drain.
	if _, dropped := q.drain(); dropped != 0 {
		t.Fatalf("drop counter did not reset: %d", dropped)
	}
}

// TestEnvelopeShape pins the wire envelope every FFI caller depends on.
func TestEnvelopeShape(t *testing.T) {
	okEnv := Envelope(map[string]any{"x": 1}, nil)
	if !strings.Contains(string(okEnv), `"ok":true`) || !strings.Contains(string(okEnv), `"result"`) {
		t.Fatalf("bad ok envelope: %s", okEnv)
	}
	errEnv := Envelope(nil, errUnknownHandle)
	var env map[string]any
	if err := json.Unmarshal(errEnv, &env); err != nil {
		t.Fatalf("error envelope is not JSON: %v", err)
	}
	if env["ok"] != false || env["error"] == "" {
		t.Fatalf("bad error envelope: %s", errEnv)
	}
}
