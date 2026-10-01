package p2p

import (
	"encoding/base32"
	"testing"

	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// keyFor builds the full record key as stored in the DHT. (Inner validators
// receive this full form — NamespacedValidator dispatches on the first
// segment but passes the key through unchanged.)
func keyFor(t *testing.T, pid peer.ID) string {
	t.Helper()
	return "/np4/ecdh/" + base32.StdEncoding.EncodeToString([]byte(pid))
}

func TestNp4ValidatorAcceptsBoundKey(t *testing.T) {
	priv, _, err := ic.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ic.MarshalPublicKey(priv.GetPublic())
	if err != nil {
		t.Fatal(err)
	}
	if err := (np4Validator{}).Validate(keyFor(t, pid), value); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}
}

// TestNp4ValidatorRejectsPoisoning: the load-bearing anti-poisoning test — a
// record signed by an attacker under a victim's key must be rejected. The DHT
// never verifies signatures, so this binding is the only defense.
func TestNp4ValidatorRejectsPoisoning(t *testing.T) {
	attacker, _, err := ic.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	victim, _, err := ic.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	victimID, _ := peer.IDFromPrivateKey(victim)

	value, err := ic.MarshalPublicKey(attacker.GetPublic())
	if err != nil {
		t.Fatal(err)
	}
	if err := (np4Validator{}).Validate(keyFor(t, victimID), value); err == nil {
		t.Fatal("poisoned record accepted: attacker key stored under victim's ID")
	}
}

func TestNp4ValidatorRejectsGarbage(t *testing.T) {
	priv, _, _ := ic.GenerateEd25519Key(nil)
	pid, _ := peer.IDFromPrivateKey(priv)

	if err := (np4Validator{}).Validate("not-valid-base32!!!", []byte("x")); err == nil {
		t.Error("invalid base32 key accepted")
	}
	if err := (np4Validator{}).Validate(keyFor(t, pid), []byte("not a key")); err == nil {
		t.Error("non-key value accepted")
	}
	if _, err := (np4Validator{}).Select(keyFor(t, pid), nil); err == nil {
		t.Error("Select with no values must error")
	}
}

// TestNp4ValidatorAdmission pins the allowlist choke point: a validly bound
// record is still refused when its publisher is not admitted. Admission nil
// (the development default) must allow everything.
func TestNp4ValidatorAdmission(t *testing.T) {
	insider, _, err := ic.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	outsider, _, err := ic.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	insiderID, _ := peer.IDFromPrivateKey(insider)
	outsiderID, _ := peer.IDFromPrivateKey(outsider)
	val := func(k ic.PubKey) []byte {
		value, err := ic.MarshalPublicKey(k)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}

	allowList := map[peer.ID]struct{}{insiderID: {}}
	v := np4Validator{Admission: func(id peer.ID) bool {
		_, ok := allowList[id]
		return ok
	}}

	if err := v.Validate(keyFor(t, insiderID), val(insider.GetPublic())); err != nil {
		t.Errorf("admitted peer rejected: %v", err)
	}
	if err := v.Validate(keyFor(t, outsiderID), val(outsider.GetPublic())); err == nil {
		t.Error("non-admitted peer accepted: allowlist has no teeth")
	}

	if err := (np4Validator{}).Validate(keyFor(t, outsiderID), val(outsider.GetPublic())); err != nil {
		t.Errorf("nil admission must allow all valid bindings, got %v", err)
	}
}
