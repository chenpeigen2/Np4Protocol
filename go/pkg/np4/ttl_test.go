package np4

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/p2p"

	"Np4Protocol/go/pkg/cell"
	"Np4Protocol/go/pkg/onion"
)

// TestRelayDropsCraftedOverlongTTL pins the inbound TTL ceiling: honest
// senders cap the initial TTL at MaxInitialTTL, so a relay receiving ttl
// above that must drop BEFORE any crypto work — otherwise one 8KB packet
// with ttl=255 buys the attacker 255 decrypt rounds and 255 mix round-trips
// across a looped path.
func TestRelayDropsCraftedOverlongTTL(t *testing.T) {
	net := newAuthNet(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := net.rcv.RefreshContacts(ctx); err != nil {
		t.Fatalf("RefreshContacts: %v", err)
	}

	buildPacket := func(ttl uint8) ([]byte, error) {
		msgID, err := cell.NewMsgID()
		if err != nil {
			return nil, err
		}
		c, err := cell.Seal(msgID, cell.TypeText, make([]byte, cell.TagSize), []byte("loop bomb"))
		if err != nil {
			return nil, err
		}
		destPub, err := net.snd.lookupDestPub(net.rcv.ID())
		if err != nil {
			return nil, err
		}
		hops := []onion.Hop{
			{PeerID: net.boot.ID(), ECDHPub: net.boot.identity.ECDHPub()},
			{PeerID: net.rcv.ID(), ECDHPub: destPub},
		}
		on, err := onion.Build(hops, c)
		if err != nil {
			return nil, err
		}
		return onion.Wrap(ttl, on.Bytes())
	}

	deliver := make(chan string, 8)
	net.rcv.OnMessage(func(m *message.Message) { deliver <- string(m.Content) })

	// Control: the same onion with a legitimate ttl=1 is delivered.
	legit, err := buildPacket(1)
	if err != nil {
		t.Fatalf("build legit packet: %v", err)
	}
	if err := sendWire(net.snd, net.boot.ID(), legit); err != nil {
		t.Fatalf("send legit: %v", err)
	}
	select {
	case got := <-deliver:
		if got != "loop bomb" {
			t.Fatalf("control delivery corrupted: %q", got)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("control packet with legit ttl was not delivered")
	}

	// Attack: identical onion wrapped with a crafted ttl=200 must die at the
	// first relay — never decrypted, never forwarded, never delivered.
	bomb, err := buildPacket(200)
	if err != nil {
		t.Fatalf("build crafted packet: %v", err)
	}
	if err := sendWire(net.snd, net.boot.ID(), bomb); err != nil {
		t.Fatalf("send crafted: %v", err)
	}
	select {
	case got := <-deliver:
		t.Fatalf("crafted ttl=200 packet was delivered: %q", got)
	case <-time.After(4 * time.Second):
		// The mix delay window has passed with no delivery: dropped.
	}
}

// sendWire opens an onion-protocol stream to firstHop and writes a raw wire
// packet, mimicking a (possibly malicious) peer.
func sendWire(n *Node, firstHop peer.ID, wire []byte) error {
	ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
	defer cancel()
	s, err := n.host.NewStream(ctx, firstHop, ProtocolOnion)
	if err != nil {
		return err
	}
	defer s.Close()
	return p2p.WriteMsg(s, wire)
}
