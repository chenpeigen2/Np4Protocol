package np4

import (
	"Np4Protocol/go/pkg/bootstrap"
	"Np4Protocol/go/pkg/crypto"
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/mix"
	"Np4Protocol/go/pkg/router"
	"Np4Protocol/go/pkg/transport"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

type PeerSession struct {
	PeerID    string
	PeerAddr  string
	SharedKey []byte
	CreatedAt time.Time
}

type Node struct {
	id         string
	transport  transport.Transport
	crypto     *crypto.ChaCha20Encryptor
	keyExch    *crypto.X25519KeyExchange
	router     *router.Router
	mixEngine  *mix.MixEngine[message.Message]
	bus        *message.MessageBus
	listener   transport.Listener
	publicKey  []byte
	privateKey []byte
	peerKeys   map[string]*PeerSession
	stopCh     chan struct{}
	stopOnce   sync.Once
	mu         sync.RWMutex
}

func NewNode(listenAddr string) (*Node, error) {
	idBytes := make([]byte, 16)
	_, err := rand.Read(idBytes)
	if err != nil {
		return nil, err
	}
	id := hex.EncodeToString(idBytes)

	tcp := transport.NewTCPTransport()
	listener, err := tcp.Listen(listenAddr)
	if err != nil {
		return nil, err
	}

	n := &Node{
		id:        id,
		transport: tcp,
		crypto:    crypto.NewChaCha20Encryptor(),
		keyExch:   crypto.NewX25519KeyExchange(),
		router:    router.NewRouter(),
		bus:       message.NewMessageBus(),
		listener:  listener,
		peerKeys:  make(map[string]*PeerSession),
		stopCh:    make(chan struct{}),
	}

	n.mixEngine = mix.NewMixEngine[message.Message](10, 500*time.Millisecond, n.sendBatch)

	go n.acceptLoop()

	return n, nil
}

func (n *Node) ID() string {
	return n.id
}

func (n *Node) Addr() string {
	return n.listener.Addr().String()
}

func (n *Node) AddPeer(id, addr string) {
	n.router.AddNode(&router.Node{ID: id, Addr: addr})
}

func (n *Node) OnMessage(handler func(*message.Message)) {
	n.bus.OnMessage(handler)
}

func (n *Node) Send(destID string, content []byte) error {
	node, ok := n.router.GetNode(destID)
	if !ok {
		return errors.New("unknown destination")
	}

	conn, err := n.transport.Connect(node.Addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	msg := &message.Message{
		Type:     message.TypeAsync,
		DestID:   destID,
		SenderID: n.id,
		Content:  content,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return conn.Write(data)
}

func (n *Node) Stop() {
	n.stopOnce.Do(func() {
		close(n.stopCh)
		n.listener.Close()
	})
}

func (n *Node) Register(bootstrapAddr string) error {
	conn, err := n.transport.Connect(bootstrapAddr)
	if err != nil {
		return err
	}

	pubKey, privKey, err := n.keyExch.GenerateKeyPair()
	if err != nil {
		conn.Close()
		return err
	}

	n.mu.Lock()
	n.publicKey = pubKey
	n.privateKey = privKey
	n.mu.Unlock()

	msg := bootstrap.BootstrapMessage{
		Type:      "register",
		NodeID:    n.id,
		Addr:      n.listener.Addr().String(),
		PublicKey: pubKey,
	}

	data, err := bootstrap.Serialize(msg)
	if err != nil {
		conn.Close()
		return err
	}

	conn.Write(data)
	conn.Close()
	return nil
}

func (n *Node) acceptLoop() {
	for {
		select {
		case <-n.stopCh:
			return
		default:
		}

		conn, err := n.listener.Accept()
		if err != nil {
			continue
		}

		go n.handleConn(conn)
	}
}

func (n *Node) handleConn(conn transport.Conn) {
	defer conn.Close()

	data, err := conn.Read()
	if err != nil {
		return
	}

	// Try to detect bootstrap messages by checking the "type" field
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &probe) == nil && probe.Type == "key_exchange_request" {
		var bsMsg bootstrap.BootstrapMessage
		if bootstrap.Deserialize(data, &bsMsg) != nil {
			return
		}
		n.mu.RLock()
		pubKey := n.publicKey
		privKey := n.privateKey
		n.mu.RUnlock()

		// Compute shared secret with requester
		sharedKey, err := n.keyExch.ComputeSharedSecret(privKey, bsMsg.PublicKey)
		if err == nil {
			n.mu.Lock()
			n.peerKeys[bsMsg.NodeID] = &PeerSession{
				PeerID:    bsMsg.NodeID,
				PeerAddr:  bsMsg.Addr,
				SharedKey: sharedKey,
				CreatedAt: time.Now(),
			}
			n.mu.Unlock()
		}

		resp := bootstrap.BootstrapMessage{
			Type:      "key_exchange_response",
			NodeID:    n.id,
			PublicKey: pubKey,
			Success:   true,
		}
		respData, _ := bootstrap.Serialize(resp)
		conn.Write(respData)
		return
	}

	var msg message.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	n.bus.Send(&msg)
}

func (n *Node) ExchangeKeys(bootstrapAddr, peerID string) error {
	conn, err := n.transport.Connect(bootstrapAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	n.mu.RLock()
	pubKey := n.publicKey
	n.mu.RUnlock()

	reqMsg := bootstrap.BootstrapMessage{
		Type:      "key_exchange_request",
		NodeID:    n.id,
		TargetID:  peerID,
		PublicKey: pubKey,
	}

	data, err := bootstrap.Serialize(reqMsg)
	if err != nil {
		return err
	}
	if err := conn.Write(data); err != nil {
		return err
	}

	// Read response with peer's public key
	respData, err := conn.Read()
	if err != nil {
		return err
	}

	var respMsg bootstrap.BootstrapMessage
	if err := bootstrap.Deserialize(respData, &respMsg); err != nil {
		return err
	}

	if !respMsg.Success {
		return errors.New("key exchange failed: " + respMsg.Error)
	}

	// Compute shared secret
	n.mu.RLock()
	privKey := n.privateKey
	n.mu.RUnlock()

	sharedKey, err := n.keyExch.ComputeSharedSecret(privKey, respMsg.PublicKey)
	if err != nil {
		return err
	}

	n.mu.Lock()
	n.peerKeys[peerID] = &PeerSession{
		PeerID:    peerID,
		PeerAddr:  respMsg.Addr,
		SharedKey: sharedKey,
		CreatedAt: time.Now(),
	}
	n.mu.Unlock()

	return nil
}

func (n *Node) GetPeerSession(peerID string) (*PeerSession, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	session, ok := n.peerKeys[peerID]
	return session, ok
}

func (n *Node) sendBatch(batch []*message.Message) {
	for _, msg := range batch {
		n.bus.Send(msg)
	}
}
