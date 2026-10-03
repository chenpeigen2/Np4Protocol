package p2p

import (
	"context"
	"strconv"
	"testing"

	ds "github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"
)

// TestBoundedDatastoreRejectsBeyondCap pins the memory-exhaustion contract:
// NEW keys beyond the cap are rejected, updates to EXISTING keys still pass
// (a full store degrades onboarding, never existing records), deletes free
// slots, and Query keeps working.
func TestBoundedDatastoreRejectsBeyondCap(t *testing.T) {
	b := newBoundedDatastore(ds.NewMapDatastore())
	// Fill the real production cap: the bound must never be a mutable
	// package variable (round-4 lesson — mutable security knobs drift).

	ctx := context.Background()
	sentinel := ds.NewKey("sentinel")
	if err := b.Put(ctx, sentinel, []byte("original")); err != nil {
		t.Fatalf("sentinel put: %v", err)
	}
	for i := 0; i < maxDHTRecords-1; i++ {
		k := ds.NewKey("k" + strconv.Itoa(i))
		if err := b.Put(ctx, k, []byte{byte(i)}); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	if b.count != maxDHTRecords {
		t.Fatalf("count %d, want %d", b.count, maxDHTRecords)
	}
	if err := b.Put(ctx, ds.NewKey("new"), []byte("x")); err == nil {
		t.Fatal("record beyond cap accepted")
	}
	// Updating an existing key must still work at cap.
	if err := b.Put(ctx, sentinel, []byte("updated")); err != nil {
		t.Fatalf("existing-key update rejected at cap: %v", err)
	}
	// Delete frees a slot.
	if err := b.Delete(ctx, sentinel); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := b.Put(ctx, ds.NewKey("new"), []byte("x")); err != nil {
		t.Fatalf("put after delete: %v", err)
	}

	res, err := b.Query(ctx, dsq.Query{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if n := countResults(res); n != maxDHTRecords {
		t.Fatalf("query returned %d entries, want %d", n, maxDHTRecords)
	}
}

// TestBoundedDatastoreBatch: batched writes respect the same cap and count
// accounting (put+delete of the same key inside one batch nets zero).
func TestBoundedDatastoreBatch(t *testing.T) {
	b := newBoundedDatastore(ds.NewMapDatastore())
	// Pre-fill to cap-1 so the batch has room for exactly one new key.
	ctx := context.Background()
	for i := 0; i < maxDHTRecords-1; i++ {
		if err := b.Put(ctx, ds.NewKey("pre"+strconv.Itoa(i)), []byte{byte(i)}); err != nil {
			t.Fatalf("prefill %d: %v", i, err)
		}
	}
	batch, err := b.Batch(ctx)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	_ = batch.Put(ctx, ds.NewKey("a"), []byte("1"))
	_ = batch.Put(ctx, ds.NewKey("b"), []byte("2"))
	_ = batch.Put(ctx, ds.NewKey("c"), []byte("3")) // beyond cap, rejected at commit
	if err := batch.Commit(ctx); err == nil {
		t.Fatal("batch committing beyond cap succeeded")
	}
	// The rejected op aborts the commit mid-way; the store must still be
	// consistent (either prefix applied, count matching actual keys).
	has, err := b.Has(ctx, ds.NewKey("a"))
	if err != nil || !has {
		t.Fatalf("prefix of batch lost: has a = %v, %v", has, err)
	}
	if b.count != maxDHTRecords {
		t.Fatalf("count %d inconsistent with %d applied ops", b.count, maxDHTRecords)
	}
}

func countResults(r dsq.Results) int {
	n := 0
	for {
		// NextSync's bool is "got an entry"; false means exhausted.
		if _, ok := r.NextSync(); !ok {
			break
		}
		n++
	}
	return n
}
