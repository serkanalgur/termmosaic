package buffer

import "sync"

// maxTrackedRects bounds the dirty-rect list. Once this many disjoint
// rectangles accumulate the set collapses to "the whole buffer is dirty",
// which is the correct conservative answer and keeps marking cheap in the
// pathological case.
const maxTrackedRects = 64

// dirtySet is a coalescing rectangle accumulator. It is the invalidation
// primitive ADR 0003 asks for: widgets mark rectangles, and the renderer turns
// them into the regions it diffs.
//
// It is mutex-guarded because ADR 0003 requires Invalidate to be safe to call
// from any goroutine ("the dirty accumulator is atomic" — a mutex is the Go
// spelling of that).
type dirtySet struct {
	mu    sync.Mutex
	rects []Rect
	full  bool
}

func (d *dirtySet) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rects = d.rects[:0]
	d.full = false
}

func (d *dirtySet) isEmpty() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.full && len(d.rects) == 0
}

// mark adds r, merging it into any existing rectangle it touches or overlaps.
// The overlap test is inclusive of shared edges: two rectangles sharing a column
// are contiguous on screen and cheaper as one diff region than as two.
func (d *dirtySet) mark(r Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.markLocked(r)
}

func (d *dirtySet) markLocked(r Rect) {
	if d.full {
		return
	}
	if len(d.rects) >= maxTrackedRects {
		d.full = true
		d.rects = d.rects[:0]
		return
	}
	for i, e := range d.rects {
		if !touches(e, r) {
			continue
		}
		d.rects[i] = e.Union(r)
		// The union may now touch neighbours it did not before; fold them in.
		// Bounded by maxTrackedRects so this terminates cheaply.
		for j := i + 1; j < len(d.rects); j++ {
			if touches(d.rects[j], d.rects[i]) {
				d.rects[i] = d.rects[j].Union(d.rects[i])
				d.rects = append(d.rects[:j], d.rects[j+1:]...)
			}
		}
		for j := i - 1; j >= 0; j-- {
			if touches(d.rects[j], d.rects[i]) {
				d.rects[i] = d.rects[j].Union(d.rects[i])
				d.rects = append(d.rects[:j], d.rects[j+1:]...)
			}
		}
		return
	}
	d.rects = append(d.rects, r)
}

// markAll collapses the set to fully dirty.
func (d *dirtySet) markAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.full = true
	d.rects = d.rects[:0]
}

// takeInto moves the accumulated rectangles into dst (which must be empty) and
// resets the set. dst's backing array is reused across frames by the caller, so
// a steady-state frame does not allocate here.
//
// Rectangles are clipped to the given buffer size; empty results are dropped.
// The returned slice aliases dst and is only valid until the next call.
func (d *dirtySet) takeInto(dst []Rect, bufW, bufH int) []Rect {
	d.mu.Lock()
	defer d.mu.Unlock()
	dst = dst[:0]
	if d.full {
		d.full = false
		d.rects = d.rects[:0]
		if bufW > 0 && bufH > 0 {
			return append(dst, Rect{W: bufW, H: bufH})
		}
		return dst
	}
	for _, r := range d.rects {
		if c := r.Clip(bufW, bufH); !c.Empty() {
			dst = append(dst, c)
		}
	}
	d.rects = d.rects[:0]
	return dst
}

// touches reports whether two rectangles overlap or share an edge.
func touches(a, b Rect) bool { return a.Touches(b) }
