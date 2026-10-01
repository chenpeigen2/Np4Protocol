package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/curve25519"
)

// Forward secrecy via time-bucketed X25519 subkeys.
//
// The onion layer and the sender-auth tag both do ECDH against a published
// X25519 key. Instead of one static key derived from the ed25519 seed (which
// would make a later key-file compromise decrypt the entire recorded
// history), the node generates a FRESH RANDOM X25519 keypair per time bucket
// (default 24h) and keeps the private keys of the last RetentionWindow
// buckets (default 7d). Published records carry the current subkey signed by
// the master ed25519 key, so the peer-ID binding is unchanged; receivers
// decrypt by trying the retained window.
//
// Properties:
//   - an attacker who records traffic and later steals the key files can
//     decrypt at most the retention window (7d) — older traffic is gone;
//   - a receiver offline for ≤7d still decrypts mail sent to its older
//     subkeys (protocol messages are best-effort anyway);
//   - the master ed25519 key (and thus the peer ID, address book, allowlist)
//     never rotates.
//
// Subkeys are NOT derived from the master seed on purpose: derivation would
// make every past subkey recomputable from the seed file and void the first
// property. The price is sidecar state next to the identity file.
const (
	// RotationPeriod is how often a fresh X25519 subkey is generated.
	RotationPeriod = 24 * time.Hour
	// RetentionWindow is how long retired subkeys are kept before their
	// private keys are destroyed — the maximum age of traffic still
	// decryptable after a key compromise.
	RetentionWindow = 7 * RotationPeriod

	rotationDomain = "np4-rotation-v1"
)

type rotatedKey struct {
	bucket int64
	priv   []byte // 32-byte X25519 private
	pub    []byte // 32-byte X25519 public
}

// bucketOf maps a wall-clock time to a rotation bucket index.
func (i *Identity) bucketOf(t time.Time) int64 {
	period := i.bucketPeriod
	if period <= 0 {
		period = RotationPeriod
	}
	return t.Unix() / int64(period/time.Second)
}

// ensureRotationLocked aligns the subkey window with the current bucket:
// generates a fresh keypair when the bucket rolled over, drops retired keys
// past the retention window, and persists the sidecar. Returns true when a
// rotation happened (callers republish the record).
func (i *Identity) ensureRotationLocked() (bool, error) {
	now := i.now()
	cur := i.bucketOf(now)
	if len(i.rotKeys) > 0 && i.rotKeys[0].bucket == cur {
		// Same bucket: still drop anything that aged out of the window.
		kept := i.pruneLocked(cur)
		if kept {
			return false, i.persistLocked()
		}
		return false, nil
	}
	k, err := newRotatedKey(cur)
	if err != nil {
		return false, err
	}
	i.rotKeys = append([]rotatedKey{*k}, i.rotKeys...)
	i.pruneLocked(cur)
	i.ecdhPriv = i.rotKeys[0].priv
	i.ecdhPub = i.rotKeys[0].pub
	if err := i.persistLocked(); err != nil {
		return false, err
	}
	return true, nil
}

// pruneLocked drops subkeys older than the retention window in place.
func (i *Identity) pruneLocked(cur int64) (changed bool) {
	period := i.bucketPeriod
	if period <= 0 {
		period = RotationPeriod
	}
	keepBuckets := int64(RetentionWindow / period)
	if keepBuckets < 1 {
		keepBuckets = 1
	}
	kept := i.rotKeys[:0]
	for _, k := range i.rotKeys {
		if cur-k.bucket <= keepBuckets {
			kept = append(kept, k)
		} else {
			changed = true // private key destroyed: traffic to it is history
		}
	}
	i.rotKeys = kept
	return changed
}

func newRotatedKey(bucket int64) (*rotatedKey, error) {
	priv := make([]byte, 32)
	if _, err := rand.Read(priv); err != nil {
		return nil, fmt.Errorf("generate subkey: %w", err)
	}
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("derive subkey pub: %w", err)
	}
	return &rotatedKey{bucket: bucket, priv: priv, pub: pub}, nil
}

// sidecar is the persisted subkey window, stored next to the identity file
// as <path>.keys. Private keys at rest: 0600, same threat surface as the
// seed itself. Bucket numbers are persisted as written — they are real-wall
// -clock facts, and recomputing them under a different period/clock would
// corrupt the window ordering.
type sidecarKey struct {
	Bucket int64  `json:"bucket"`
	Priv   string `json:"priv"` // base64
}

type sidecar struct {
	Keys []sidecarKey `json:"keys"` // current first
}

