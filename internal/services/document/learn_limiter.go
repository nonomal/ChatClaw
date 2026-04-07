package document

import (
	"context"
	"sync"
)

// libLearnGate limits concurrent document learning jobs per library.
type libLearnGate struct {
	mu       sync.Mutex
	max      int
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

// acquireLibraryLearnSlot blocks until this library has fewer than maxParallel
// document jobs in progress. Caller must call the returned release exactly once.
func acquireLibraryLearnSlot(ctx context.Context, libraryID int64, maxParallel int) (release func(), err error) {
	maxParallel = clampBatchMaxDocuments(maxParallel)
	g := gateForLibrary(libraryID)

	for {
		g.mu.Lock()
		if g.max != maxParallel {
			g.max = maxParallel
		}
		if g.inFlight < g.max {
			g.inFlight++
			g.mu.Unlock()
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
	var w chan struct{}
	if len(g.wait) > 0 {
		w = g.wait[0]
		g.wait = g.wait[1:]
	}
	g.mu.Unlock()
	if w != nil {
		close(w)
	}
}
