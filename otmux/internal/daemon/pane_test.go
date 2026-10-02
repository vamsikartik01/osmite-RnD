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
	if !p.lastOutput.IsZero() {
		t.Fatal("output just after typing counted as work")
	}
	p.noteOutput(t0.Add(time.Second))
	if !p.lastOutput.Equal(t0.Add(time.Second)) {
		t.Fatal("output well after typing should count as work")
	}
}
