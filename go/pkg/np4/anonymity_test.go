package np4

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"Np4Protocol/go/pkg/cell"
	"Np4Protocol/go/pkg/identity"
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/onion"
	"Np4Protocol/go/pkg/p2p"

	"github.com/libp2p/go-libp2p/core/peer"
)

// This file is the anonymity-maintenance test suite: it quantifies how much
// an honest-but-curious MIDDLE relay learns from timing correlation.
//
// Model: all traffic traverses R1 → ADV → R2 → dest. ADV never sees the true
// sender or receiver — only its immediate neighbors — so the only way it can
// link an ingress packet to its egress packet is timing. The attack is the
// strongest simple heuristic: pair each egress with the most recent unmatched
// ingress within a generous delay budget.
//
// The test runs the same traffic twice:
//   - mixed:   ADV mixes normally (batch 10 / 200ms shuffle) — precision must
//     sit at the Monte-Carlo chance baseline.
//   - control: ADV forwards immediately in arrival order (mixing disabled via
//     WithImmediateRelayForward) — precision must be ~1.0, proving the harness
//     detects correlation when mixing is absent. Without this control the
//     mixed-run assertion would be vacuous.
//
// Deterministic: fixed PRNG seed, fixed schedule shape.

const (
	anClients   = 6
	anSends     = 60
	anSeed      = 42
	anMaxDelay  = 400 * time.Millisecond // attacker's per-pair delay budget
	anWindowGap = 150 * time.Millisecond // egress gap that closes a flush window
	anMCRuns    = 500
)

type linkRecorder struct {
	mu     sync.Mutex
	events []LinkEvent
}

func (r *linkRecorder) record(ev LinkEvent) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *linkRecorder) snapshot() []LinkEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]LinkEvent(nil), r.events...)
}

// anSend is one flow's ground truth at the adversary: which layer arrives
// (layer 2) and which layer leaves (layer 3).
type anSend struct {
	inEph  [32]byte
	outEph [32]byte
}

type anRunResult struct {
	precision float64
	delivered int
	sends     []anSend
	egress    []LinkEvent // adversary egress events, time-sorted
}

// TestMiddleRelayUnlinkability is the load-bearing anonymity assertion.
func TestMiddleRelayUnlinkability(t *testing.T) {
	mixed := runAnonymityExperiment(t, false)
	control := runAnonymityExperiment(t, true)

	t.Logf("delivered %d/%d sends in each run", mixed.delivered, anSends)
	if mixed.delivered != anSends || control.delivered != anSends {
		t.Fatalf("incomplete delivery ruins the experiment (mixed %d, control %d)",
			mixed.delivered, control.delivered)
	}

	chanceMean, chanceStd := monteCarloBaseline(mixed.sends, mixed.egress)
	tolerance := chanceStd*3 + 0.08

	t.Logf("attacker precision: control=%.2f mixed=%.2f chance=%.2f±%.2f (tolerance %.2f)",
		control.precision, mixed.precision, chanceMean, chanceStd, tolerance)

	if control.precision < 0.85 {
		t.Errorf("control run precision %.2f — attack should be near-perfect without "+
			"mixing; the harness cannot validate the mixed run", control.precision)
	}
	if mixed.precision > chanceMean+tolerance {
		t.Errorf("mixed run precision %.2f exceeds chance baseline %.2f±%.2f — "+
			"the relay's mixing is not defeating timing correlation",
			mixed.precision, chanceMean, chanceStd)
	}
	if mixed.precision > control.precision-0.15 {
		t.Errorf("mixing gains nothing: mixed %.2f vs control %.2f", mixed.precision, control.precision)
	}
}

