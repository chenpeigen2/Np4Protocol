package bootstrap

import (
	"Np4Protocol/go/pkg/transport"
	"crypto/rand"
	"sync"
)

type PeerInfo struct {
	ID        string
	Addr      string
	PublicKey []byte
	Nonce     []byte
}

type BootstrapServer struct {
	transport transport.Transport
	listener  transport.Listener
	peers     map[string]*PeerInfo
	mu        sync.RWMutex
}

func NewBootstrapServer() (*BootstrapServer, error) {
	tcp := transport.NewTCPTransport()
	return &BootstrapServer{
		transport: tcp,
		peers:     make(map[string]*PeerInfo),
	}, nil
}

func (s *BootstrapServer) Start(addr string) error {
	listener, err := s.transport.Listen(addr)
	if err != nil {
		return err
	}
	s.listener = listener
	go s.acceptLoop()
	return nil
}

func (s *BootstrapServer) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
}

func (s *BootstrapServer) Addr() string {
	return s.listener.Addr().String()
}

func (s *BootstrapServer) PeerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.peers)
}

func (s *BootstrapServer) GetPeer(id string) (*PeerInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	peer, ok := s.peers[id]
	return peer, ok
}

func (s *BootstrapServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *BootstrapServer) handleConn(conn transport.Conn) {
	defer conn.Close()

	for {
		data, err := conn.Read()
		if err != nil {
			return
		}

		var msg BootstrapMessage
		if err := Deserialize(data, &msg); err != nil {
			return
		}

		switch msg.Type {
		case "register":
			s.handleRegister(conn, msg)
		case "key_exchange_request":
			s.handleKeyExchangeRequest(conn, msg)
		}
	}
}

func (s *BootstrapServer) handleRegister(conn transport.Conn, msg BootstrapMessage) {
	nonce := make([]byte, 16)
	rand.Read(nonce)

	s.mu.Lock()
	s.peers[msg.NodeID] = &PeerInfo{
		ID:        msg.NodeID,
		Addr:      msg.Addr,
		PublicKey: msg.PublicKey,
		Nonce:     nonce,
	}
	s.mu.Unlock()

	resp := BootstrapMessage{
		Type:    "register",
		Success: true,
		Nonce:   nonce,
	}
	data, _ := Serialize(resp)
	conn.Write(data)
}

func (s *BootstrapServer) handleKeyExchangeRequest(conn transport.Conn, msg BootstrapMessage) {
	s.mu.RLock()
	targetPeer, ok := s.peers[msg.TargetID]
	senderPeer, senderOk := s.peers[msg.NodeID]
	s.mu.RUnlock()

	if !ok || !senderOk {
		resp := BootstrapMessage{Type: "key_exchange_response", Success: false, Error: "peer not found"}
		data, _ := Serialize(resp)
		conn.Write(data)
		return
	}

	// Connect to target node and forward the request
	targetConn, err := s.transport.Connect(targetPeer.Addr)
	if err != nil {
		resp := BootstrapMessage{Type: "key_exchange_response", Success: false, Error: "target unreachable"}
		data, _ := Serialize(resp)
		conn.Write(data)
		return
	}
	defer targetConn.Close()

	// Forward request to target with sender's public key
	forwardMsg := BootstrapMessage{
		Type:      "key_exchange_request",
		NodeID:    msg.NodeID,
		Addr:      senderPeer.Addr,
		PublicKey: senderPeer.PublicKey,
		TargetID:  msg.TargetID,
	}
	forwardData, _ := Serialize(forwardMsg)
	targetConn.Write(forwardData)

	// Read target's response (its public key)
	respData, err := targetConn.Read()
	if err != nil {
		resp := BootstrapMessage{Type: "key_exchange_response", Success: false, Error: "target timeout"}
		data, _ := Serialize(resp)
		conn.Write(data)
		return
	}

	var targetResp BootstrapMessage
	if err := Deserialize(respData, &targetResp); err != nil {
		resp := BootstrapMessage{Type: "key_exchange_response", Success: false, Error: "invalid target response"}
		data, _ := Serialize(resp)
		conn.Write(data)
		return
	}

	// Forward target's public key back to requester
	replyMsg := BootstrapMessage{
		Type:      "key_exchange_response",
		NodeID:    msg.TargetID,
		Addr:      targetPeer.Addr,
		PublicKey: targetResp.PublicKey,
		Success:   true,
	}
	replyData, _ := Serialize(replyMsg)
	conn.Write(replyData)
}
