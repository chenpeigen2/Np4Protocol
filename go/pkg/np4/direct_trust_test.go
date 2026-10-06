package np4

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/p2p"
)

// TestDirectCannotForgeVerification pins the direct-path trust boundary: the
// direct protocol carries NO sender authentication (there is no tag to
// check), so the wire-supplied Verified flag and SenderID MUST NOT reach the
// application. An attacker crafting {"SenderID": "<victim's friend>",
// "Verified": true} would otherwise get a forged "✓ 已验证" badge in the UI.
func TestDirectCannotForgeVerification(t *testing.T) {
	dir := t.TempDir()
	snd, err := NewNode(0, WithIdentity(filepath.Join(dir, "snd")))
	if err != nil {
		t.Fatalf("snd: %v", err)
	}
	defer snd.Close()
	rcv, err := NewNode(0, WithIdentity(filepath.Join(dir, "rcv")))
	if err != nil {
		t.Fatalf("rcv: %v", err)
	}
	defer rcv.Close()
	if err := snd.Connect(peer.AddrInfo{ID: rcv.ID(), Addrs: rcv.Host().Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	ch := make(chan *message.Message, 8)
	rcv.OnMessage(func(m *message.Message) { ch <- m })

	// SendDirect marshals message.Message verbatim — the closest an attacker
	// gets to injecting arbitrary wire JSON on the direct protocol.
	attacker := &message.Message{
		DestID:   rcv.ID().String(),
		SenderID: "12D3KooWImpersonatedFriend",
		Verified: true, // the forgery
		Content:  []byte("i claim to be your verified friend"),
	}
	if err := sendRawDirect(snd, rcv.ID(), attacker); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case m := <-ch:
		if m.Verified {
			t.Fatal("wire-supplied Verified=true reached the application: forged badge")
		}
		if m.SenderID == "12D3KooWImpersonatedFriend" && m.Verified {
			t.Fatal("unreachable")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("direct message not delivered at all")
	}
}

// TestDirectEmptyDestIsRejected pins the broadcast-injection guard: the
// direct protocol is strictly 1:1 (SendDirect always sets DestID). An empty
// DestID would be accepted by EVERY dialable node at once — an
// amplification channel with no legitimate sender.
func TestDirectEmptyDestIsRejected(t *testing.T) {
	dir := t.TempDir()
	snd, err := NewNode(0, WithIdentity(filepath.Join(dir, "snd")))
	if err != nil {
		t.Fatalf("snd: %v", err)
	}
	defer snd.Close()
	rcv, err := NewNode(0, WithIdentity(filepath.Join(dir, "rcv")))
	if err != nil {
		t.Fatalf("rcv: %v", err)
	}
	defer rcv.Close()
	if err := snd.Connect(peer.AddrInfo{ID: rcv.ID(), Addrs: rcv.Host().Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	delivered := make(chan *message.Message, 4)
	rcv.OnMessage(func(m *message.Message) { delivered <- m })

	if err := sendRawDirect(snd, rcv.ID(), &message.Message{
		DestID:   "", // broadcast claim: no legitimate sender produces this
		SenderID: "anonymous",
		Content:  []byte("spray every node"),
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case m := <-delivered:
		t.Fatalf("empty-DestID message delivered: %q", m.Content)
	case <-time.After(3 * time.Second):
		// dropped, as required
	}
}

// sendRawDirect writes an arbitrary crafted message over the direct protocol,
// bypassing SendDirect's field fixing — the attacker's exact capability.
func sendRawDirect(n *Node, dest peer.ID, msg *message.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return sendRawDirectJSON(n, dest, data)
}

func sendRawDirectJSON(n *Node, dest peer.ID, data []byte) error {
	ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
	defer cancel()
	s, err := n.host.NewStream(ctx, dest, ProtocolDirect)
	if err != nil {
		return err
	}
	defer s.Close()
	return p2p.WriteMsg(s, data)
}

// TestDirectIgnoresUnknownWireFields pins the rebuild defense: the direct
// handler unmarshals attacker-controlled JSON, so the wire can carry ANY
// extra fields (SessionKey, forged Verified, etc.). The handler must rebuild
// the message from honored fields only — unknown/forged wire content never
// reaches the application. (SessionKey was removed from message.Message
// entirely; the wire field is simply ignored on unmarshal.)
func TestDirectIgnoresUnknownWireFields(t *testing.T) {
	dir := t.TempDir()
	snd, err := NewNode(0, WithIdentity(filepath.Join(dir, "snd")))
	if err != nil {
		t.Fatalf("snd: %v", err)
	}
	defer snd.Close()
	rcv, err := NewNode(0, WithIdentity(filepath.Join(dir, "rcv")))
	if err != nil {
		t.Fatalf("rcv: %v", err)
	}
	defer rcv.Close()
	if err := snd.Connect(peer.AddrInfo{ID: rcv.ID(), Addrs: rcv.Host().Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	ch := make(chan *message.Message, 4)
	rcv.OnMessage(func(m *message.Message) { ch <- m })

	// Raw attacker JSON: forged Verified plus a SessionKey field that no
	// longer exists on the struct — both must be inert.
	raw := []byte(`{"DestID":"` + rcv.ID().String() +
		`","SenderID":"12D3KooWImpersonatedFriend","Verified":true,` +
		`"SessionKey":"attacker-chosen key material","Content":"cGF5bG9hZA=="}`)
	if err := sendRawDirectJSON(snd, rcv.ID(), raw); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case m := <-ch:
		if m.Verified {
			t.Fatal("forged Verified=true reached the application")
		}
		if string(m.Content) != "payload" {
			t.Fatalf("content corrupted: %q", m.Content)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("direct message not delivered")
	}
}
