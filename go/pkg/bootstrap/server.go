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
