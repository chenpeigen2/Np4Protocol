package message

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSendDispatchesToHandlers(t *testing.T) {
	b := NewMessageBus()
	b.Start()
	defer b.Stop()

	var got int32
	b.OnMessage(func(*Message) {
		atomic.AddInt32(&got, 1)
	})

	for i := 0; i < 10; i++ {
		if err := b.Send(&Message{Content: []byte("x")}); err != nil {
			t.Fatalf("Send[%d]: %v", i, err)
		}
	}

	// Give workers a moment to drain.
	deadline := make(chan struct{})
	go func() { time.Sleep(200 * time.Millisecond); close(deadline) }()
	<-deadline

	if atomic.LoadInt32(&got) != 10 {
		t.Errorf("expected 10 handler calls, got %d", got)
	}
}

func TestBroadcastDoesNotMutateInput(t *testing.T) {
	b := NewMessageBus()
	b.Start()
	defer b.Stop()

	original := &Message{Type: TypeAsync, DestID: "dest", Content: []byte("x")}
	snapshot := *original

	if err := b.Broadcast(original); err != nil {
		t.Fatalf("Broadcast: %v", err)
	}

	if original.Type != snapshot.Type || original.DestID != snapshot.DestID {
		t.Errorf("Broadcast mutated input: %+v vs %+v", original, snapshot)
	}
}

func TestBusStopsCleanly(t *testing.T) {
	b := NewMessageBus()
	b.Start()
	b.Send(&Message{Content: []byte("x")})
	b.Stop()
	// Calling Stop twice must not panic.
	b.Stop()
}

func TestBusWorkersDrain(t *testing.T) {
	b := NewMessageBus()
	b.Start()
	defer b.Stop()

	var got int32
	var wg sync.WaitGroup
	wg.Add(100)
	b.OnMessage(func(*Message) {
		atomic.AddInt32(&got, 1)
		wg.Done()
	})

	for i := 0; i < 100; i++ {
		if err := b.Send(&Message{Content: []byte("x")}); err != nil {
			t.Fatalf("Send[%d]: %v", i, err)
		}
	}
	wg.Wait()
	if atomic.LoadInt32(&got) != 100 {
		t.Errorf("expected 100 handler calls, got %d", got)
	}
}
