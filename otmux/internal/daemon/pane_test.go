package daemon

import (
	"testing"
	"time"
)

// Output right after the user types (an agent echoing or redrawing its
// input box) must not count as the agent working.
func TestEchoIsNotWork(t *testing.T) {
	p := &Pane{}
	t0 := time.Unix(1000, 0)

	p.lastPoke = t0
	p.noteOutput(t0.Add(100 * time.Millisecond))
	if p.bursts != 0 {
		t.Fatal("output just after typing counted as work")
	}
	for i := 0; i < workBursts; i++ {
		p.noteOutput(t0.Add(time.Second + time.Duration(i)*100*time.Millisecond))
	}
	if !p.working(t0.Add(time.Second + 300*time.Millisecond)) {
		t.Fatal("a spinner well after typing should count as work")
	}
	if p.working(t0.Add(3 * time.Second)) {
		t.Fatal("still working after the output stopped")
	}
}

// One late redraw after a click or resize (Claude Code clearing a hint a
// second later) must not count as work, nor must one write split into reads.
func TestLoneRedrawIsNotWork(t *testing.T) {
	p := &Pane{}
	t0 := time.Unix(1000, 0)

	p.lastPoke = t0
	p.noteOutput(t0.Add(985 * time.Millisecond))
	p.noteOutput(t0.Add(986 * time.Millisecond)) // same write, second read
	if p.working(t0.Add(time.Second)) {
		t.Fatal("a single late redraw counted as work")
	}
	// Another lone redraw well after the first starts over.
	p.noteOutput(t0.Add(4 * time.Second))
	if p.working(t0.Add(4 * time.Second)) {
		t.Fatal("two far-apart redraws counted as work")
	}
}
