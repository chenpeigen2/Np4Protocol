package bootstrap

import "encoding/json"

type BootstrapMessage struct {
    Type      string     `json:"type"`
    NodeID    string     `json:"node_id"`
    Addr      string     `json:"addr"`
    PublicKey  []byte     `json:"public_key"`
    TargetID  string     `json:"target_id,omitempty"`
    Nonce     []byte     `json:"nonce,omitempty"`
    Success   bool       `json:"success"`
    Approved  bool       `json:"approved,omitempty"`
    Error     string     `json:"error,omitempty"`
    Peers     []PeerInfo `json:"peers,omitempty"`
}

func Serialize(msg BootstrapMessage) ([]byte, error) {
    return json.Marshal(msg)
}

func Deserialize(data []byte, msg *BootstrapMessage) error {
    return json.Unmarshal(data, msg)
}
