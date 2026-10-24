package np4

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"Np4Protocol/go/pkg/message"
)

// TestEndToEndDeliveryAck pins the receipt round trip: SendTracked →
// receiver verifies → TypeAck travels back through the mix → sender's
// OnAck fires with the exact msg_id and the receiver's identity. Also pins
// the no-echo property: the ack never reaches the chat bus and triggers no
// counter-ack (that would loop forever).
func TestEndToEndDeliveryAck(t *testing.T) {
	net := newAuthNet(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Both sides need each other cached: rcv to verify+ack, snd to verify
	// the incoming ack against its contacts.
	if err := net.rcv.RefreshContacts(ctx); err != nil {
		t.Fatalf("rcv contacts: %v", err)
	}
	if err := net.snd.RefreshContacts(ctx); err != nil {
		t.Fatalf("snd contacts: %v", err)
	}

	type receipt struct {
		msgID []byte
		from  peer.ID
	}
	acks := make(chan receipt, 8)
	net.snd.OnAck(func(msgID []byte, from peer.ID) {
		acks <- receipt{msgID, from}
	})

	busMsgs := make(chan *message.Message, 8)
	net.snd.OnMessage(func(m *message.Message) { busMsgs <- m })

	msgID, err := net.snd.SendTracked(net.rcv.ID(), []byte("prove delivery"))
	if err != nil {
		t.Fatalf("SendTracked: %v", err)
	}

	// The receipt must name exactly this message and come from rcv.
	select {
	case got := <-acks:
		if !bytes.Equal(got.msgID, msgID) {
			t.Fatalf("ack named msg_id %x, want %x", got.msgID, msgID)
		}
		if got.from != net.rcv.ID() {
			t.Fatalf("ack from %s, want %s", got.from, net.rcv.ID())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no delivery receipt within 20s (mix round trip ≈ 2s)")
	}

	// No echo: nothing further on the ack channel, nothing binary on the bus.
	select {
	case extra := <-acks:
		t.Fatalf("spurious second receipt (ack loop?): %+v", extra)
	case <-busMsgs:
		t.Fatal("ack cell leaked into the chat bus")
	case <-time.After(3 * time.Second):
	}
}

// TestNoAckForUnverifiedSender: without verification there is no return
// address — an anonymous/unverified message must NOT produce a receipt
// (and the sender must not expect one).
func TestNoAckForUnverifiedSender(t *testing.T) {
	net := newAuthNet(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Sender cached (so the incoming ack would verify if one existed);
	// receiver's cache deliberately COLD: the arriving message is
	// unverified → no receipt is generated.
	if err := net.snd.RefreshContacts(ctx); err != nil {
		t.Fatalf("snd contacts: %v", err)
	}

	acks := make(chan []byte, 4)
	net.snd.OnAck(func(msgID []byte, from peer.ID) { acks <- msgID })

	if _, err := net.snd.SendTracked(net.rcv.ID(), []byte("no receipt for strangers")); err != nil {
		t.Fatalf("SendTracked: %v", err)
	}

	// The message itself must still arrive (degrade, never drop).
	got := awaitMessage(t, net.rcv, 20*time.Second)
	if string(got.Content) != "no receipt for strangers" || got.Verified {
		t.Fatalf("unexpected delivery: %+v", got)
	}
	select {
	case id := <-acks:
		t.Fatalf("unverified sender received a delivery receipt for %x", id)
	case <-time.After(4 * time.Second):
		// no ack, as required
	}
}