func runAnonymityExperiment(t *testing.T, control bool) anRunResult {
	t.Helper()
	dir := t.TempDir()
	label := "mixed"
	if control {
		label = "control"
	}

	boot, err := NewNode(0, WithIdentity(filepath.Join(dir, "boot")), WithDHTServer())
	if err != nil {
		t.Fatalf("[%s] bootstrap: %v", label, err)
	}
	defer boot.Close()
	bootAddr := peer.AddrInfo{ID: boot.ID(), Addrs: boot.Host().Addrs()}

	// R1 and R2 forward immediately: the adversary's mixing is the ONLY
	// shuffler in the path, so the measurement isolates its contribution.
	// ADV mixes normally, except in the control run.
	mkRelay := func(name string, immediate bool) *Node {
		opts := []Option{
			WithIdentity(filepath.Join(dir, name)),
			WithBootstrap([]peer.AddrInfo{bootAddr}),
		}
		if immediate {
			opts = append(opts, WithImmediateRelayForward())
		}
		n, err := NewNode(0, opts...)
		if err != nil {
			t.Fatalf("[%s] relay %s: %v", label, name, err)
		}
		t.Cleanup(func() { n.Close() })
		if err := n.ServeRelay(); err != nil {
			t.Fatalf("[%s] relay %s ServeRelay: %v", label, name, err)
		}
		return n
	}
	r1 := mkRelay("r1", true)
	adv := mkRelay("adv", control)
	mkRelay("r2", true) // cleanup registered inside; identity reloaded below

	clients := make([]*Node, anClients)
	for i := range clients {
		n, err := NewNode(0,
			WithIdentity(filepath.Join(dir, fmt.Sprintf("c%d", i))),
			WithBootstrap([]peer.AddrInfo{bootAddr}),
		)
		if err != nil {
			t.Fatalf("[%s] client %d: %v", label, i, err)
		}
		t.Cleanup(func() { n.Close() })
		if err := n.PublishKeys(); err != nil {
			t.Fatalf("[%s] client %d PublishKeys: %v", label, i, err)
		}
		clients[i] = n
	}

	reload := func(name string) *identity.Identity {
		id, err := identity.LoadOrCreate(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("[%s] reload %s: %v", label, name, err)
		}
		return id
	}
	r1ID, advID, r2ID := reload("r1"), reload("adv"), reload("r2")

	// Let DHT records propagate before attaching the observer / sending.
	time.Sleep(6 * time.Second)

	rec := &linkRecorder{}
	adv.OnLinkEvent(rec.record)

	recvCounts := make([]int, anClients)
	var recvMu sync.Mutex
	for i, c := range clients {
		i, c := i, c
		c.OnMessage(func(msg *message.Message) {
			_ = msg
			recvMu.Lock()
			recvCounts[i]++
			recvMu.Unlock()
		})
	}

	rng := rand.New(rand.NewSource(anSeed))
	sends := make([]anSend, 0, anSends)

	for i := 0; i < anSends; i++ {
		src := rng.Intn(anClients)
		dst := rng.Intn(anClients)
		if dst == src {
			dst = (dst + 1) % anClients
		}
		dstID := reload(fmt.Sprintf("c%d", dst))

		c, err := cell.Seal([]byte(fmt.Sprintf("%s-flow-%d", label, i)))
		if err != nil {
			t.Fatalf("[%s] seal: %v", label, err)
		}
		hops := []onion.Hop{
			{PeerID: r1ID.PeerID(), ECDHPub: r1ID.ECDHPub()},
			{PeerID: advID.PeerID(), ECDHPub: advID.ECDHPub()},
			{PeerID: r2ID.PeerID(), ECDHPub: r2ID.ECDHPub()},
			{PeerID: dstID.PeerID(), ECDHPub: dstID.ECDHPub()},
		}
		on, err := onion.Build(hops, c)
		if err != nil {
			t.Fatalf("[%s] build: %v", label, err)
		}
		// Ground truth: what ADV receives (layer 2) and forwards (layer 3).
		d1, err := onion.Decode(on.Bytes(), r1ID)
		if err != nil {
			t.Fatalf("[%s] peel r1: %v", label, err)
		}
		d2, err := onion.Decode(d1.Inner, advID)
		if err != nil {
			t.Fatalf("[%s] peel adv: %v", label, err)
		}
		var s anSend
		copy(s.inEph[:], d1.Inner[:32])
		copy(s.outEph[:], d2.Inner[:32])
		sends = append(sends, s)

		wire, err := onion.Wrap(3, on.Bytes())
		if err != nil {
			t.Fatalf("[%s] wrap: %v", label, err)
		}
		sendRawTo(t, clients[src], r1, wire)

		// Jittered inter-arrival so flush windows contain 1..~5 packets.
		time.Sleep(time.Duration(30+rng.Intn(100)) * time.Millisecond)
	}

	// Wait for full delivery.
	waitFor(t, 60*time.Second, func() bool {
		recvMu.Lock()
		defer recvMu.Unlock()
		total := 0
		for _, n := range recvCounts {
			total += n
		}
		return total >= anSends
	})
	// Allow ADV's last relay-mix flush to drain so egress events are recorded.
	time.Sleep(2 * time.Second)

	events := rec.snapshot()
	ingress := map[[32]byte]time.Time{}
	var egress []LinkEvent
	for _, ev := range events {
		if ev.In {
			ingress[ev.Eph] = ev.T
		} else {
			egress = append(egress, ev)
		}
	}
	sort.Slice(egress, func(i, j int) bool { return egress[i].T.Before(egress[j].T) })

	return anRunResult{
		precision: attackPrecision(sends, ingress, egress),
		delivered: countDelivered(recvCounts),
		sends:     sends,
		egress:    egress,
	}
}

