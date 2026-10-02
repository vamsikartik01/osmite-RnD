package daemon

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// batchFixture is one visible pane in a workspace with one client.
func batchFixture() (*Server, *Pane, *client) {
	s := &Server{}
	c := &client{out: make(chan []byte, 64), scroll: map[uint32]int{}, historyDue: map[uint32]*Pane{}}
	p := &Pane{id: 1, emu: vt.NewEmulator(20, 5)}
	t := &Tab{panes: map[uint32]*Pane{1: p}}
	ws := &Workspace{tabs: []*Tab{t}, clients: map[*client]struct{}{c: {}}}
	p.ws, p.tab = ws, t
	return s, p, c
}

// sent drains the client's queue: the payloads of its Output frames, and how
// many History frames it got.
func sent(c *client) (outputs []string, histories int) {
	for {
		select {
		case f := <-c.out:
			switch protocol.Type(f[4]) {
			case protocol.TypeOutput:
				_, data, _ := protocol.DecodeOutput(f[5:])
				outputs = append(outputs, string(data))
			case protocol.TypeHistory:
				histories++
			}
		default:
			return outputs, histories
		}
	}
}

func queue(s *Server, p *Pane, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queueOutput(p, []byte(data))
}

func TestOutputReadsAreSentTogether(t *testing.T) {
	s, p, c := batchFixture()
	queue(s, p, "a")
	queue(s, p, "b")
	queue(s, p, "c")
	time.Sleep(outputDelay + 20*time.Millisecond)
	if got, _ := sent(c); len(got) != 1 || got[0] != "abc" {
		t.Fatalf("got %q, want one frame \"abc\"", got)
	}
}

func TestSynchronizedUpdateIsHeldUntilItEnds(t *testing.T) {
	s, p, c := batchFixture()
	s.mu.Lock()
	p.setFrame(true)
	s.mu.Unlock()
	queue(s, p, "first half ")
	time.Sleep(outputDelay + 20*time.Millisecond)
	if got, _ := sent(c); len(got) != 0 {
		t.Fatalf("sent %q mid-update", got)
	}
	s.mu.Lock()
	p.setFrame(false)
	s.mu.Unlock()
	queue(s, p, "second half")
	if got, _ := sent(c); len(got) != 1 || got[0] != "first half second half" {
		t.Fatalf("got %q, want the whole update at once", got)
	}
}

func TestUnendedUpdateIsSentAfterTimeout(t *testing.T) {
	s, p, c := batchFixture()
	s.mu.Lock()
	p.setFrame(true)
	s.mu.Unlock()
	queue(s, p, "x")
	time.Sleep(frameTimeout + 50*time.Millisecond)
	if got, _ := sent(c); len(got) != 1 {
		t.Fatalf("got %q, want the held output after the timeout", got)
	}
}

// Held output must go out before a snapshot (which already shows it), and
// never again after.
func TestFlushBeforeSnapshotSendsOnce(t *testing.T) {
	s, p, c := batchFixture()
	queue(s, p, "x")
	s.mu.Lock()
	s.flushWorkspace(p.ws)
	s.mu.Unlock()
	time.Sleep(outputDelay + 20*time.Millisecond)
	if got, _ := sent(c); len(got) != 1 {
		t.Fatalf("got %q, want exactly one frame", got)
	}
}

func TestWheelBurstIsThrottled(t *testing.T) {
	s, p, c := batchFixture()
	s.mu.Lock()
	for i := range 10 {
		c.scroll[p.id] = i + 1
		s.queueHistory(c, p)
	}
	s.mu.Unlock()
	if _, n := sent(c); n != 1 {
		t.Fatalf("%d History frames right away, want 1", n)
	}
	time.Sleep(historyInterval + 30*time.Millisecond)
	if _, n := sent(c); n != 1 {
		t.Fatalf("%d History frames after the burst, want 1 (the final offset)", n)
	}
}

// Programs only use synchronized updates once the terminal says it knows the
// mode; newPane primes the emulator so it does.
func TestEmulatorReportsSynchronizedOutput(t *testing.T) {
	emu := vt.NewEmulator(20, 5)
	_, _ = emu.WriteString(ansi.SetModeSynchronizedOutput + ansi.ResetModeSynchronizedOutput)
	reply := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := io.ReadAtLeast(emu, buf, 1)
		reply <- string(buf[:n])
	}()
	_, _ = emu.WriteString(ansi.RequestModeSynchronizedOutput)
	select {
	case r := <-reply:
		if !strings.Contains(r, "2026;2$y") {
			t.Fatalf("DECRQM reply %q, want mode 2026 reported as reset (2)", r)
		}
	case <-time.After(time.Second):
		t.Fatal("no DECRQM reply")
	}
}
