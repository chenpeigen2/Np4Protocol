package identity

import (
	"sync"
	"testing"
)

// TDD: two concurrent LoadOrCreate calls on a fresh path both see "not
// exists", both generate, and the second WriteFile silently overwrites the
// first — the callers walk away with DIFFERENT peer IDs while agreeing on
// one file. Every caller must observe the same identity (O_EXCL create +
// reload on conflict).
func TestLoadOrCreateConcurrentSingleIdentity(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		path := t.TempDir() + "/identity"

		const n = 8
		results := make(chan string, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				id, err := LoadOrCreate(path)
				if err != nil {
					t.Errorf("attempt %d: LoadOrCreate: %v", attempt, err)
					return
				}
				results <- id.PeerID().String()
			}()
		}
		wg.Wait()
		close(results)

		unique := map[string]bool{}
		for pid := range results {
			unique[pid] = true
		}
		if len(unique) != 1 {
			t.Fatalf("attempt %d: %d concurrent creators produced %d distinct identities",
				attempt, n, len(unique))
		}
	}
}
