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

func TestBootstrapServerKeyExchange(t *testing.T) {
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

	// Start a mock Node B that listens for key exchange requests
	nodeBListener, err := tcp.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer nodeBListener.Close()
	nodeBAddr := nodeBListener.Addr().String()

	go func() {
		conn, err := nodeBListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		data, err := conn.Read()
		if err != nil {
			return
		}
		var reqMsg BootstrapMessage
		Deserialize(data, &reqMsg)

		// Respond with B's public key
		resp := BootstrapMessage{
			Type: "key_exchange_response", PublicKey: []byte{0xBB},
		}
		respData, _ := Serialize(resp)
		conn.Write(respData)
	}()

	// Node A registers
	connA, err := tcp.Connect(server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer connA.Close()
	regA := BootstrapMessage{
		Type: "register", NodeID: "nodeA", Addr: "10.0.0.1:8080",
		PublicKey: []byte{0xAA},
	}
	dataA, _ := Serialize(regA)
	connA.Write(dataA)
	time.Sleep(50 * time.Millisecond)

	// Drain Node A's register response
	connA.Read()

	// Node B registers
	connB, err := tcp.Connect(server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer connB.Close()
	regB := BootstrapMessage{
		Type: "register", NodeID: "nodeB", Addr: nodeBAddr,
		PublicKey: []byte{0xBB},
	}
	dataB, _ := Serialize(regB)
	connB.Write(dataB)
	time.Sleep(50 * time.Millisecond)

	// Node A requests key exchange with B
	reqMsg := BootstrapMessage{
		Type: "key_exchange_request", NodeID: "nodeA", TargetID: "nodeB",
	}
	reqData, _ := Serialize(reqMsg)
	connA.Write(reqData)

	time.Sleep(200 * time.Millisecond)

	// Node A should receive B's public key from bootstrap
	respData, err := connA.Read()
	if err != nil {
		t.Fatal("node A did not receive key exchange response")
	}

	var respMsg BootstrapMessage
	Deserialize(respData, &respMsg)

	if respMsg.Type != "key_exchange_response" {
		t.Errorf("expected key_exchange_response, got %s", respMsg.Type)
	}
	if !respMsg.Success {
		t.Errorf("expected success, got error: %s", respMsg.Error)
	}
	if string(respMsg.PublicKey) != string([]byte{0xBB}) {
		t.Error("public key mismatch")
	}
}

func TestBootstrapListPeers(t *testing.T) {
	server, _ := NewBootstrapServer()
	defer server.Stop()
	server.Start("127.0.0.1:0")

	tcp := transport.NewTCPTransport()

	// Register node A
	connA, _ := tcp.Connect(server.Addr())
	defer connA.Close()
	regA := BootstrapMessage{Type: "register", NodeID: "nodeA", Addr: "10.0.0.1:8080", PublicKey: []byte{0xAA}}
	dataA, _ := Serialize(regA)
	connA.Write(dataA)
	time.Sleep(50 * time.Millisecond)

	// Register node B
	connB, _ := tcp.Connect(server.Addr())
	defer connB.Close()
	regB := BootstrapMessage{Type: "register", NodeID: "nodeB", Addr: "10.0.0.2:8080", PublicKey: []byte{0xBB}}
	dataB, _ := Serialize(regB)
	connB.Write(dataB)
	time.Sleep(50 * time.Millisecond)

	// Node A lists peers (new connection)
	tcp2 := transport.NewTCPTransport()
	listConn, _ := tcp2.Connect(server.Addr())
	defer listConn.Close()
	listMsg := BootstrapMessage{Type: "list_peers", NodeID: "nodeA"}
	listData, _ := Serialize(listMsg)
	listConn.Write(listData)

	respData, err := listConn.Read()
	if err != nil {
		t.Fatal("no response")
	}

	var respMsg BootstrapMessage
	Deserialize(respData, &respMsg)

	if respMsg.Type != "list_peers" {
		t.Errorf("expected list_peers, got %s", respMsg.Type)
	}
	if !respMsg.Success {
		t.Error("expected success")
	}
	if len(respMsg.Peers) != 1 {
		t.Errorf("expected 1 peer, got %d", len(respMsg.Peers))
	}
	if respMsg.Peers[0].ID != "nodeB" {
		t.Errorf("expected nodeB, got %s", respMsg.Peers[0].ID)
	}
}
