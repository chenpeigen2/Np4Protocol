package bootstrap

import (
	"Np4Protocol/go/pkg/transport"
	"testing"
	"time"
)

func TestBootstrapServerRegister(t *testing.T) {
	server, err := NewBootstrapServer()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	err = server.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	tcp := transport.NewTCPTransport()
	conn, err := tcp.Connect(server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	regMsg := BootstrapMessage{
		Type:      "register",
		NodeID:    "node1",
		Addr:      "192.168.1.1:8080",
		PublicKey: []byte{1, 2, 3},
	}

	data, _ := Serialize(regMsg)
	conn.Write(data)

	time.Sleep(50 * time.Millisecond)

	if server.PeerCount() != 1 {
		t.Errorf("expected 1 peer, got %d", server.PeerCount())
	}

	peer, ok := server.GetPeer("node1")
	if !ok {
		t.Fatal("peer not found")
	}
	if peer.Addr != "192.168.1.1:8080" {
		t.Errorf("addr: got %s, want 192.168.1.1:8080", peer.Addr)
	}
}