// attackPrecision pairs each adversary egress with the most recent unmatched
// ingress within anMaxDelay (strongest simple timing heuristic) and scores
// the guess against ground truth.
func attackPrecision(sends []anSend, ingress map[[32]byte]time.Time, egress []LinkEvent) float64 {
	trueInOfOut := map[[32]byte][32]byte{}
	for _, s := range sends {
		trueInOfOut[s.outEph] = s.inEph
	}
	used := map[[32]byte]bool{}
	correct, total := 0, 0
	for _, out := range egress {
		trueIn, ok := trueInOfOut[out.Eph]
		if !ok {
			continue // egress not from this experiment's traffic
		}
		total++
		// Most recent unused ingress within the delay budget.
		var best [32]byte
		var bestT time.Time
		for eph, t := range ingress {
			if used[eph] || t.After(out.T) || out.T.Sub(t) > anMaxDelay {
				continue
			}
			if t.After(bestT) {
				best, bestT = eph, t
			}
		}
		if bestT.IsZero() {
			continue
		}
		used[best] = true
		if best == trueIn {
			correct++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(correct) / float64(total)
}

// monteCarloBaseline computes what the same attack would score if mixing were
// perfect: within each flush window the ingress↔egress pairing is a uniform
// random permutation. Self-calibrating — no analytic window assumptions.
func monteCarloBaseline(sends []anSend, egress []LinkEvent) (mean, std float64) {
	windows := clusterWindows(sends, egress)
	rng := rand.New(rand.NewSource(anSeed + 1))
	base := time.Unix(0, 0)

	var scores []float64
	for run := 0; run < anMCRuns; run++ {
		// Synthetic traffic: same windows, same sizes; within each window
		// the egress assignments are a fresh uniform permutation.
		var synth []anSend
		var evs []LinkEvent
		tick := 0
		for _, w := range windows {
			perm := rng.Perm(len(w))
			for pos, sendIdx := range w {
				// Attacker-observable ingress: arrival order (positions).
				inE := sends[sendIdx].inEph
				// Egress at position pos carries the permuted send; ground
				// truth pairs it with THAT send's ingress, not position's.
				trueSend := sends[w[perm[pos]]]
				synth = append(synth, anSend{inEph: trueSend.inEph, outEph: trueSend.outEph})
				evs = append(evs,
					LinkEvent{T: base.Add(time.Duration(tick) * time.Millisecond), In: true, Eph: inE},
					LinkEvent{T: base.Add(time.Duration(tick)*time.Millisecond + time.Microsecond), In: false, Eph: trueSend.outEph})
				tick++
			}
		}
		in := map[[32]byte]time.Time{}
		var outs []LinkEvent
		for _, ev := range evs {
			if ev.In {
				in[ev.Eph] = ev.T
			} else {
				outs = append(outs, ev)
			}
		}
		scores = append(scores, attackPrecision(synth, in, outs))
	}

	mean = 0
	for _, p := range scores {
		mean += p
	}
	mean /= float64(len(scores))
	std = 0
	for _, p := range scores {
		std += (p - mean) * (p - mean)
	}
	std = math.Sqrt(std / float64(len(scores)))
	return mean, std
}

// clusterWindows groups sends by their adversary-egress time: a gap larger
// than anWindowGap closes a flush window.
func clusterWindows(sends []anSend, egress []LinkEvent) [][]int {
	outTime := map[[32]byte]time.Time{}
	for _, ev := range egress {
		outTime[ev.Eph] = ev.T
	}
	type item struct {
		idx int
		t   time.Time
	}
	var items []item
	for i, s := range sends {
		if t, ok := outTime[s.outEph]; ok {
			items = append(items, item{idx: i, t: t})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].t.Before(items[j].t) })

	var windows [][]int
	for i, it := range items {
		if i == 0 || it.t.Sub(items[i-1].t) > anWindowGap {
			windows = append(windows, nil)
		}
		windows[len(windows)-1] = append(windows[len(windows)-1], it.idx)
	}
	return windows
}

func countDelivered(counts []int) int {
	total := 0
	for _, n := range counts {
		total += n
	}
	return total
}

func sendRawTo(t *testing.T, from, to *Node, wire []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := from.Host().Connect(ctx, peer.AddrInfo{ID: to.ID(), Addrs: to.Host().Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	s, err := from.Host().NewStream(ctx, to.ID(), ProtocolOnion)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer s.Close()
	if err := p2p.WriteMsg(s, wire); err != nil {
		t.Fatalf("write wire: %v", err)
	}
}
