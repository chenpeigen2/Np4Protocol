package p2p

import (
	"encoding/base32"
	"testing"

	"Np4Protocol/go/pkg/pathsel"

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

// rotationRecordFor frames a v2 key record: master pub (binding anchor) +
// current subkey + master signature. The key's own public bytes stand in for
// the subkey — the validator treats the subkey as opaque bytes.
func rotationRecordFor(t *testing.T, priv ic.PrivKey) []byte {
	t.Helper()
	raw, err := priv.GetPublic().Raw()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := pathsel.EncodeRotationRecord(priv.GetPublic(), 0, raw, func(msg []byte) []byte {
		sig, err := priv.Sign(msg)
		if err != nil {
			t.Fatal(err)
		}
		return sig
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec
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
	if err := (np4Validator{}).Validate(keyFor(t, pid), rotationRecordFor(t, priv)); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}
}

// TestNp4ValidatorRejectsPoisoning: the load-bearing anti-poisoning test — a
// record whose master key hashes to a DIFFERENT peer ID must be rejected,
// regardless of a valid signature. The DHT never verifies signatures; the
// binding is the defense. (An attacker literally cannot produce a record
// bound to the victim's ID: that would require a master key hashing to the
// victim's peer ID, and the signature proves possession of it anyway.)
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

	if err := (np4Validator{}).Validate(keyFor(t, victimID), rotationRecordFor(t, attacker)); err == nil {
		t.Fatal("poisoned record accepted: attacker key stored under victim's ID")
	}
}

// TestNp4ValidatorRejectsForgery: a record bound to the right peer ID but
// signed by someone without the master private key must be rejected — the
// signature check catches transplanted payloads.
func TestNp4ValidatorRejectsForgery(t *testing.T) {
	victim, _, err := ic.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	attacker, _, err := ic.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	victimID, _ := peer.IDFromPrivateKey(victim)
	attackerRaw, err := attacker.GetPublic().Raw()
	if err != nil {
		t.Fatal(err)
	}
	// Correct master (binding passes), attacker's signature over the subkey.
	rec, err := pathsel.EncodeRotationRecord(victim.GetPublic(), 0, attackerRaw, func(msg []byte) []byte {
		sig, _ := attacker.Sign(msg)
		return sig
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := (np4Validator{}).Validate(keyFor(t, victimID), rec); err == nil {
		t.Fatal("forged subkey signature accepted")
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
	if err := (np4Validator{}).Validate(keyFor(t, pid), rotationRecordFor(t, priv)[:10]); err == nil {
		t.Error("truncated record accepted")
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

	allowList := map[peer.ID]struct{}{insiderID: {}}
	v := np4Validator{Admission: func(id peer.ID) bool {
		_, ok := allowList[id]
		return ok
	}}

	if err := v.Validate(keyFor(t, insiderID), rotationRecordFor(t, insider)); err != nil {
		t.Errorf("admitted peer rejected: %v", err)
	}
	if err := v.Validate(keyFor(t, outsiderID), rotationRecordFor(t, outsider)); err == nil {
		t.Error("non-admitted peer accepted: allowlist has no teeth")
	}

	if err := (np4Validator{}).Validate(keyFor(t, outsiderID), rotationRecordFor(t, outsider)); err != nil {
		t.Errorf("nil admission must allow all valid bindings, got %v", err)
	}
}
