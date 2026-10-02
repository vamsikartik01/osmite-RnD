package daemon

import (
	"time"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// Output batching. A program's screen update often reaches us split across
// several PTY reads. Forwarding each read as its own frame lets clients draw
// the update half done (flicker, a cursor jumping about), and in remote mode
// every frame is a websocket message to the browser. So output is held
// briefly and sent as one frame: while the program is inside a synchronized
// update (mode 2026) until it ends it, otherwise for outputDelay.
const (
	outputDelay  = 4 * time.Millisecond
	frameTimeout = 100 * time.Millisecond // for a program that never ends its update
	maxPending   = 1 << 20
)

// historyInterval is how often a client's scrollback view is redrawn while
// the wheel turns: a fast swipe sends many notches, and each would otherwise
// be a full-screen History frame.
const historyInterval = 16 * time.Millisecond

// queueOutput holds output the emulator has just taken until it can be sent
// as part of a whole update. Called with s.mu held.
func (s *Server) queueOutput(p *Pane, data []byte) {
	p.pending = append(p.pending, data...)
	switch {
	case len(p.pending) >= maxPending:
		s.flushOutput(p)
	case p.inFrame:
		s.armOutput(p, frameTimeout-time.Since(p.frameStart))
	case p.frameEnded:
		s.flushOutput(p)
	default:
		s.armOutput(p, outputDelay)
	}
}

// armOutput flushes p's output after d, unless a flush is already due.
func (s *Server) armOutput(p *Pane, d time.Duration) {
	if p.flushArmed {
		return
	}
	p.flushArmed = true
	gen := p.flushGen
	time.AfterFunc(max(d, 0), func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if p.flushGen != gen || p.exited {
			return // flushed meanwhile
		}
		p.flushArmed = false
		if left := frameTimeout - time.Since(p.frameStart); p.inFrame && left > 0 {
			s.armOutput(p, left) // the update began after this was armed
			return
		}
		s.flushOutput(p)
	})
}

// flushOutput sends p's held output to the clients viewing it. Called with
// s.mu held.
func (s *Server) flushOutput(p *Pane) {
	p.flushGen++
	p.flushArmed = false
	p.frameEnded = false
	if p.inFrame {
		p.frameStart = time.Now() // an update that ran over: the next part waits again
	}
	if len(p.pending) == 0 {
		return
	}
	if p.visible() {
		frame := protocol.EncodeFrame(protocol.TypeOutput, protocol.EncodeOutput(p.id, p.pending))
		for c := range p.ws.clients {
			c.queue(frame)
		}
	}
	p.pending = p.pending[:0]
}

// flushWorkspace sends ws's held output. Snapshots already show it, so it
// must go out before any snapshot does, or clients would apply it twice.
func (s *Server) flushWorkspace(ws *Workspace) {
	for _, t := range ws.tabs {
		for _, p := range t.panes {
			s.flushOutput(p)
		}
	}
}

// setFrame records the program beginning or ending a synchronized update.
func (p *Pane) setFrame(on bool) {
	if on && !p.inFrame {
		p.frameStart = time.Now()
	}
	if !on && p.inFrame {
		p.frameEnded = true
	}
	p.inFrame = on
}

// queueHistory sends c the scrollback view of p at its current offset: right
// away, then at most once per historyInterval while the wheel keeps turning.
// Called with s.mu held.
func (s *Server) queueHistory(c *client, p *Pane) {
	if c.historyArmed {
		c.historyDue[p.id] = p
		return
	}
	c.queueJSON(protocol.TypeHistory, p.history(c.scroll[p.id]))
	s.armHistory(c)
}

func (s *Server) armHistory(c *client) {
	c.historyArmed = true
	time.AfterFunc(historyInterval, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		c.historyArmed = false
		if len(c.historyDue) == 0 || c.closed {
			return
		}
		for id, p := range c.historyDue {
			if !p.exited {
				c.queueJSON(protocol.TypeHistory, p.history(c.scroll[id]))
			}
			delete(c.historyDue, id)
		}
		s.armHistory(c)
	})
}
