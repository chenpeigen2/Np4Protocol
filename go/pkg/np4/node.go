package np4

import (
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/mix"
	"Np4Protocol/go/pkg/p2p"
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

const Np4MessageProtocol = protocol.ID("/np4/message/1.0.0")

type Node struct {
	host      host.Host
	bus       *message.MessageBus
	mixEngine *mix.MixEngine[message.Message]
	stopCh    chan struct{}
	stopOnce  sync.Once
}

func NewNode(port int) (*Node, error) {
	h, err := p2p.NewHost(port)
	if err != nil {
		return nil, err
	}

	n := &Node{
		host:   h,
		bus:    message.NewMessageBus(),
		stopCh: make(chan struct{}),
	}

	n.mixEngine = mix.NewMixEngine[message.Message](10, 500*time.Millisecond, n.sendBatch)

	// Register stream handler for incoming messages
	h.SetStreamHandler(Np4MessageProtocol, n.handleStream)

	return n, nil
}

func (n *Node) ID() peer.ID {
	return n.host.ID()
}

func (n *Node) Host() host.Host {
	return n.host
}

func (n *Node) Addrs() []string {
	addrs := n.host.Addrs()
	result := make([]string, len(addrs))
	for i, a := range addrs {
		result[i] = a.String() + "/p2p/" + n.host.ID().String()
	}
	return result
}

func (n *Node) OnMessage(handler func(*message.Message)) {
	n.bus.OnMessage(handler)
}

func (n *Node) Connect(addrs []multiaddr.Multiaddr, pid peer.ID) error {
	info := peer.AddrInfo{ID: pid, Addrs: addrs}
	return n.host.Connect(context.Background(), info)
}

func (n *Node) Send(destID peer.ID, content []byte) error {
	s, err := n.host.NewStream(context.Background(), destID, Np4MessageProtocol)
	if err != nil {
		return err
	}
	defer s.Close()

	msg := &message.Message{
		Type:     message.TypeAsync,
		SenderID: n.host.ID().String(),
		Content:  content,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return p2p.WriteMsg(s, data)
}

func (n *Node) Stop() {
	n.stopOnce.Do(func() {
		close(n.stopCh)
		n.host.Close()
	})
}

func (n *Node) handleStream(s network.Stream) {
	defer s.Close()

	data, err := p2p.ReadMsg(s)
	if err != nil {
		return
	}

	var msg message.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	n.bus.Send(&msg)
}

func (n *Node) sendBatch(batch []*message.Message) {
	for _, msg := range batch {
		n.bus.Send(msg)
	}
}
