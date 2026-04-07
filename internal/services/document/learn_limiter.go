package document

import (
	"context"
	"sync"
)

// libLearnGate limits concurrent document learning jobs per library.
type libLearnGate struct {
	mu       sync.Mutex
	limit    int // effective concurrency cap (from latest getMax, clamped)
	inFlight int
	wait     []chan struct{}
}

var libLearnGates sync.Map // libraryID -> *libLearnGate

func clampBatchMaxDocuments(n int) int {
	if n < 1 {
		return 1
	}
	if n > 5 {
		return 5
	}
	return n
}

func gateForLibrary(libraryID int64) *libLearnGate {
	if v, ok := libLearnGates.Load(libraryID); ok {
		return v.(*libLearnGate)
	}
	g := &libLearnGate{}
	if v, loaded := libLearnGates.LoadOrStore(libraryID, g); loaded {
		return v.(*libLearnGate)
	}
	return g
}

// takeWakeBatchLocked returns up to (limit-inFlight) waiters to notify.
// Caller must hold g.mu. Channels are removed from g.wait and must be closed after unlock.
func (g *libLearnGate) takeWakeBatchLocked() []chan struct{} {
	free := g.limit - g.inFlight
	if free <= 0 || len(g.wait) == 0 {
		return nil
	}
	n := free
	if n > len(g.wait) {
		n = len(g.wait)
	}
	out := make([]chan struct{}, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, g.wait[0])
		g.wait = g.wait[1:]
	}
	return out
}

// acquireLibraryLearnSlot blocks until this library has fewer than maxParallel
// document jobs in progress. Caller must call the returned release exactly once.
//
// getMax is invoked on every wait iteration so the limit tracks the latest library
// settings from DB (avoids stale max values from an older goroutine shrinking the gate).
func acquireLibraryLearnSlot(ctx context.Context, libraryID int64, getMax func() (int, error)) (release func(), err error) {
	g := gateForLibrary(libraryID)

	for {
		mpRaw, err := getMax()
		if err != nil {
			return nil, err
		}
		limit := clampBatchMaxDocuments(mpRaw)

		g.mu.Lock()
		g.limit = limit
		toWake := g.takeWakeBatchLocked()
		if g.inFlight < g.limit {
			g.inFlight++
			g.mu.Unlock()
			for _, ch := range toWake {
				close(ch)
			}
			released := false
			return func() {
				if released {
					return
				}
				released = true
				g.releaseOne()
			}, nil
		}
		ch := make(chan struct{})
		g.wait = append(g.wait, ch)
		g.mu.Unlock()
		for _, c := range toWake {
			close(c)
		}

		select {
		case <-ctx.Done():
			g.mu.Lock()
			for i, w := range g.wait {
				if w == ch {
					g.wait = append(g.wait[:i], g.wait[i+1:]...)
					break
				}
			}
			g.mu.Unlock()
			return nil, ctx.Err()
		case <-ch:
		}
	}
}

func (g *libLearnGate) releaseOne() {
	g.mu.Lock()
	if g.inFlight > 0 {
		g.inFlight--
	}
	toWake := g.takeWakeBatchLocked()
	g.mu.Unlock()
	for _, ch := range toWake {
		close(ch)
	}
}
