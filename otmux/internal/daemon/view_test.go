package daemon

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// Written to a blank terminal, a snapshot with history must leave the same
// history in its scrollback and the same screen as the pane's.
func TestSnapshotWithHistoryRoundTrips(t *testing.T) {
	src := vt.NewEmulator(20, 5)
	src.SetScrollbackSize(1000)
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(src, "\x1b[3%dmline %d\x1b[0m\r\n", i%7, i)
	}
	_, _ = src.WriteString("$ ")
	p := &Pane{emu: src}

	snap := p.snapshotWithHistory(100)
	if snap.Scrollback != src.ScrollbackLen() {
		t.Fatalf("Scrollback = %d, want all %d history lines", snap.Scrollback, src.ScrollbackLen())
	}
	dst := vt.NewEmulator(20, 5)
	dst.SetScrollbackSize(1000)
	_, _ = dst.WriteString(snap.Data)

	if dst.ScrollbackLen() != src.ScrollbackLen() {
		t.Fatalf("scrollback has %d lines, want %d", dst.ScrollbackLen(), src.ScrollbackLen())
	}
	for i := range src.ScrollbackLen() {
		want, got := strings.TrimRight(src.Scrollback().Line(i).String(), " "), strings.TrimRight(dst.Scrollback().Line(i).String(), " ")
		if got != want {
			t.Fatalf("history line %d = %q, want %q", i, got, want)
		}
	}
	if got, want := dst.String(), src.String(); got != want {
		t.Fatalf("screen:\n%s\nwant:\n%s", got, want)
	}

	// Fewer lines than there are: only the newest.
	if s := p.snapshotWithHistory(3); s.Scrollback != 3 || !strings.Contains(s.Data, "line 24") || strings.Contains(s.Data, "line 23") {
		t.Fatalf("3 lines of history: %d lines, %q", s.Scrollback, s.Data)
	}
}

func TestSnapshotWithHistorySkipsFullScreenPrograms(t *testing.T) {
	emu := vt.NewEmulator(20, 5)
	for i := range 10 {
		fmt.Fprintf(emu, "line %d\r\n", i)
	}
	_, _ = emu.WriteString("\x1b[?1049h") // e.g. vim
	if s := (&Pane{emu: emu}).snapshotWithHistory(100); s.Scrollback != 0 {
		t.Fatalf("alternate screen snapshot carried %d history lines", s.Scrollback)
	}
}
