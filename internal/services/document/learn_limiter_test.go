package document

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTakeWakeBatchLocked_wakesUpToFreeCapacity(t *testing.T) {
	g := &libLearnGate{limit: 5, inFlight: 1}
	ch1, ch2, ch3 := make(chan struct{}), make(chan struct{}), make(chan struct{})
	g.wait = []chan struct{}{ch1, ch2, ch3}

	g.mu.Lock()
	out := g.takeWakeBatchLocked()
	g.mu.Unlock()

	if len(out) != 3 {
		t.Fatalf("expected 3 channels (free=4, wait=3), got %d", len(out))
	}
	if len(g.wait) != 0 {
		t.Fatalf("expected wait queue drained, got len=%d", len(g.wait))
	}
	for _, ch := range out {
		close(ch)
	}
}

func TestReleaseOne_wakesMultipleWaitersWhenSeveralSlotsFree(t *testing.T) {
	g := &libLearnGate{limit: 5, inFlight: 2}
	w1, w2 := make(chan struct{}), make(chan struct{})
	g.wait = []chan struct{}{w1, w2}

	g.releaseOne()

	select {
	case <-w1:
	default:
		t.Fatal("w1 not notified")
	}
	select {
	case <-w2:
	default:
		t.Fatal("w2 not notified")
	}
	if g.inFlight != 1 {
		t.Fatalf("inFlight want 1, got %d", g.inFlight)
	}
}

func TestAcquireLibraryLearnSlot_wakesStrandedAfterMaxIncrease(t *testing.T) {
	libID := int64(998_001_235)

	var limitAtomic int32 = 1
	getMax := func() (int, error) {
		return int(atomic.LoadInt32(&limitAtomic)), nil
	}

	relHold, err := acquireLibraryLearnSlot(context.Background(), libID, getMax)
	if err != nil {
		t.Fatal(err)
	}

	var acquired int32
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel, err := acquireLibraryLearnSlot(context.Background(), libID, getMax)
			if err != nil {
				return
			}
			atomic.AddInt32(&acquired, 1)
			time.Sleep(20 * time.Millisecond)
			rel()
		}()
	}
	time.Sleep(100 * time.Millisecond)

	atomic.StoreInt32(&limitAtomic, 5)

	relBumper, err := acquireLibraryLearnSlot(context.Background(), libID, getMax)
	if err != nil {
		t.Fatal(err)
	}
	relBumper()

	relHold()
	wg.Wait()

	if n := atomic.LoadInt32(&acquired); n != 3 {
		t.Fatalf("expected 3 goroutines to acquire after max increase, got %d", n)
	}
}
