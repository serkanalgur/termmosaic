package render

import (
	"sync"
	"testing"
	"time"

	"github.com/serkanalgur/termmosaic/headless"
)

// TestPostWakesThePacer is the regression test for the async story ADR 0003
// documents: mutate widget state inside Post, and the next frame must paint it.
//
// Pacer.Run gates every frame on NeedsFrame(), and needsFrameLocked did not
// consider queued callbacks, so a posted callback only ever ran if some other
// work happened to dirty the buffer. A frame budget with no independent source
// of dirt therefore painted once and stopped - which is every app that fetches
// on a timer and updates the screen from the result.
func TestPostWakesThePacer(t *testing.T) {
	sink := headless.NewMemorySink(20, 3)
	r := New(sink, Config{Width: 20, Height: 3})
	if err := r.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	if r.NeedsFrame() {
		t.Fatal("precondition: the renderer should be idle after the first frame")
	}

	// applied is written on the render goroutine (Post runs its callback there)
	// and read here, so it needs the mutex. A bare bool would be a data race in
	// the test itself.
	var mu sync.Mutex
	applied := false
	r.Post(func() {
		mu.Lock()
		applied = true
		mu.Unlock()
		r.InvalidateAll()
	})

	if !r.NeedsFrame() {
		t.Error("NeedsFrame() is false after Post: the callback only runs inside Render, so it can never run")
	}

	quit := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- NewPacer(r).Run(quit) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := applied
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(quit)
	<-done

	mu.Lock()
	wasApplied := applied
	mu.Unlock()
	if !wasApplied {
		t.Error("the posted callback never ran under Pacer.Run: the pacer idles forever after the first frame")
	}
}

// TestPostDoesNotWakeAnIdleRendererPacer guards the other side of the fix: a
// Post whose callback marks nothing dirty must still produce zero bytes, so
// the idle frame stays free (ADR 0003's frame-budget contract).
func TestPostDoesNotWakeAnIdleRendererPacer(t *testing.T) {
	sink := headless.NewMemorySink(20, 3)
	r := New(sink, Config{Width: 20, Height: 3})
	if err := r.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}

	r.Post(func() { /* deliberately invalidates nothing */ })
	// One frame is spent running the callback; it must write nothing.
	n, err := r.Render()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("a no-op Post wrote %d bytes, want 0: idle frames must stay free", n)
	}
}
