package np4

import (
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// relayLimiter is a per-peer token bucket over relay ingress. Without it a
// single malicious client can flood the relay mix (capacity 256, drop-oldest)
// and evict everyone else's cells — a one-node denial of service in the
// single-relay deployment.
//
// Legitimate chat is far below the default budget: one message is one cell
// and humans type well under 1 cell/s; the burst absorbs mix-flush
// jitter where a batch arrives back-to-back.
type relayLimiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64 // bucket capacity
	buckets map[peer.ID]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRelayLimiter(rate float64, burst int) *relayLimiter {
	if burst < 1 {
		burst = 1
	}
	return &relayLimiter{
		rate:    rate,
		burst:   float64(burst),
		buckets: make(map[peer.ID]*bucket),
	}
}

// allow consumes one token for pid, refilling by elapsed time. Buckets are
// created lazily; once the map grows past the cap the sweep evicts idle
// buckets and then the least-recently-used half — a churn of short-lived
// peers cannot leak memory, and eviction only resets a bucket (a peer
// regains burst, never gains unmetered passage).
func (l *relayLimiter) allow(pid peer.ID) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) > maxLimiterEntries {
		l.sweepLocked(now)
	}
	b, ok := l.buckets[pid]
	if !ok {
		l.buckets[pid] = &bucket{tokens: l.burst - 1, last: now}
		return true
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

const (
	maxLimiterEntries = 4096
	limiterIdleCutoff = 10 * time.Minute
)

func (l *relayLimiter) sweepLocked(now time.Time) {
	for pid, b := range l.buckets {
		if now.Sub(b.last) > limiterIdleCutoff {
			delete(l.buckets, pid)
		}
	}
	if len(l.buckets) <= maxLimiterEntries {
		return
	}
	type kv struct {
		pid  peer.ID
		last time.Time
	}
	all := make([]kv, 0, len(l.buckets))
	for pid, b := range l.buckets {
		all = append(all, kv{pid, b.last})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].last.Before(all[j].last) })
	for _, e := range all[:len(all)/2] {
		delete(l.buckets, e.pid)
	}
}