func sidecarPath(path string) string { return path + ".keys" }

func (i *Identity) persistLocked() error {
	if i.sidecarPath == "" {
		return nil // ephemeral identity: nothing on disk
	}
	sc := sidecar{}
	for _, k := range i.rotKeys {
		sc.Keys = append(sc.Keys, sidecarKey{Bucket: k.bucket, Priv: base64.StdEncoding.EncodeToString(k.priv)})
	}
	data, err := json.Marshal(&sc)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(i.sidecarPath), ".np4-keys-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	// Atomic replacement: readers never observe a torn file.
	return os.Rename(tmpName, i.sidecarPath)
}

func (i *Identity) loadSidecar() error {
	if i.sidecarPath == "" {
		return nil
	}
	data, err := os.ReadFile(i.sidecarPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil // first run: ensureRotation creates the window
	} else if err != nil {
		return fmt.Errorf("read key sidecar: %w", err)
	}
	var sc sidecar
	if err := json.Unmarshal(data, &sc); err != nil {
		// A corrupt sidecar must not brick the identity: start a fresh
		// window (peers address the new subkey on their next key fetch).
		fmt.Fprintf(os.Stderr, "[identity] corrupt key sidecar %s: %v; starting a fresh subkey window\n", i.sidecarPath, err)
		return nil
	}
	var keys []rotatedKey
	for _, sk := range sc.Keys {
		priv, err := base64.StdEncoding.DecodeString(sk.Priv)
		if err != nil || len(priv) != 32 || sk.Bucket < 0 {
			continue
		}
		pub, err := curve25519.X25519(priv, curve25519.Basepoint)
		if err != nil {
			continue
		}
		keys = append(keys, rotatedKey{bucket: sk.Bucket, priv: priv, pub: pub})
	}
	if len(keys) == 0 {
		return nil
	}
	i.rotKeys = keys
	i.ecdhPriv = keys[0].priv
	i.ecdhPub = keys[0].pub
	return nil
}

// EnsureCurrentBucket aligns the subkey window with the current time bucket.
// It returns true when the bucket rolled over and a fresh subkey was
// generated (callers must republish the DHT record). Safe for concurrent use.
func (i *Identity) EnsureCurrentBucket() (bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.ensureRotationLocked()
}

// CurrentBucket returns the active rotation bucket.
func (i *Identity) CurrentBucket() int64 {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if len(i.rotKeys) == 0 {
		return i.bucketOf(i.now())
	}
	return i.rotKeys[0].bucket
}

// BucketPeriod exposes the active rotation period (for callers that align
// their own loops with it).
func (i *Identity) BucketPeriod() time.Duration {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.bucketPeriod <= 0 {
		return RotationPeriod
	}
	return i.bucketPeriod
}

// RotationPub returns the current subkey and its bucket as a consistent
// pair (a rotation between the two reads would otherwise publish a record
// signed over a mismatched bucket).
func (i *Identity) RotationPub() ([]byte, int64) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	pub := append([]byte(nil), i.ecdhPub...)
	return pub, i.rotKeys[0].bucket
}

// ECDHPrivs returns copies of the retained X25519 private keys, current
// first — the window receivers try when decrypting and verifying.
func (i *Identity) ECDHPrivs() [][]byte {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([][]byte, 0, len(i.rotKeys))
	for _, k := range i.rotKeys {
		out = append(out, append([]byte(nil), k.priv...))
	}
	return out
}

// Sign signs msg with the master ed25519 key. Used to bind the current
// subkey into the published record without rotating the peer identity.
func (i *Identity) Sign(msg []byte) []byte {
	return ed25519.Sign(i.stdPriv, msg)
}

// SetTestRotation replaces the clock and bucket period. TEST-ONLY: it exists
// so rotation e2e tests can use second-scale buckets instead of waiting a
// day. Never call from production code paths.
func (i *Identity) SetTestRotation(nowFn func() time.Time, period time.Duration) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.nowFn = nowFn
	i.bucketPeriod = period
}

// initRotation loads (or lazily creates) the subkey window after the master
// identity is established.
func (i *Identity) initRotation() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.loadSidecar(); err != nil {
		return err
	}
	if len(i.rotKeys) == 0 {
		k, err := newRotatedKey(i.bucketOf(i.now()))
		if err != nil {
			return err
		}
		i.rotKeys = []rotatedKey{*k}
		i.ecdhPriv = k.priv
		i.ecdhPub = k.pub
	}
	return i.persistLocked()
}
