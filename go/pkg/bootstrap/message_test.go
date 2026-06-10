package bootstrap

import (
    "testing"
)

func TestBootstrapMessageSerialize(t *testing.T) {
    msg := BootstrapMessage{
        Type:     "register",
        NodeID:   "node1",
        Addr:     "192.168.1.1:8080",
        PublicKey: []byte{1, 2, 3, 4},
    }

    data, err := Serialize(msg)
    if err != nil {
        t.Fatal(err)
    }

    var decoded BootstrapMessage
    err = Deserialize(data, &decoded)
    if err != nil {
        t.Fatal(err)
    }

    if decoded.Type != msg.Type {
        t.Errorf("Type: got %s, want %s", decoded.Type, msg.Type)
    }
    if decoded.NodeID != msg.NodeID {
        t.Errorf("NodeID: got %s, want %s", decoded.NodeID, msg.NodeID)
    }
    if decoded.Addr != msg.Addr {
        t.Errorf("Addr: got %s, want %s", decoded.Addr, msg.Addr)
    }
}
