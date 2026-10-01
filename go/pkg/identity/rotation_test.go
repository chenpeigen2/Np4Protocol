package identity

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"
)

// rotationFixture builds a persistent identity with a second-scale bucket
// driven by a controllable clock.
func rotationFixture(t *testing.T, dir string, period time.Duration) (*Identity, *time.Time) {
	t.Helper()
	path := filepath.Join(dir, "id")
	if _, err := LoadOrCreate(path); err != nil {
		t.Fatalf("identity: %v", err)
	}
	// Reload through the persisted file so the sidecar path is wired.
	id, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("identity reload: %v", err)
	}
	now := time.Now()
	clock := &now
	id.SetTestRotation(func() time.Time { return *clock }, period)
	if _, err := id.EnsureCurrentBucket(); err != nil {
		t.Fatalf("initial rotation: %v", err)
	}
	return id, clock
}

// TestRotationGeneratesFreshKeypairs: advancing the clock past a bucket
// boundary yields a brand-new subkey; the old one stays in the retention
// window for decryption of in-flight traffic.
func TestRotationGeneratesFreshKeypairs(t *testing.T) {
	id, clock := rotationFixture(t, t.TempDir(), time.Second)
	oldPub := append([]byte(nil), id.ECDHPub()...)
	oldPrivs := id.ECDHPrivs()

	*clock = clock.Add(1500 * time.Millisecond) // next bucket
	rotated, err := id.EnsureCurrentBucket()
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if !rotated {
		t.Fatal("bucket boundary crossed without rotation")
	}
	newPub := id.ECDHPub()
	if string(newPub) == string(oldPub) {
		t.Fatal("rotation reused the same subkey")
	}
	privs := id.ECDHPrivs()
	if len(privs) != 2 {
		t.Fatalf("retained %d privs, want 2 (current + previous)", len(privs))
	}
	// Current first, previous retained.
	if string(privs[0]) == string(oldPrivs[0]) {
		t.Fatal("current priv slot did not advance")
	}
	if string(privs[1]) != string(oldPrivs[0]) {
		t.Fatal("previous subkey not retained")
	}
	// ECDH with the retained old priv still reproduces the old pub pairing.
	shared, err := curve25519.X25519(privs[1], oldPub)
	if err != nil {
		t.Fatalf("retained priv unusable: %v", err)
	}
	if len(shared) != 32 {
		t.Fatalf("shared size %d", len(shared))
	}
}

// TestRotationDropsExpiredKeys: keys past the retention window are destroyed
// — that destruction IS the forward-secrecy property.
func TestRotationDropsExpiredKeys(t *testing.T) {
	id, _ := rotationFixture(t, t.TempDir(), time.Second)
	id.mu.Lock()
	// Window of exactly 3 buckets: shrink the period so
	// keepBuckets = RetentionWindow/period = 3, then place "now" three
	// buckets past the newest key.
	id.bucketPeriod = RetentionWindow / 3
	cur := int64(100)
	periodSecs := int64(id.bucketPeriod / time.Second)
	fakeNow := time.Unix(cur*periodSecs, 0)
	id.nowFn = func() time.Time { return fakeNow }
	id.rotKeys = nil // drop the fixture's key: this test fills its own window
	for age := int64(0); age < 10; age++ {
		k, err := newRotatedKey(cur - age)
		if err != nil {
			t.Fatalf("subkey %d: %v", age, err)
		}
		id.rotKeys = append(id.rotKeys, *k)
	}
	id.ecdhPriv = id.rotKeys[0].priv
	id.ecdhPub = id.rotKeys[0].pub
	id.pruneLocked(cur)
	id.mu.Unlock()

	if got := len(id.ECDHPrivs()); got != 4 {
		t.Fatalf("kept %d keys, want 4 (ages 0..3 within the 3-bucket window)", got)
	}
}

// TestRotationPersistsAcrossRestart: the sidecar survives restarts, so a
// restarted node still decrypts traffic addressed to its pre-restart
// subkeys — offline receivers work within the window.
func TestRotationPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id")
	id, clock := rotationFixture(t, dir, time.Second)
	oldPub := append([]byte(nil), id.ECDHPub()...)

	*clock = clock.Add(1500 * time.Millisecond)
	if _, err := id.EnsureCurrentBucket(); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// "Restart": load the identity file fresh, restore the test clock. The
	// first EnsureCurrentBucket after the clock switch is the switch's own
	// rotation (buckets computed under the new period differ from the
	// persisted ones); what matters is the WINDOW surviving.
	reloaded, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	reloaded.SetTestRotation(func() time.Time { return *clock }, time.Second)
	if _, err := reloaded.EnsureCurrentBucket(); err != nil {
		t.Fatalf("reload rotation: %v", err)
	}

	privs := reloaded.ECDHPrivs()
	if len(privs) < 2 {
		t.Fatalf("reloaded window has %d keys, want ≥2", len(privs))
	}
	found := false
	for _, priv := range privs {
		pub, err := curve25519.X25519(priv, curve25519.Basepoint)
		if err != nil {
			continue
		}
		if string(pub) == string(oldPub) {
			found = true
		}
	}
	if !found {
		t.Fatal("pre-restart subkey lost across restart: offline receivers would lose mail")
	}
	if _, err := os.Stat(sidecarPath(path)); err != nil {
		t.Fatalf("sidecar missing: %v", err)
	}
}

// TestRotationBucketPersistsOverRestart: once the clock is aligned, a
// same-bucket reload must NOT generate a new subkey (that would silently
// replace the published key).
func TestRotationBucketPersistsOverRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id")
	_, _ = rotationFixture(t, dir, time.Hour)

	reloaded, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	reloaded.SetTestRotation(func() time.Time { return time.Now() }, time.Hour)
	// First call absorbs the clock alignment; the second must be a no-op.
	_, err = reloaded.EnsureCurrentBucket()
	if err != nil {
		t.Fatalf("reload rotation: %v", err)
	}
	pub1 := append([]byte(nil), reloaded.ECDHPub()...)
	rotated, err := reloaded.EnsureCurrentBucket()
	if err != nil {
		t.Fatalf("second rotation check: %v", err)
	}
	if rotated {
		t.Fatal("same-bucket reload reported a rotation")
	}
	if string(reloaded.ECDHPub()) != string(pub1) {
		t.Fatal("same-bucket reload replaced the subkey")
	}
}

// TestSignUsesMasterKey: the rotation signature comes from the master
// ed25519 key, verifiable against the published master public key.
func TestSignUsesMasterKey(t *testing.T) {
	id, err := LoadOrCreate("")
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	sig := id.Sign([]byte("payload"))
	if !ed25519.Verify(ed25519.PublicKey(id.SigningPub()), []byte("payload"), sig) {
		t.Fatal("signature does not verify under the master public key")
	}
	if ed25519.Verify(ed25519.PublicKey(id.SigningPub()), []byte("payload2"), sig) {
		t.Fatal("signature verifies under different payload")
	}
}
