package mix

import (
	"sort"
	"testing"
)

// TestCryptoShuffleIsAPermutation pins the shuffle's correctness contract:
// the output is a permutation of the input (no loss, no duplication) — and,
// with n=10, is not the identity permutation (P(identity) = 1/10!, safely
// non-flaky). The security claim — unpredictability to an observer — follows
// from the crypto/rand source, which leaves no PRNG state to recover.
func TestCryptoShuffleIsAPermutation(t *testing.T) {
	for round := 0; round < 8; round++ {
		n := 10
		order := make([]int, n)
		for i := range order {
			order[i] = i
		}
		identity := append([]int(nil), order...)
		cryptoShuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })

		sorted := append([]int(nil), order...)
		sort.Ints(sorted)
		for i := range sorted {
			if sorted[i] != i {
				t.Fatalf("round %d: output is not a permutation: %v", round, order)
			}
		}
		same := true
		for i := range order {
			if order[i] != identity[i] {
				same = false
				break
			}
		}
		if same {
			t.Fatalf("round %d: shuffle returned the identity permutation", round)
		}
	}
}

// TestCryptoShuffleDistributes: over many shuffles of a 2-element buffer, both
// orders must appear roughly half the time — a broken swap direction (always
// swapping one way) biases this to 100/0.
func TestCryptoShuffleDistributes(t *testing.T) {
	firstAtZero := 0
	const rounds = 2000
	for i := 0; i < rounds; i++ {
		order := []int{0, 1}
		cryptoShuffle(2, func(i, j int) { order[i], order[j] = order[j], order[i] })
		if order[0] == 0 {
			firstAtZero++
		}
	}
	// P(|deviation| > 15% of rounds) under a fair coin is astronomically low.
	if firstAtZero < rounds*35/100 || firstAtZero > rounds*65/100 {
		t.Fatalf("shuffle biased: first slot held 0 in %d/%d rounds", firstAtZero, rounds)
	}
}
