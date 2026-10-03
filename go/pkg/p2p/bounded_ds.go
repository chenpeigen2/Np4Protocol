package p2p

import (
	"context"
	"errors"
	"sync"

	ds "github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"
)

// DHT record store bound. kad-dht's default in-memory datastore is an
// UNBOUNDED map, and the /kad protocol stream bypasses the onion ingress
// limiter entirely: in open-admission mode an attacker can generate unlimited
// valid identities and PutValue until every ModeServer node (the bootstrap
// AND every client) runs out of memory. Each record is validator-bounded to
// 140 bytes, so the count is what must be capped.
//
// 100k records ≈ tens of MB worst case. When the store is full, NEW records
// are rejected while updates to existing keys still pass — a full store
// degrades new-node onboarding, never existing records, and the operator's
// answer is to enable the allowlist (which stops the flood at the connection
// gate).
const maxDHTRecords = 100_000

// ErrRecordStoreFull is returned when the bounded datastore is at capacity
// and a NEW key arrives.
var ErrRecordStoreFull = errors.New("dht record store full")

// boundedDatastore wraps a batching datastore with a hard cap on the number
// of stored keys. Everything else (Get, Query, ...) delegates untouched.
type boundedDatastore struct {
	// mu guards count AND delegates inner access: kad-dht serves Get/Query
	// from handler goroutines concurrently with Put/Batch writes — the
	// counter and the underlying map must be traversed under the same lock.
	mu    sync.RWMutex
	inner ds.Batching
	count int
}

func newBoundedDatastore(inner ds.Batching) *boundedDatastore {
	return &boundedDatastore{inner: inner}
}

// putLocked stores one key. Caller holds mu.
func (b *boundedDatastore) putLocked(ctx context.Context, k ds.Key, value []byte) error {
	exists, err := b.inner.Has(ctx, k)
	if err != nil && !errors.Is(err, ds.ErrNotFound) {
		return err
	}
	if !exists && b.count >= maxDHTRecords {
		return ErrRecordStoreFull
	}
	if err := b.inner.Put(ctx, k, value); err != nil {
		return err
	}
	if !exists {
		b.count++
	}
	return nil
}

func (b *boundedDatastore) Put(ctx context.Context, k ds.Key, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.putLocked(ctx, k, value)
}

func (b *boundedDatastore) Get(ctx context.Context, k ds.Key) ([]byte, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.inner.Get(ctx, k)
}

func (b *boundedDatastore) Has(ctx context.Context, k ds.Key) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.inner.Has(ctx, k)
}

func (b *boundedDatastore) GetSize(ctx context.Context, k ds.Key) (int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.inner.GetSize(ctx, k)
}

func (b *boundedDatastore) Delete(ctx context.Context, k ds.Key) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	exists, err := b.inner.Has(ctx, k)
	if err != nil && !errors.Is(err, ds.ErrNotFound) {
		return err
	}
	if err := b.inner.Delete(ctx, k); err != nil {
		return err
	}
	if exists {
		b.count--
	}
	return nil
}

func (b *boundedDatastore) Query(ctx context.Context, q dsq.Query) (dsq.Results, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.inner.Query(ctx, q)
}

func (b *boundedDatastore) Sync(ctx context.Context, prefix ds.Key) error {
	return b.inner.Sync(ctx, prefix)
}

func (b *boundedDatastore) Close() error { return b.inner.Close() }

// batch accumulates writes and applies them under the count bound at Commit.
type boundedBatch struct {
	b    *boundedDatastore
	ops  []dsOp
	opsN int
}

type dsOp struct {
	del   bool
	key   ds.Key
	value []byte
}

func (b *boundedDatastore) Batch(ctx context.Context) (ds.Batch, error) {
	return &boundedBatch{b: b}, nil
}

func (bb *boundedBatch) Put(ctx context.Context, k ds.Key, value []byte) error {
	bb.ops = append(bb.ops, dsOp{key: k, value: value})
	return nil
}

func (bb *boundedBatch) Delete(ctx context.Context, k ds.Key) error {
	bb.ops = append(bb.ops, dsOp{del: true, key: k})
	return nil
}

func (bb *boundedBatch) Commit(ctx context.Context) error {
	bb.b.mu.Lock()
	defer bb.b.mu.Unlock()
	for _, op := range bb.ops {
		var err error
		if op.del {
			err = bb.b.inner.Delete(ctx, op.key)
		} else {
			err = bb.b.putLocked(ctx, op.key, op.value)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
